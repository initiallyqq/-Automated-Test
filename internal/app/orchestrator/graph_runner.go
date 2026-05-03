package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"automated-test/internal/domain/workflow"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/graph"
)

type GraphRunner struct {
	orch     *Orchestrator
	compiled *graph.Graph
}

func NewGraphRunner(opts Options) (*GraphRunner, error) {
	orch := New(opts)

	schema := graph.NewStateSchema().
		AddField("workflow_state", graph.StateField{
			Type:    typeOf[workflow.State](),
			Reducer: graph.DefaultReducer,
			Default: func() any { return workflow.State{} },
		}).
		AddField("request", graph.StateField{
			Type:    typeOf[RunRequest](),
			Reducer: graph.CoverReducer,
			Default: func() any { return RunRequest{} },
		})

	sg := graph.NewStateGraph(schema)

	gr := &GraphRunner{orch: orch}

	sg.AddNode("project_analysis", gr.node("projectAnalysis"))
	sg.AddNode("scenario_planning", gr.node("scenarioPlanning"))
	sg.AddNode("test_generation", gr.node("testGeneration"))
	sg.AddNode("test_execution", gr.node("testExecution"))
	sg.AddNode("failure_diagnosis", gr.node("failureDiagnosis"))
	sg.AddNode("test_fixing", gr.node("testFixing"))
	sg.AddNode("review_guard", gr.node("reviewGuard"))
	sg.AddNode("apply_patch", gr.node("applyPatch"))
	sg.AddNode("rerun", gr.node("reRun"))
	sg.AddNode("archive", gr.node("archive"))

	sg.SetEntryPoint("project_analysis")
	sg.SetFinishPoint("archive")

	sg.AddEdge("project_analysis", "scenario_planning")
	sg.AddEdge("scenario_planning", "test_generation")
	sg.AddEdge("test_generation", "test_execution")
	sg.AddConditionalEdges("test_execution", gr.routeAfterExecution, map[string]string{
		"archive":           "archive",
		"failure_diagnosis": "failure_diagnosis",
	})
	sg.AddConditionalEdges("failure_diagnosis", gr.routeAfterDiagnosis, map[string]string{
		"test_fixing": "test_fixing",
		"archive":     "archive",
	})
	sg.AddEdge("test_fixing", "review_guard")
	sg.AddConditionalEdges("review_guard", gr.routeAfterGuard, map[string]string{
		"apply_patch": "apply_patch",
		"archive":     "archive",
	})
	sg.AddEdge("apply_patch", "rerun")
	sg.AddConditionalEdges("rerun", gr.routeAfterRerun, map[string]string{
		"failure_diagnosis": "failure_diagnosis",
		"archive":           "archive",
	})

	compiled, err := sg.Compile()
	if err != nil {
		return nil, fmt.Errorf("compile graph: %w", err)
	}
	gr.compiled = compiled
	return gr, nil
}

func (gr *GraphRunner) Run(ctx context.Context, req RunRequest, taskID string) (workflow.State, error) {
	var err error
	req, err = gr.orch.prepareRunRequest(req)
	if err != nil {
		return workflow.State{}, err
	}
	defer gr.orch.stopTargetApp()
	ws := workflow.NewState(taskID, req.ProjectID, gr.orch.config.Patch.MaxTestFixRetry)
	ws.RepoVersion = "local"
	if gr.orch.repo != nil {
		if err := gr.orch.repo.UpsertProject(ctx, req.ProjectID, req.ProjectID, req.RepoPath, "fullstack"); err != nil {
			return ws, err
		}
		if err := gr.orch.repo.SaveTask(ctx, ws); err != nil {
			return ws, err
		}
	}

	initialState := graph.State{
		"workflow_state": ws,
		"request":        req,
	}

	executor, err := graph.NewExecutor(gr.compiled, graph.WithMaxSteps(50))
	if err != nil {
		return ws, fmt.Errorf("create executor: %w", err)
	}

	eventChan, err := executor.Execute(ctx, initialState, &agent.Invocation{InvocationID: taskID})
	if err != nil {
		gr.orch.emitProgress(ProgressEvent{
			Type:    ProgressError,
			TaskID:  taskID,
			Status:  string(workflow.StatusFailed),
			Message: err.Error(),
		})
		return ws, fmt.Errorf("execute graph: %w", err)
	}

	for evt := range eventChan {
		if evt == nil || evt.StateDelta == nil {
			continue
		}
		if raw, ok := evt.StateDelta["workflow_state"]; ok {
			var s workflow.State
			if json.Unmarshal(raw, &s) == nil {
				ws = s
			}
		}
	}

	gr.orch.emitProgress(ProgressEvent{
		Type:   ProgressDone,
		TaskID: ws.TaskID,
		Status: string(ws.Status),
		Phase:  string(ws.Phase),
		Data:   ws,
	})
	return ws, nil
}

