package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"automated-test/internal/config"
	"automated-test/internal/domain/workflow"
	"automated-test/internal/infra/agentruntime"
	"automated-test/internal/infra/llm"
	apiscantool "automated-test/internal/tools/apiscan"
	dbscantool "automated-test/internal/tools/dbscan"
	generatortool "automated-test/internal/tools/generator"
	guardtool "automated-test/internal/tools/guard"
	patchtool "automated-test/internal/tools/patch"
	playwrighttool "automated-test/internal/tools/playwright"
	repotool "automated-test/internal/tools/repo"
)

type Options struct {
	Config        config.Config
	Repository    Repository
	JSONGenerator JSONGenerator
	AgentExecutor agentruntime.AgentExecutor
	ToolRegistry  *agentruntime.Registry
	ProgressChan  chan<- ProgressEvent
}

type Orchestrator struct {
	config        config.Config
	repo          Repository
	agentExecutor agentruntime.AgentExecutor
	toolRegistry  *agentruntime.Registry
	progressChan  chan<- ProgressEvent
}

type JSONGenerator interface {
	GenerateJSON(ctx context.Context, prompt string, schema any) ([]byte, error)
}

type Repository interface {
	UpsertProject(ctx context.Context, id, name, repoPath, projectType string) error
	SaveTask(ctx context.Context, state workflow.State) error
	SaveEvent(ctx context.Context, taskID string, from, to workflow.Phase, status workflow.Status, reason string) error
	SaveAgentRun(ctx context.Context, record agentruntime.RunRecord) error
	SaveDiagnosis(ctx context.Context, taskID string, result workflow.DiagnosisResult) error
	SavePatch(ctx context.Context, taskID string, patch workflow.Patch) error
	SaveTestArtifacts(ctx context.Context, taskID string, artifacts []workflow.TestArtifact) error
	SaveExecution(ctx context.Context, taskID string, result workflow.ExecutionResult) error
}

type RunRequest struct {
	ProjectID             string `json:"projectId"`
	RepoPath              string `json:"repoPath"`
	BaseURL               string `json:"baseUrl,omitempty"`
	AutoApplyPatch        *bool  `json:"autoApplyPatch,omitempty"`
	UseGraph              bool   `json:"useGraph,omitempty"`
	Headed                bool   `json:"headed,omitempty"`
	Visual                bool   `json:"visual,omitempty"`
	SlowMoMS              int    `json:"slowMoMs,omitempty"`
	ForceExecutionFailure bool   `json:"forceExecutionFailure,omitempty"`
}

type RunResult struct {
	TaskID string          `json:"taskId"`
	Phase  workflow.Phase  `json:"phase"`
	Status workflow.Status `json:"status"`
	State  workflow.State  `json:"state"`
}

type ProgressEvent struct {
	Type      string    `json:"type"`
	TaskID    string    `json:"taskId"`
	Timestamp time.Time `json:"timestamp"`
	Phase     string    `json:"phase,omitempty"`
	Status    string    `json:"status,omitempty"`
	AgentName string    `json:"agentName,omitempty"`
	Message   string    `json:"message,omitempty"`
	Data      any       `json:"data,omitempty"`
}

func (e ProgressEvent) SSE() string {
	b, _ := json.Marshal(e)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", e.Type, string(b))
}

const (
	ProgressPhase = "phase"
	ProgressAgent = "agent"
	ProgressExec  = "execution"
	ProgressDone  = "done"
	ProgressError = "error"
)

func New(opts Options) *Orchestrator {
	toolRegistry := defaultToolRegistry(opts.ToolRegistry)
	generator := defaultJSONGenerator(opts.Config, opts.JSONGenerator)
	return &Orchestrator{
		config:        opts.Config,
		repo:          opts.Repository,
		agentExecutor: defaultAgentExecutor(opts.AgentExecutor, generator, toolRegistry),
		toolRegistry:  toolRegistry,
		progressChan:  opts.ProgressChan,
	}
}

func defaultAgentExecutor(provided agentruntime.AgentExecutor, generator JSONGenerator, toolRegistry *agentruntime.Registry) agentruntime.AgentExecutor {
	if provided != nil {
		return provided
	}
	if generator == nil {
		return nil
	}
	executor, err := agentruntime.NewTRPCAgentExecutorFromJSONExecutor("qwen-json", generator, toolRegistry)
	if err != nil {
		return nil
	}
	return executor
}

func defaultToolRegistry(provided *agentruntime.Registry) *agentruntime.Registry {
	if provided != nil {
		return provided
	}
	registry, err := agentruntime.NewDefaultToolRegistry()
	if err != nil {
		return nil
	}
	return registry
}

func defaultJSONGenerator(cfg config.Config, provided JSONGenerator) JSONGenerator {
	if provided != nil {
		return provided
	}
	if cfg.LLM.Provider != "qwen" || os.Getenv("QWEN_API_KEY") == "" {
		return nil
	}
	client, err := llm.NewQwenClient(cfg.LLM.Model)
	if err != nil {
		return nil
	}
	return client
}

func (o *Orchestrator) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
	state := workflow.NewState(taskID, req.ProjectID, o.config.Patch.MaxTestFixRetry)
	state.RepoVersion = "local"
	if o.repo != nil {
		if err := o.repo.UpsertProject(ctx, req.ProjectID, req.ProjectID, req.RepoPath, "fullstack"); err != nil {
			return RunResult{}, err
		}
		if err := o.repo.SaveTask(ctx, state); err != nil {
			return RunResult{}, err
		}
	}

	steps := []func(context.Context, *workflow.State, RunRequest) error{
		o.projectAnalysis,
		o.scenarioPlanning,
		o.testGeneration,
		o.testExecution,
	}

	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return RunResult{}, err
		}
		from := state.Phase
		if err := step(ctx, &state, req); err != nil {
			state.Phase = workflow.PhaseFailed
			state.Status = workflow.StatusFailed
			state.LastError = err.Error()
			_ = o.persist(ctx, from, state, "step failed")
			return RunResult{TaskID: taskID, Phase: state.Phase, Status: state.Status, State: state}, err
		}
		if err := o.persist(ctx, from, state, "step completed"); err != nil {
			return RunResult{}, err
		}
	}

	if state.ExecutionResult != nil && !state.ExecutionResult.Passed {
		if err := o.runFailureBranch(ctx, &state, req); err != nil {
			state.Phase = workflow.PhaseFailed
			state.Status = workflow.StatusFailed
			state.LastError = err.Error()
			_ = o.persist(ctx, workflow.PhaseFailureDiagnosis, state, "failure branch failed")
			return RunResult{TaskID: taskID, Phase: state.Phase, Status: state.Status, State: state}, err
		}
	} else {
		from := state.Phase
		if err := o.archive(ctx, &state, req); err != nil {
			state.Phase = workflow.PhaseFailed
			state.Status = workflow.StatusFailed
			state.LastError = err.Error()
			_ = o.persist(ctx, from, state, "archive failed")
			return RunResult{TaskID: taskID, Phase: state.Phase, Status: state.Status, State: state}, err
		}
		if err := o.persist(ctx, from, state, "archive completed"); err != nil {
			return RunResult{}, err
		}
	}

	o.emitProgress(ProgressEvent{
		Type:   ProgressDone,
		TaskID: state.TaskID,
		Status: string(state.Status),
		Phase:  string(state.Phase),
		Data:   state,
	})
	return RunResult{TaskID: taskID, Phase: state.Phase, Status: state.Status, State: state}, nil
}