func (gr *GraphRunner) DOT() string {
	return gr.compiled.DOT()
}

func (gr *GraphRunner) WriteDOT(w io.Writer) error {
	return gr.compiled.WriteDOT(w)
}

func (gr *GraphRunner) RenderImage(ctx context.Context, format, outputPath string) error {
	return gr.compiled.RenderImage(ctx, format, outputPath)
}

type stepFunc func(ctx context.Context, state *workflow.State, req RunRequest) error

func (gr *GraphRunner) node(methodName string) graph.NodeFunc {
	var fn stepFunc
	switch methodName {
	case "projectAnalysis":
		fn = gr.orch.projectAnalysis
	case "scenarioPlanning":
		fn = gr.orch.scenarioPlanning
	case "testGeneration":
		fn = gr.orch.testGeneration
	case "testExecution":
		fn = gr.orch.testExecution
	case "failureDiagnosis":
		fn = gr.orch.failureDiagnosis
	case "testFixing":
		fn = gr.orch.testFixing
	case "reviewGuard":
		fn = gr.orch.reviewGuard
	case "applyPatch":
		fn = gr.orch.applyPatch
	case "reRun":
		fn = gr.orch.reRun
	case "archive":
		fn = gr.orch.archive
	default:
		panic("unknown node: " + methodName)
	}

	return func(ctx context.Context, state graph.State) (any, error) {
		s, _ := getWorkflowState(state)
		req, _ := getRequest(state)
		from := s.Phase
		if err := fn(ctx, &s, req); err != nil {
			s.Phase = workflow.PhaseFailed
			s.Status = workflow.StatusFailed
			s.LastError = err.Error()
			_ = gr.orch.persist(ctx, from, s, methodName+" failed")
			gr.orch.emitProgress(ProgressEvent{
				Type:    ProgressError,
				TaskID:  s.TaskID,
				Phase:   string(s.Phase),
				Status:  string(s.Status),
				Message: err.Error(),
				Data:    s,
			})
			return nil, err
		}
		if err := gr.orch.persist(ctx, from, s, methodName+" completed"); err != nil {
			return nil, err
		}
		return graph.State{"workflow_state": s}, nil
	}
}

func (gr *GraphRunner) routeAfterExecution(_ context.Context, state graph.State) (string, error) {
	s, ok := getWorkflowState(state)
	if !ok {
		return "archive", nil
	}
	if s.ExecutionResult != nil && !s.ExecutionResult.Passed {
		return "failure_diagnosis", nil
	}
	return "archive", nil
}

func (gr *GraphRunner) routeAfterDiagnosis(_ context.Context, state graph.State) (string, error) {
	s, ok := getWorkflowState(state)
	if !ok {
		return "archive", nil
	}
	if shouldFixTests(&s) {
		return "test_fixing", nil
	}
	return "archive", nil
}

func (gr *GraphRunner) routeAfterGuard(_ context.Context, state graph.State) (string, error) {
	s, ok := getWorkflowState(state)
	if !ok {
		return "archive", nil
	}
	if s.GuardState.PatchAllowed {
		return "apply_patch", nil
	}
	return "archive", nil
}

func (gr *GraphRunner) routeAfterRerun(_ context.Context, state graph.State) (string, error) {
	s, ok := getWorkflowState(state)
	if !ok {
		return "archive", nil
	}
	if s.ExecutionResult != nil && !s.ExecutionResult.Passed &&
		s.RetryState.TestFixRetryCount < s.RetryState.MaxTestFixRetry {
		return "failure_diagnosis", nil
	}
	return "archive", nil
}

func getWorkflowState(state graph.State) (workflow.State, bool) {
	v, ok := state["workflow_state"]
	if !ok {
		return workflow.State{}, false
	}
	s, ok := v.(workflow.State)
	return s, ok
}

func getRequest(state graph.State) (RunRequest, bool) {
	v, ok := state["request"]
	if !ok {
		return RunRequest{}, false
	}
	r, ok := v.(RunRequest)
	return r, ok
}

func typeOf[T any]() reflect.Type {
	var zero T
	return reflect.TypeOf(zero)
}