func (o *Orchestrator) runFailureBranch(ctx context.Context, state *workflow.State, req RunRequest) error {
	from := state.Phase
	if err := o.failureDiagnosis(ctx, state, req); err != nil {
		return err
	}
	if err := o.persist(ctx, from, *state, "diagnosis completed"); err != nil {
		return err
	}

	if shouldFixTests(state) {
		from = state.Phase
		if err := o.testFixing(ctx, state, req); err != nil {
			return err
		}
		if err := o.persist(ctx, from, *state, "test fixing completed"); err != nil {
			return err
		}

		from = state.Phase
		if err := o.reviewGuard(ctx, state, req); err != nil {
			return err
		}
		if err := o.persist(ctx, from, *state, "review guard completed"); err != nil {
			return err
		}

		autoApply := o.config.Patch.AutoApply
		if req.AutoApplyPatch != nil {
			autoApply = *req.AutoApplyPatch
		}
		if state.GuardState.PatchAllowed && autoApply {
			from = state.Phase
			if err := o.applyPatch(ctx, state, req); err != nil {
				return err
			}
			if err := o.persist(ctx, from, *state, "patch applied"); err != nil {
				return err
			}

			from = state.Phase
			if err := o.reRun(ctx, state, req); err != nil {
				return err
			}
			if err := o.persist(ctx, from, *state, "rerun completed"); err != nil {
				return err
			}
		}
	}

	from = state.Phase
	if err := o.archive(ctx, state, req); err != nil {
		return err
	}
	if state.ExecutionResult != nil && state.ExecutionResult.Passed {
		state.Status = workflow.StatusSucceeded
	} else {
		state.Status = workflow.StatusPartialSucceeded
	}
	return o.persist(ctx, from, *state, "archived with diagnosis")
}

func shouldFixTests(state *workflow.State) bool {
	if state.DiagnosisResult == nil {
		return false
	}
	return shouldFixFailureType(state.DiagnosisResult.FailureType)
}

func shouldFixFailureType(failureType string) bool {
	switch failureType {
	case "TEST_CODE", "FIXTURE", "MOCK":
		return true
	default:
		return false
	}
}

func (o *Orchestrator) persist(ctx context.Context, from workflow.Phase, state workflow.State, reason string) error {
	if o.repo == nil {
		return nil
	}
	if err := o.repo.SaveTask(ctx, state); err != nil {
		return err
	}
	if state.Phase == workflow.PhaseFailureDiagnosis && state.DiagnosisResult != nil {
		if err := o.repo.SaveDiagnosis(ctx, state.TaskID, *state.DiagnosisResult); err != nil {
			return err
		}
	}
	if state.Phase == workflow.PhaseTestFixing || state.Phase == workflow.PhaseReviewGuard {
		for _, patch := range state.TestFixPatches {
			if err := o.repo.SavePatch(ctx, state.TaskID, patch); err != nil {
				return err
			}
		}
	}
	return o.repo.SaveEvent(ctx, state.TaskID, from, state.Phase, state.Status, reason)
}

func (o *Orchestrator) emitProgress(evt ProgressEvent) {
	if o.progressChan == nil {
		return
	}
	evt.Timestamp = time.Now().UTC()
	select {
	case o.progressChan <- evt:
	default:
	}
}

func (o *Orchestrator) emitPhase(taskID string, phase workflow.Phase, status workflow.Status, msg string) {
	o.emitProgress(ProgressEvent{
		Type:    ProgressPhase,
		TaskID:  taskID,
		Phase:   string(phase),
		Status:  string(status),
		Message: msg,
	})
}

func (o *Orchestrator) emitAgent(taskID, agentName, status, msg string, data any) {
	o.emitProgress(ProgressEvent{
		Type:      ProgressAgent,
		TaskID:    taskID,
		AgentName: agentName,
		Status:    status,
		Message:   msg,
		Data:      data,
	})
}

func (o *Orchestrator) projectAnalysis(ctx context.Context, state *workflow.State, req RunRequest) error {
	state.Phase = workflow.PhaseProjectAnalysis
	state.Status = workflow.StatusRunning
	o.emitPhase(state.TaskID, state.Phase, state.Status, "scanning repository")

	scan, err := o.scanRepo(ctx, state.TaskID, req.RepoPath)
	if err != nil {
		return err
	}
	projectType := "fullstack"
	if !scan.Frontend && scan.Backend {
		projectType = "backend"
	}
	if scan.Frontend && !scan.Backend {
		projectType = "frontend"
	}
	state.ProjectProfile = &workflow.ProjectProfile{
		ProjectType: projectType,
		Languages:   scan.Languages,
		Frameworks:  scan.Frameworks,
		PackageTool: scan.PackageTool,
		Risks:       scan.Risks,
	}
	apiScan, err := o.scanAPI(ctx, state.TaskID, req.RepoPath)
	if err != nil {
		return err
	}
	dbScan, err := o.scanDB(ctx, state.TaskID, req.RepoPath)
	if err != nil {
		return err
	}
	state.PageGraph = inferPageGraph(scan)
	state.ApiGraph = apiGraphFromScan(apiScan, scan)
	state.DataModelGraph = dataModelGraphFromScan(dbScan, scan)
	return nil
}

func (o *Orchestrator) scanRepo(ctx context.Context, taskID, repoPath string) (repotool.ScanResult, error) {
	req := repotool.ScanRequest{RepoPath: repoPath}
	if o.toolRegistry == nil {
		return repotool.ScanResult{}, errors.New("tool registry is required")
	}
	var result repotool.ScanResult
	if err := o.callRegisteredTool(ctx, taskID, agentruntime.ToolRepoScan, req, &result); err != nil {
		return repotool.ScanResult{}, err
	}
	return result, nil
}

func (o *Orchestrator) scanAPI(ctx context.Context, taskID, repoPath string) (apiscantool.ScanResult, error) {
	req := apiscantool.ScanRequest{RepoPath: repoPath}
	if o.toolRegistry == nil {
		return apiscantool.ScanResult{}, errors.New("tool registry is required")
	}
	var result apiscantool.ScanResult
	if err := o.callRegisteredTool(ctx, taskID, agentruntime.ToolAPIScan, req, &result); err != nil {
		return apiscantool.ScanResult{}, err
	}
	return result, nil
}

func (o *Orchestrator) scanDB(ctx context.Context, taskID, repoPath string) (dbscantool.ScanResult, error) {
	req := dbscantool.ScanRequest{RepoPath: repoPath}
	if o.toolRegistry == nil {
		return dbscantool.ScanResult{}, errors.New("tool registry is required")
	}
	var result dbscantool.ScanResult
	if err := o.callRegisteredTool(ctx, taskID, agentruntime.ToolDBScan, req, &result); err != nil {
		return dbscantool.ScanResult{}, err
	}
	return result, nil
}

func (o *Orchestrator) callRegisteredTool(ctx context.Context, taskID, name string, input any, output any) error {
	o.emitAgent(taskID, name, agentruntime.RunStatusRunning, fmt.Sprintf("running %s", name), nil)
	recorder := agentruntime.NewTraceRecorder(taskID, o.config.Artifacts.Root, o.repo)
	_, err := recorder.RecordCall(ctx, name, input, func(ctx context.Context) (any, error) {
		err := agentruntime.CallTRPCTool(ctx, o.toolRegistry, name, input, output)
		return output, err
	})
	if err != nil {
		o.emitAgent(taskID, name, agentruntime.RunStatusFailed, err.Error(), nil)
	} else {
		o.emitAgent(taskID, name, agentruntime.RunStatusSucceeded, fmt.Sprintf("%s completed", name), output)
	}
	return err
}

func apiGraphFromScan(apiScan apiscantool.ScanResult, repoScan repotool.ScanResult) *workflow.ApiGraph {
	if len(apiScan.Endpoints) == 0 {
		return inferApiGraph(repoScan)
	}
	endpoints := make([]workflow.ApiEndpoint, 0, len(apiScan.Endpoints))
	for _, endpoint := range apiScan.Endpoints {
		endpoints = append(endpoints, workflow.ApiEndpoint{
			Method: endpoint.Method,
			Path:   endpoint.Path,
			Name:   endpoint.Name,
		})
	}
	return &workflow.ApiGraph{Endpoints: endpoints}
}

func dataModelGraphFromScan(dbScan dbscantool.ScanResult, repoScan repotool.ScanResult) *workflow.DataModelGraph {
	if len(dbScan.Models) == 0 {
		return inferDataModelGraph(repoScan)
	}
	models := make([]workflow.DataModel, 0, len(dbScan.Models))
	for _, model := range dbScan.Models {
		models = append(models, workflow.DataModel{
			Name:   model.Name,
			Fields: model.Fields,
		})
	}
	return &workflow.DataModelGraph{Models: models}
}

func inferPageGraph(scan repotool.ScanResult) *workflow.PageGraph {
	pages := []workflow.PageNode{}
	for _, file := range scan.Files {
		if file.Kind == "html-page" {
			route := "/" + strings.TrimSuffix(strings.TrimPrefix(file.Path, "static/"), filepath.Ext(file.Path))
			if strings.EqualFold(filepath.Base(file.Path), "index.html") {
				route = "/"
			}
			pages = append(pages, workflow.PageNode{ID: sanitizeID(file.Path), Name: file.Path, Route: route})
			continue
		}
		if file.Kind != "typescript-source" && file.Kind != "javascript-source" {
			continue
		}
		path := file.Path
		if strings.Contains(path, "/pages/") || strings.Contains(path, "/app/") {
			pages = append(pages, workflow.PageNode{ID: sanitizeID(path), Name: path, Route: "/"})
		}
	}
	if len(pages) == 0 && scan.Frontend {
		pages = append(pages, workflow.PageNode{ID: "home", Name: "Home", Route: "/"})
	}
	return &workflow.PageGraph{Pages: pages}
}

func inferApiGraph(scan repotool.ScanResult) *workflow.ApiGraph {
	endpoints := []workflow.ApiEndpoint{}
	for _, file := range scan.Files {
		if file.Kind != "go-source" && file.Kind != "typescript-source" && file.Kind != "javascript-source" {
			continue
		}
		path := file.Path
		if strings.Contains(path, "api") || strings.Contains(path, "handler") || strings.Contains(path, "controller") || strings.Contains(path, "router") {
			endpoints = append(endpoints, workflow.ApiEndpoint{Method: "UNKNOWN", Path: path, Name: "inferred"})
		}
	}
	return &workflow.ApiGraph{Endpoints: endpoints}
}

func inferDataModelGraph(scan repotool.ScanResult) *workflow.DataModelGraph {
	models := []workflow.DataModel{}
	for _, file := range scan.Files {
		if file.Kind == "sql-schema" || strings.Contains(file.Path, "model") || strings.Contains(file.Path, "schema") {
			models = append(models, workflow.DataModel{Name: file.Path})
		}
	}
	return &workflow.DataModelGraph{Models: models}
}

func sanitizeID(value string) string {
	value = strings.TrimSuffix(value, filepath.Ext(value))
	replacer := strings.NewReplacer("/", "_", "\\", "_", ".", "_", "-", "_")
	return replacer.Replace(value)
}

func (o *Orchestrator) scenarioPlanning(ctx context.Context, state *workflow.State, req RunRequest) error {
	state.Phase = workflow.PhaseScenarioPlanning
	state.Status = workflow.StatusRunning
	o.emitPhase(state.TaskID, state.Phase, state.Status, "planning test scenarios")
	if o.agentExecutor == nil {
		plan := fallbackScenarioPlan(state)
		if plan == nil || len(plan.Scenarios) == 0 {
			return errors.New("scenario planning returned no scenarios")
		}
		state.ScenarioPlan = plan
		o.emitAgent(state.TaskID, "scenario.plan", agentruntime.RunStatusSucceeded, "generated deterministic scenario plan", plan)
		return nil
	}
	plan, err := o.generateScenarioPlan(ctx, state, req)
	if err != nil {
		return err
	}
	if plan == nil || len(plan.Scenarios) == 0 {
		return errors.New("scenario planning returned no scenarios")
	}
	state.ScenarioPlan = plan
	return nil
}

func (o *Orchestrator) generateScenarioPlan(ctx context.Context, state *workflow.State, req RunRequest) (*workflow.ScenarioPlan, error) {
	prompt, err := buildScenarioPrompt(state, req)
	if err != nil {
		return nil, err
	}
	raw, err := o.callJSONGenerator(ctx, state.TaskID, "scenario.plan", prompt, scenarioPlanSchema())
	if err != nil {
		return nil, err
	}
	var plan workflow.ScenarioPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, fmt.Errorf("decode scenario plan: %w", err)
	}
	return normalizeScenarioPlan(plan), nil
}

func fallbackScenarioPlan(state *workflow.State) *workflow.ScenarioPlan {
	scenarios := []workflow.Scenario{}
	if state.PageGraph != nil {
		for _, page := range state.PageGraph.Pages {
			if len(scenarios) >= 3 {
				break
			}
			route := firstNonEmpty(page.Route, "/")
			name := firstNonEmpty(page.Name, page.ID, "Primary page")
			id := sanitizeScenarioID(firstNonEmpty(page.ID, name, "page-smoke"))
			scenarios = append(scenarios, workflow.Scenario{
				ID:       id,
				Name:     name + " smoke flow",
				Level:    "L0",
				Priority: len(scenarios) + 1,
				Steps: []string{
					"open " + route,
					"review primary page content",
					"exercise health probe when available",
				},
				Assertions: []string{
					"page " + route + " renders",
					"application responds without server errors",
				},
			})
		}
	}
	if len(scenarios) == 0 && state.ApiGraph != nil {
		for _, endpoint := range state.ApiGraph.Endpoints {
			if len(scenarios) >= 3 {
				break
			}
			method := firstNonEmpty(endpoint.Method, "GET")
			path := firstNonEmpty(endpoint.Path, "/")
			if method == "UNKNOWN" || path == "" {
				continue
			}
			scenarios = append(scenarios, workflow.Scenario{
				ID:       sanitizeScenarioID(method + "-" + path),
				Name:     method + " " + path + " API smoke",
				Level:    "L0",
				Priority: len(scenarios) + 1,
				Steps: []string{
					method + " " + path,
				},
				Assertions: []string{
					method + " " + path + " status is below 500",
				},
			})
		}
	}
	if len(scenarios) == 0 {
		scenarios = []workflow.Scenario{{
			ID:       "repository-smoke",
			Name:     "Repository smoke flow",
			Level:    "L0",
			Priority: 1,
			Steps: []string{
				"open application",
				"review primary user journey",
				"exercise health probe when available",
			},
			Assertions: []string{
				"page renders",
				"critical flow metadata is visible",
			},
		}}
	}
	return normalizeScenarioPlan(workflow.ScenarioPlan{Scenarios: scenarios})
}

func buildScenarioPrompt(state *workflow.State, req RunRequest) (string, error) {
	input := map[string]any{
		"projectProfile": state.ProjectProfile,
		"pageGraph":      state.PageGraph,
		"apiGraph":       state.ApiGraph,
		"dataModelGraph": state.DataModelGraph,
	}
	bytes, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	builder.WriteString("Plan 1 to 5 high-value Playwright end-to-end scenarios for this project.\n")
	builder.WriteString("Prefer stable smoke and critical-path flows that can be generated into deterministic tests.\n")
	builder.WriteString("Return JSON only.\n\n")
	builder.WriteString("Planning rules:\n")
	builder.WriteString("1. Prioritize real routes from pageGraph when available.\n")
	builder.WriteString("2. Reference real API endpoints from apiGraph when they are relevant.\n")
	builder.WriteString("3. Mention real data models from dataModelGraph when they shape user flows; data write scenarios should exercise the target application against its isolated test database, not the automation metadata database.\n")
	builder.WriteString("4. Prefer concrete user actions and observable assertions over vague goals.\n")
	builder.WriteString("5. Keep scenarios stable: avoid external dependencies, flaky waits, and speculative product behavior.\n")
	builder.WriteString("6. Use preconditions when seed data, auth, fixtures, or mocks are needed.\n\n")
	if req.Visual {
		builder.WriteString("Visual execution mode is enabled. Include at least one browser-visible UI flow when pageGraph has routes: navigate to a real page, fill visible inputs, click visible buttons, and assert visible changes. Prefer UI actions over API-only checks for the main flow.\n\n")
	}
	builder.WriteString("High-signal summaries:\n")
	builder.WriteString("Routes: " + strings.Join(pageGraphSummary(state.PageGraph), "; ") + "\n")
	builder.WriteString("APIs: " + strings.Join(apiGraphSummary(state.ApiGraph), "; ") + "\n")
	builder.WriteString("Models: " + strings.Join(dataModelGraphSummary(state.DataModelGraph), "; ") + "\n\n")
	builder.WriteString("Project context:\n")
	builder.Write(bytes)
	return builder.String(), nil
}

func scenarioPlanSchema() map[string]any {
	return map[string]any{
		"scenarios": []map[string]any{
			{
				"id":       "stable-kebab-case-id",
				"name":     "Human readable name",
				"level":    "L0",
				"priority": 1,
				"preconditions": []string{
					"authenticated demo user exists",
				},
				"steps":      []string{"action to perform"},
				"assertions": []string{"observable expectation"},
			},
		},
	}
}

func normalizeScenarioPlan(plan workflow.ScenarioPlan) *workflow.ScenarioPlan {
	out := workflow.ScenarioPlan{Scenarios: []workflow.Scenario{}}
	seen := map[string]bool{}
	for _, scenario := range plan.Scenarios {
		if len(out.Scenarios) >= 5 {
			break
		}
		id := sanitizeScenarioID(firstNonEmpty(scenario.ID, scenario.Name, "scenario"))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(scenario.Name)
		if name == "" {
			name = strings.ReplaceAll(id, "-", " ")
		}
		level := strings.TrimSpace(scenario.Level)
		if level == "" {
			level = "L0"
		}
		priority := scenario.Priority
		if priority <= 0 {
			priority = len(out.Scenarios) + 1
		}
		steps := nonEmptyStrings(scenario.Steps)
		if len(steps) == 0 {
			steps = []string{"open application"}
		}
		assertions := nonEmptyStrings(scenario.Assertions)
		if len(assertions) == 0 {
			assertions = []string{"page renders"}
		}
		out.Scenarios = append(out.Scenarios, workflow.Scenario{
			ID:            id,
			Name:          name,
			Level:         level,
			Priority:      priority,
			Preconditions: nonEmptyStrings(scenario.Preconditions),
			Steps:         steps,
			Assertions:    assertions,
		})
	}
	return &out
}

var scenarioIDPattern = regexp.MustCompile(`[^a-z0-9]+`)

func sanitizeScenarioID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = scenarioIDPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if len(value) > 64 {
		value = strings.Trim(value[:64], "-")
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func nonEmptyStrings(values []string) []string {
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func pageGraphSummary(graph *workflow.PageGraph) []string {
	if graph == nil || len(graph.Pages) == 0 {
		return []string{"none"}
	}
	out := make([]string, 0, len(graph.Pages))
	for _, page := range graph.Pages {
		label := firstNonEmpty(page.Route, page.Name, page.ID)
		if page.Name != "" && page.Route != "" && page.Name != page.Route {
			label = page.Name + " -> " + page.Route
		}
		out = append(out, label)
	}
	return out
}

func apiGraphSummary(graph *workflow.ApiGraph) []string {
	if graph == nil || len(graph.Endpoints) == 0 {
		return []string{"none"}
	}
	out := make([]string, 0, len(graph.Endpoints))
	for _, endpoint := range graph.Endpoints {
		label := strings.TrimSpace(endpoint.Method + " " + endpoint.Path)
		if endpoint.Name != "" {
			label += " (" + endpoint.Name + ")"
		}
		out = append(out, strings.TrimSpace(label))
	}
	return out
}

func dataModelGraphSummary(graph *workflow.DataModelGraph) []string {
	if graph == nil || len(graph.Models) == 0 {
		return []string{"none"}
	}
	out := make([]string, 0, len(graph.Models))
	for _, model := range graph.Models {
		label := model.Name
		if len(model.Fields) > 0 {
			label += " [" + strings.Join(model.Fields, ", ") + "]"
		}
		out = append(out, label)
	}
	return out
}

func (o *Orchestrator) testGeneration(ctx context.Context, state *workflow.State, req RunRequest) error {
	state.Phase = workflow.PhaseTestGeneration
	state.Status = workflow.StatusRunning
	o.emitPhase(state.TaskID, state.Phase, state.Status, "generating test specs")
	scenarios := []workflow.Scenario{}
	if state.ScenarioPlan != nil {
		scenarios = state.ScenarioPlan.Scenarios
	}
	testWorkspace, err := o.testWorkspace(state)
	if err != nil {
		return err
	}
	state.TestWorkspace = testWorkspace
	result, err := generatortool.NewPlaywrightGenerator().Generate(ctx, generatortool.GenerateRequest{
		ProjectRoot:    testWorkspace,
		ProjectProfile: state.ProjectProfile,
		PageGraph:      state.PageGraph,
		ApiGraph:       state.ApiGraph,
		DataModelGraph: state.DataModelGraph,
		Scenarios:      scenarios,
		BaseURL:        req.BaseURL,
		Visual:         req.Visual,
	})
	if err != nil {
		return err
	}
	state.TestArtifacts = result.Artifacts
	if o.repo != nil {
		if err := o.repo.SaveTestArtifacts(ctx, state.TaskID, state.TestArtifacts); err != nil {
			return err
		}
	}
	o.emitProgress(ProgressEvent{
		Type:    ProgressExec,
		TaskID:  state.TaskID,
		Phase:   string(state.Phase),
		Status:  string(state.Status),
		Message: "generated playwright artifacts",
		Data:    result.Artifacts,
	})
	return nil
}

func (o *Orchestrator) testExecution(ctx context.Context, state *workflow.State, req RunRequest) error {
	state.Phase = workflow.PhaseTestExecution
	state.Status = workflow.StatusRunning
	o.emitPhase(state.TaskID, state.Phase, state.Status, "running playwright tests")
	outputDir := filepath.Join(o.config.Artifacts.Root, "projects", state.ProjectID, "executions", state.TaskID)
	testWorkspace, err := o.ensureTestWorkspace(state)
	if err != nil {
		return err
	}
	result, err := playwrighttool.NewRunner(
		o.config.Runner.NodePath,
		o.config.Runner.PlaywrightRunnerDir,
		outputDir,
	).Run(ctx, playwrighttool.RunRequest{
		ProjectRoot: testWorkspace,
		SpecPattern: specPattern(req),
		OutputDir:   outputDir,
		Headed:      req.Headed,
		SlowMoMS:    req.SlowMoMS,
		OnOutput: func(stream, line string) {
			o.emitProgress(ProgressEvent{
				Type:    ProgressExec,
				TaskID:  state.TaskID,
				Phase:   string(state.Phase),
				Status:  string(state.Status),
				Message: line,
				Data: map[string]string{
					"stream": stream,
				},
			})
		},
	})
	state.ExecutionResult = &workflow.ExecutionResult{
		ExecutionID:    result.ExecutionID,
		Passed:         result.Passed && !req.ForceExecutionFailure,
		PassedCount:    boolCount(result.Passed && !req.ForceExecutionFailure),
		FailedCount:    boolCount(!result.Passed || req.ForceExecutionFailure),
		ResultPath:     result.ResultPath,
		TracePath:      result.TracePath,
		HtmlReportPath: result.HtmlReportPath,
		Mode:           result.Mode,
		StdoutPath:     result.StdoutPath,
		StderrPath:     result.StderrPath,
		LogPath:        result.LogPath,
	}
	if o.repo != nil {
		if err := o.repo.SaveExecution(ctx, state.TaskID, *state.ExecutionResult); err != nil {
			return err
		}
	}
	o.emitProgress(ProgressEvent{
		Type:    ProgressExec,
		TaskID:  state.TaskID,
		Phase:   string(state.Phase),
		Status:  string(state.Status),
		Message: "playwright execution completed",
		Data:    state.ExecutionResult,
	})
	if req.ForceExecutionFailure {
		return nil
	}
	return err
}

func (o *Orchestrator) failureDiagnosis(ctx context.Context, state *workflow.State, _ RunRequest) error {
	state.Phase = workflow.PhaseFailureDiagnosis
	state.Status = workflow.StatusRunning
	o.emitPhase(state.TaskID, state.Phase, state.Status, "diagnosing test failure")

	if o.agentExecutor == nil {
		return errors.New("agent executor is required for failure diagnosis")
	}
	diagnosis, err := o.generateDiagnosis(ctx, state)
	if err != nil {
		return err
	}
	state.DiagnosisResult = diagnosis
	return nil
}

func (o *Orchestrator) generateDiagnosis(ctx context.Context, state *workflow.State) (*workflow.DiagnosisResult, error) {
	prompt, err := buildDiagnosisPrompt(state)
	if err != nil {
		return nil, err
	}
	raw, err := o.callJSONGenerator(ctx, state.TaskID, "failure.diagnosis", prompt, diagnosisSchema())
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		DiagnosisResult *workflow.DiagnosisResult `json:"diagnosisResult"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.DiagnosisResult != nil {
		return normalizeDiagnosis(wrapped.DiagnosisResult)
	}
	var rawDiag struct {
		FailureType string                  `json:"failureType"`
		RootCause   string                  `json:"rootCause"`
		Confidence  float64                 `json:"confidence"`
		Evidence    []workflow.EvidenceItem `json:"evidence"`
		FixTarget   string                  `json:"fixTarget"`
		FixTargets  []string                `json:"fixTargets"`
		NextAction  string                  `json:"nextAction"`
	}
	if err := json.Unmarshal(raw, &rawDiag); err != nil {
		return nil, fmt.Errorf("decode diagnosis result: %w", err)
	}
	diagnosis := &workflow.DiagnosisResult{
		FailureType: rawDiag.FailureType,
		RootCause:   rawDiag.RootCause,
		Confidence:  rawDiag.Confidence,
		Evidence:    rawDiag.Evidence,
		FixTargets:  rawDiag.FixTargets,
		NextAction:  rawDiag.NextAction,
	}
	if len(diagnosis.FixTargets) == 0 && rawDiag.FixTarget != "" {
		diagnosis.FixTargets = []string{rawDiag.FixTarget}
	}
	return normalizeDiagnosis(diagnosis)
}

func buildDiagnosisPrompt(state *workflow.State) (string, error) {
	input := map[string]any{
		"projectProfile":  state.ProjectProfile,
		"testArtifacts":   state.TestArtifacts,
		"executionResult": state.ExecutionResult,
		"executionFiles":  cleanedExecutionContext(state.ExecutionResult),
	}
	bytes, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	builder.WriteString("Diagnose the Playwright execution failure.\n")
	builder.WriteString("Classify it as TEST_CODE, FIXTURE, MOCK, ENVIRONMENT, PRODUCT_BUG, or UNKNOWN.\n")
	builder.WriteString("Return JSON only.\n\n")
	builder.WriteString("Diagnosis rules:\n")
	builder.WriteString("1. If the report mentions missing Playwright browsers, missing executables, launch failures, or install commands, classify as ENVIRONMENT.\n")
	builder.WriteString("2. If the failure points to an assertion, selector, route, timing, or generated test logic inside a spec file, classify as TEST_CODE.\n")
	builder.WriteString("3. Prefer concrete evidence from report files, stderr, stdout, and failing spec locations over generic guesses.\n")
	builder.WriteString("4. Set fixTargets to the exact failing spec paths (one or more) when the failure is test-code related. If multiple files share the same root cause (e.g. generated template issue), list all affected files.\n\n")
	builder.WriteString("Failure context:\n")
	builder.Write(bytes)
	return builder.String(), nil
}

func diagnosisSchema() map[string]any {
	return map[string]any{
		"diagnosisResult": map[string]any{
			"failureType": "TEST_CODE",
			"rootCause":   "concise root cause",
			"confidence":  0.7,
			"evidence": []map[string]string{
				{"type": "execution", "summary": "what failed", "ref": "path"},
			},
			"fixTargets": []string{"relative or artifact path"},
			"nextAction": "TEST_FIXING",
		},
	}
}

func normalizeDiagnosis(diagnosis *workflow.DiagnosisResult) (*workflow.DiagnosisResult, error) {
	if diagnosis == nil {
		return nil, errors.New("diagnosis is nil")
	}
	diagnosis.FailureType = strings.ToUpper(strings.TrimSpace(diagnosis.FailureType))
	if diagnosis.FailureType == "" {
		return nil, errors.New("diagnosis failureType is required")
	}
	diagnosis.RootCause = strings.TrimSpace(diagnosis.RootCause)
	if diagnosis.RootCause == "" {
		return nil, errors.New("diagnosis rootCause is required")
	}
	if diagnosis.Confidence < 0 {
		diagnosis.Confidence = 0
	}
	if diagnosis.Confidence > 1 {
		diagnosis.Confidence = 1
	}
	for i, t := range diagnosis.FixTargets {
		diagnosis.FixTargets[i] = strings.TrimSpace(t)
	}
	diagnosis.NextAction = strings.ToUpper(strings.TrimSpace(diagnosis.NextAction))
	if diagnosis.NextAction == "" {
		if shouldFixFailureType(diagnosis.FailureType) {
			diagnosis.NextAction = "TEST_FIXING"
		} else {
			diagnosis.NextAction = "WAITING_REVIEW"
		}
	}
	diagnosis.Evidence = normalizeEvidence(diagnosis.Evidence)
	return diagnosis, nil
}

func normalizeEvidence(values []workflow.EvidenceItem) []workflow.EvidenceItem {
	out := []workflow.EvidenceItem{}
	for _, item := range values {
		item.Type = strings.TrimSpace(item.Type)
		item.Summary = strings.TrimSpace(item.Summary)
		item.Ref = strings.TrimSpace(item.Ref)
		if item.Type == "" || item.Summary == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (o *Orchestrator) testFixing(ctx context.Context, state *workflow.State, req RunRequest) error {
	state.Phase = workflow.PhaseTestFixing
	state.Status = workflow.StatusRunning
	state.RetryState.TestFixRetryCount++
	state.RetryState.TotalRetryCount++
	o.emitPhase(state.TaskID, state.Phase, state.Status, "generating test fixes")
	if state.DiagnosisResult == nil {
		return nil
	}
	if o.agentExecutor == nil {
		return errors.New("agent executor is required for test fixing")
	}
	patches, err := o.generateTestFixPatches(ctx, state, req.RepoPath)
	if err != nil {
		return err
	}
	if len(patches) == 0 {
		return errors.New("test fixing returned no patches")
	}
	state.TestFixPatches = patches
	return nil
}

func (o *Orchestrator) generateTestFixPatches(ctx context.Context, state *workflow.State, repoPath string) ([]workflow.Patch, error) {
	testWorkspace := firstNonEmpty(state.TestWorkspace, filepath.Join(repoPath, "e2e"))
	prompt, err := buildTestFixPrompt(state, testWorkspace)
	if err != nil {
		return nil, err
	}
	raw, err := o.callJSONGenerator(ctx, state.TaskID, "test.fix", prompt, testFixSchema())
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Patches []workflow.Patch `json:"patches"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, fmt.Errorf("decode test fix patches: %w", err)
	}
	patches, err := normalizePatches(wrapped.Patches)
	if err != nil {
		return nil, err
	}
	return normalizePatchTargets(testWorkspace, patches), nil
}

func buildTestFixPrompt(state *workflow.State, testWorkspace string) (string, error) {
	input := map[string]any{
		"diagnosisResult": state.DiagnosisResult,
		"testArtifacts":   state.TestArtifacts,
		"executionResult": state.ExecutionResult,
		"executionFiles":  cleanedExecutionContext(state.ExecutionResult),
		"targetFiles":     targetFileContexts(testWorkspace, state.DiagnosisResult),
		"retryState":      state.RetryState,
		"previousPatches": state.TestFixPatches,
	}
	bytes, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	builder.WriteString("Generate the smallest safe unified diff to fix generated Playwright test code only.\n")
	builder.WriteString("The generated tests live in an isolated test workspace, not in the product repository. Do not change product code. Return JSON only.\n\n")
	builder.WriteString("Patch rules:\n")
	builder.WriteString("1. Use the exact current target file content provided in targetFile.currentContent.\n")
	builder.WriteString("2. The unified diff must match real existing lines from the current file. Never use placeholder lines like old/new.\n")
	builder.WriteString("3. Only patch the failing test files named in diagnosisResult.fixTargets.\n")
	builder.WriteString("4. Keep the patch minimal and focused on the concrete failure evidence.\n")
	builder.WriteString("5. If the failure is ENVIRONMENT or there is no safe test-only fix, return an empty patches array.\n\n")
	builder.WriteString("Fix context:\n")
	builder.Write(bytes)
	return builder.String(), nil
}

func testFixSchema() map[string]any {
	return map[string]any{
		"patches": []map[string]any{
			{
				"id":         "optional-patch-id",
				"targetPath": "e2e/specs/smoke/example.spec.ts",
				"diff":       "--- a/e2e/specs/smoke/example.spec.ts\n+++ b/e2e/specs/smoke/example.spec.ts\n@@ -1,1 +1,1 @@\n-old\n+new\n",
				"riskLevel":  "LOW",
				"rationale":  "why this test-only change is safe",
			},
		},
	}
}

func normalizePatches(patches []workflow.Patch) ([]workflow.Patch, error) {
	out := []workflow.Patch{}
	for _, patch := range patches {
		patch.TargetPath = filepath.ToSlash(strings.TrimSpace(patch.TargetPath))
		patch.Diff = strings.TrimSpace(patch.Diff)
		patch.Rationale = strings.TrimSpace(patch.Rationale)
		patch.RiskLevel = strings.ToUpper(strings.TrimSpace(patch.RiskLevel))
		if patch.TargetPath == "" {
			return nil, errors.New("patch targetPath is required")
		}
		if patch.Diff == "" {
			return nil, errors.New("patch diff is required")
		}
		if !looksLikeUnifiedDiff(patch.Diff) {
			return nil, errors.New("patch diff must be a unified diff")
		}
		if looksLikePlaceholderDiff(patch.Diff) {
			return nil, errors.New("patch diff contains placeholder content")
		}
		if strings.TrimSpace(patch.ID) == "" {
			patch.ID = fmt.Sprintf("patch_%d_%d", time.Now().UnixNano(), len(out)+1)
		}
		if patch.RiskLevel == "" {
			patch.RiskLevel = "LOW"
		}
		if patch.Rationale == "" {
			patch.Rationale = "LLM generated a minimal test-only unified diff."
		}
		patch.Applied = false
		out = append(out, patch)
	}
	if len(out) == 0 {
		return nil, errors.New("no patches generated")
	}
	return out, nil
}

func normalizePatchTargets(repoPath string, patches []workflow.Patch) []workflow.Patch {
	root, err := filepath.Abs(firstNonEmpty(repoPath, "."))
	if err != nil {
		root = firstNonEmpty(repoPath, ".")
	}
	rootSlash := filepath.ToSlash(root)
	base := filepath.Base(root)
	for i := range patches {
		target := filepath.ToSlash(strings.TrimSpace(patches[i].TargetPath))
		if target == "" {
			continue
		}
		if abs, err := filepath.Abs(filepath.FromSlash(target)); err == nil {
			absSlash := filepath.ToSlash(abs)
			if rel, ok := trimPathPrefix(absSlash, rootSlash); ok {
				target = rel
			}
		}
		if rel, ok := trimPathPrefix(target, rootSlash); ok {
			target = rel
		}
		prefix := filepath.ToSlash(base) + "/"
		if strings.HasPrefix(target, prefix) {
			target = strings.TrimPrefix(target, prefix)
		}
		if idx := strings.Index(target, "e2e/"); idx > 0 {
			target = target[idx:]
		}
		patches[i].TargetPath = filepath.ToSlash(strings.TrimPrefix(target, "/"))
	}
	return patches
}

func trimPathPrefix(path, root string) (string, bool) {
	path = filepath.ToSlash(path)
	root = strings.TrimRight(filepath.ToSlash(root), "/")
	if path == root {
		return "", true
	}
	prefix := root + "/"
	if strings.HasPrefix(path, prefix) {
		return strings.TrimPrefix(path, prefix), true
	}
	return "", false
}

func looksLikeUnifiedDiff(diff string) bool {
	return strings.Contains(diff, "--- ") && strings.Contains(diff, "+++ ") && strings.Contains(diff, "@@")
}

func looksLikePlaceholderDiff(diff string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(diff, "\r\n", "\n"))
	return strings.Contains(normalized, "\n-old\n+new") || strings.Contains(normalized, "@@ -1,1 +1,1 @@\n-old\n+new")
}

func executionContext(result *workflow.ExecutionResult) map[string]any {
	if result == nil {
		return nil
	}
	return map[string]any{
		"result": readPromptFile(result.ResultPath, 16000),
		"stdout": readPromptFile(result.StdoutPath, 12000),
		"stderr": readPromptFile(result.StderrPath, 12000),
		"log":    readPromptFile(result.LogPath, 12000),
		"trace":  strings.TrimSpace(result.TracePath),
		"report": readPromptFile(playwrightReportPath(result.ResultPath), 24000),
	}
}

func cleanedExecutionContext(result *workflow.ExecutionResult) map[string]any {
	if result == nil {
		return nil
	}
	return map[string]any{
		"result": readCleanPromptFile(result.ResultPath, 12000),
		"stdout": readCleanPromptFile(result.StdoutPath, 10000),
		"stderr": readCleanPromptFile(result.StderrPath, 10000),
		"log":    readCleanPromptFile(result.LogPath, 10000),
		"trace":  strings.TrimSpace(result.TracePath),
		"report": readCleanPromptFile(playwrightReportPath(result.ResultPath), 16000),
		"cleaning": map[string]string{
			"ansi":       "removed terminal escape sequences",
			"secrets":    "redacted likely API keys and tokens",
			"noise":      "collapsed repeated lines and truncated oversized files",
			"localPaths": "shortened workspace paths when possible",
		},
	}
}

var (
	ansiEscapePattern   = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	secretPattern       = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|authorization)(["'\s:=]+)([^"'\s,}]+)`)
	longJSONLinePattern = regexp.MustCompile(`\\u001b\[[0-9;?]*[A-Za-z]`)
)

func cleanPromptText(text string, limit int) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = ansiEscapePattern.ReplaceAllString(text, "")
	text = longJSONLinePattern.ReplaceAllString(text, "")
	text = secretPattern.ReplaceAllString(text, `$1$2[REDACTED]`)
	if wd, err := os.Getwd(); err == nil {
		text = strings.ReplaceAll(text, filepath.ToSlash(wd)+"/", "")
		text = strings.ReplaceAll(text, wd+string(os.PathSeparator), "")
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	last := ""
	repeat := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line == last {
			repeat++
			if repeat > 2 {
				continue
			}
		} else {
			last = line
			repeat = 0
		}
		out = append(out, line)
	}
	cleaned := strings.Join(out, "\n")
	if limit > 0 && len(cleaned) > limit {
		return cleaned[:limit] + "\n...[cleaned and truncated]"
	}
	return cleaned
}

func readCleanPromptFile(path string, limit int) string {
	return cleanPromptText(readPromptFile(path, limit*2), limit)
}

func targetFileContexts(repoPath string, diagnosis *workflow.DiagnosisResult) map[string]any {
	if diagnosis == nil || len(diagnosis.FixTargets) == 0 {
		return nil
	}
	root, err := filepath.Abs(firstNonEmpty(repoPath, "."))
	if err != nil {
		root = firstNonEmpty(repoPath, ".")
	}
	targets := []map[string]any{}
	for _, fixTarget := range diagnosis.FixTargets {
		if strings.TrimSpace(fixTarget) == "" {
			continue
		}
		rel := normalizePatchTargets(root, []workflow.Patch{{TargetPath: fixTarget}})[0].TargetPath
		target := filepath.Join(root, filepath.FromSlash(rel))
		abs, _ := filepath.Abs(target)
		targets = append(targets, map[string]any{
			"path":           abs,
			"currentContent": readPromptFile(abs, 24000),
		})
	}
	if len(targets) == 0 {
		return nil
	}
	return map[string]any{"targetFiles": targets}
}

type ExecutionArtifactSummary struct {
	Trace         *FileSummary             `json:"trace,omitempty"`
	Report        *FileSummary             `json:"report,omitempty"`
	HtmlReport    *FileSummary             `json:"htmlReport,omitempty"`
	Stdout        *FileSummary             `json:"stdout,omitempty"`
	Stderr        *FileSummary             `json:"stderr,omitempty"`
	Log           *FileSummary             `json:"log,omitempty"`
	StdoutSnippet string                   `json:"stdoutSnippet,omitempty"`
	StderrSnippet string                   `json:"stderrSnippet,omitempty"`
	LogSnippet    string                   `json:"logSnippet,omitempty"`
	Screenshots   []FileSummary            `json:"screenshots,omitempty"`
	ReportSummary *PlaywrightReportSummary `json:"reportSummary,omitempty"`
}

type FileSummary struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Exists bool   `json:"exists"`
}

type PlaywrightReportSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

func (o *Orchestrator) SummarizeExecutionArtifacts(state workflow.State) ExecutionArtifactSummary {
	result := state.ExecutionResult
	if result == nil {
		return ExecutionArtifactSummary{}
	}
	summary := ExecutionArtifactSummary{}
	if result.TracePath != "" {
		summary.Trace = fileInfo(result.TracePath)
	}
	if result.HtmlReportPath != "" {
		summary.HtmlReport = fileInfo(result.HtmlReportPath)
	}
	if result.StdoutPath != "" {
		summary.Stdout = fileInfo(result.StdoutPath)
		summary.StdoutSnippet = readSnippet(result.StdoutPath, 2000)
	}
	if result.StderrPath != "" {
		summary.Stderr = fileInfo(result.StderrPath)
		summary.StderrSnippet = readSnippet(result.StderrPath, 2000)
	}
	if result.LogPath != "" {
		summary.Log = fileInfo(result.LogPath)
		summary.LogSnippet = readSnippet(result.LogPath, 2000)
	}
	reportPath := playwrightReportPath(result.ResultPath)
	if reportPath != "" {
		summary.Report = fileInfo(reportPath)
		if data, err := os.ReadFile(reportPath); err == nil {
			var report struct {
				Stats *struct {
					Expected   int `json:"expected"`
					Unexpected int `json:"unexpected"`
					Skipped    int `json:"skipped"`
					Flaky      int `json:"flaky"`
				} `json:"stats"`
				Suites []struct {
					Specs []struct {
						Tests []struct {
							Results []struct {
								Status string `json:"status"`
							} `json:"results"`
						} `json:"tests"`
					} `json:"specs"`
				} `json:"suites"`
			}
			if json.Unmarshal(data, &report) == nil {
				ps := &PlaywrightReportSummary{}
				if report.Stats != nil {
					ps.Total = report.Stats.Expected + report.Stats.Unexpected + report.Stats.Skipped + report.Stats.Flaky
					ps.Passed = report.Stats.Expected
					ps.Failed = report.Stats.Unexpected
					ps.Skipped = report.Stats.Skipped
				}
				// Fallback: count from nested suites if stats unavailable
				if ps.Total == 0 {
					for _, suite := range report.Suites {
						for _, spec := range suite.Specs {
							for _, test := range spec.Tests {
								for _, r := range test.Results {
									ps.Total++
									switch r.Status {
									case "passed", "expected":
										ps.Passed++
									case "failed", "unexpected":
										ps.Failed++
									case "skipped":
										ps.Skipped++
									}
								}
							}
						}
					}
				}
				summary.ReportSummary = ps
			}
		}
	}
	screenshots, _ := filepath.Glob(filepath.Join(o.config.Artifacts.Root, "projects", state.ProjectID, "executions", state.TaskID, "*.png"))
	for _, s := range screenshots {
		summary.Screenshots = append(summary.Screenshots, *fileInfo(s))
	}
	return summary
}

func fileInfo(path string) *FileSummary {
	info, err := os.Stat(path)
	if err != nil {
		return &FileSummary{Path: path, Exists: false}
	}
	return &FileSummary{Path: path, Size: info.Size(), Exists: true}
}

func readSnippet(path string, limit int) string {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(bytes)
	if limit > 0 && len(text) > limit {
		return text[:limit] + "\n...[truncated]"
	}
	return text
}

func playwrightReportPath(resultPath string) string {
	if strings.TrimSpace(resultPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(resultPath), "playwright-report.json")
}

func readPromptFile(path string, limit int) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(bytes)
	if limit > 0 && len(text) > limit {
		return text[:limit] + "\n...[truncated]"
	}
	return text
}

func (o *Orchestrator) callJSONGenerator(ctx context.Context, taskID, name, prompt string, schema any) ([]byte, error) {
	o.emitAgent(taskID, name, agentruntime.RunStatusRunning, fmt.Sprintf("calling %s", name), nil)
	recorder := agentruntime.NewTraceRecorder(taskID, o.config.Artifacts.Root, o.repo)
	input := map[string]any{"prompt": prompt, "schema": schema}
	var outputBytes []byte
	_, err := recorder.RecordCall(ctx, name, input, func(ctx context.Context) (any, error) {
		bytes, err := o.agentExecutor.ExecuteJSON(ctx, name, prompt, schema)
		outputBytes = bytes
		return json.RawMessage(bytes), err
	})
	if err != nil {
		o.emitAgent(taskID, name, agentruntime.RunStatusFailed, err.Error(), nil)
	} else {
		o.emitAgent(taskID, name, agentruntime.RunStatusSucceeded, fmt.Sprintf("%s completed", name), json.RawMessage(outputBytes))
	}
	return outputBytes, err
}

func (o *Orchestrator) reviewGuard(ctx context.Context, state *workflow.State, _ RunRequest) error {
	state.Phase = workflow.PhaseReviewGuard
	state.Status = workflow.StatusRunning
	o.emitPhase(state.TaskID, state.Phase, state.Status, "reviewing patches")
	decision, err := guardtool.NewReviewGuard(o.config.Patch.AllowedPaths, o.config.Patch.BlockedPaths).Review(ctx, state.TestFixPatches)
	if err != nil {
		return err
	}
	state.GuardState = workflow.GuardState{
		RiskLevel:        decision.RiskLevel,
		PatchAllowed:     decision.PatchAllowed,
		RerunAllowed:     decision.RerunAllowed,
		NeedsHumanReview: decision.NeedsHumanReview,
	}
	if !decision.PatchAllowed && len(decision.Reasons) > 0 {
		state.GuardState.BlockedReason = strings.Join(decision.Reasons, "; ")
	}
	for i := range state.TestFixPatches {
		state.TestFixPatches[i].RiskLevel = decision.RiskLevel
	}
	return nil
}

func (o *Orchestrator) applyPatch(ctx context.Context, state *workflow.State, _ RunRequest) error {
	testWorkspace, err := o.ensureTestWorkspace(state)
	if err != nil {
		return err
	}
	result, err := patchtool.NewApplier(o.config.Patch.AllowedPaths, o.config.Patch.BlockedPaths).Apply(ctx, patchtool.ApplyRequest{
		ProjectRoot: testWorkspace,
		Patches:     state.TestFixPatches,
	})
	if err != nil {
		return err
	}
	state.TestFixPatches = result.Patches
	return nil
}

func (o *Orchestrator) reRun(ctx context.Context, state *workflow.State, req RunRequest) error {
	state.Phase = workflow.PhaseReRun
	state.Status = workflow.StatusRunning
	state.RetryState.ExecutionRetryCount++
	o.emitPhase(state.TaskID, state.Phase, state.Status, "re-running tests")
	outputDir := filepath.Join(o.config.Artifacts.Root, "projects", state.ProjectID, "executions", state.TaskID+"-rerun")
	testWorkspace, err := o.ensureTestWorkspace(state)
	if err != nil {
		return err
	}
	result, err := playwrighttool.NewRunner(
		o.config.Runner.NodePath,
		o.config.Runner.PlaywrightRunnerDir,
		outputDir,
	).Run(ctx, playwrighttool.RunRequest{
		ProjectRoot: testWorkspace,
		SpecPattern: specPattern(req),
		OutputDir:   outputDir,
		Headed:      req.Headed,
		SlowMoMS:    req.SlowMoMS,
		OnOutput: func(stream, line string) {
			o.emitProgress(ProgressEvent{
				Type:    ProgressExec,
				TaskID:  state.TaskID,
				Phase:   string(state.Phase),
				Status:  string(state.Status),
				Message: line,
				Data: map[string]string{
					"stream": stream,
				},
			})
		},
	})
	state.ExecutionResult = &workflow.ExecutionResult{
		ExecutionID:    result.ExecutionID,
		Passed:         result.Passed,
		PassedCount:    boolCount(result.Passed),
		FailedCount:    boolCount(!result.Passed),
		ResultPath:     result.ResultPath,
		TracePath:      result.TracePath,
		HtmlReportPath: result.HtmlReportPath,
		Mode:           result.Mode,
		StdoutPath:     result.StdoutPath,
		StderrPath:     result.StderrPath,
		LogPath:        result.LogPath,
	}
	if o.repo != nil {
		if err := o.repo.SaveExecution(ctx, state.TaskID, *state.ExecutionResult); err != nil {
			return err
		}
	}
	return err
}

func (o *Orchestrator) archive(_ context.Context, state *workflow.State, _ RunRequest) error {
	state.Phase = workflow.PhaseDone
	o.emitPhase(state.TaskID, state.Phase, state.Status, "archiving results")
	if state.ExecutionResult != nil && !state.ExecutionResult.Passed {
		state.Status = workflow.StatusPartialSucceeded
	} else if state.Status != workflow.StatusPartialSucceeded {
		state.Status = workflow.StatusSucceeded
	}
	return nil
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func specPattern(req RunRequest) string {
	if req.Visual {
		return "e2e/specs/visual/**/*.spec.ts"
	}
	return "e2e/specs/**/*.spec.ts"
}

func (o *Orchestrator) testWorkspace(state *workflow.State) (string, error) {
	if strings.TrimSpace(state.TestWorkspace) != "" {
		return filepath.Abs(state.TestWorkspace)
	}
	root := filepath.Join(o.config.Artifacts.Root, "projects", state.ProjectID, "generated-tests", state.TaskID)
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	state.TestWorkspace = abs
	return abs, nil
}

func (o *Orchestrator) ensureTestWorkspace(state *workflow.State) (string, error) {
	root, err := o.testWorkspace(state)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(root, "e2e", "playwright.config.ts")); err != nil {
		return "", fmt.Errorf("generated test workspace is not ready: %w", err)
	}
	return root, nil
}
