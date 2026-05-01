package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Tool interface {
	Name() string
	Description() string
	InputSchema() any
	Invoke(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

type Registry struct {
	tools map[string]Tool
}

type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema,omitempty"`
}

type RunRecord struct {
	ID             string     `json:"id"`
	TaskID         string     `json:"taskId"`
	AgentName      string     `json:"agentName"`
	InputSummary   string     `json:"inputSummary,omitempty"`
	OutputSummary  string     `json:"outputSummary,omitempty"`
	InputJSONPath  string     `json:"inputJsonPath,omitempty"`
	OutputJSONPath string     `json:"outputJsonPath,omitempty"`
	Status         string     `json:"status"`
	Error          string     `json:"error,omitempty"`
	StartedAt      time.Time  `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
}

const (
	RunStatusRunning   = "RUNNING"
	RunStatusSucceeded = "SUCCEEDED"
	RunStatusFailed    = "FAILED"
)

type AgentExecutor interface {
	ExecuteJSON(ctx context.Context, agentName, prompt string, schema any) ([]byte, error)
}

type JSONExecutor interface {
	GenerateJSON(ctx context.Context, prompt string, schema any) ([]byte, error)
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(tool Tool) error {
	if tool == nil {
		return errors.New("tool is nil")
	}
	name := tool.Name()
	if name == "" {
		return errors.New("tool name is required")
	}
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool already registered: %s", name)
	}
	r.tools[name] = tool
	return nil
}

func (r *Registry) Call(ctx context.Context, name string, input any, output any) error {
	if r == nil {
		return errors.New("tool registry is nil")
	}
	tool, ok := r.tools[name]
	if !ok {
		return fmt.Errorf("tool is not registered: %s", name)
	}
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal tool input: %w", err)
	}
	outputBytes, err := tool.Invoke(ctx, inputBytes)
	if err != nil {
		return err
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(outputBytes, output); err != nil {
		return fmt.Errorf("decode tool output: %w", err)
	}
	return nil
}

func (r *Registry) List() []ToolInfo {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ToolInfo, 0, len(names))
	for _, name := range names {
		tool := r.tools[name]
		out = append(out, ToolInfo{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		})
	}
	return out
}

type JSONTool[In any, Out any] struct {
	name        string
	description string
	inputSchema any
	handler     func(context.Context, In) (Out, error)
}

func NewJSONTool[In any, Out any](name, description string, inputSchema any, handler func(context.Context, In) (Out, error)) *JSONTool[In, Out] {
	return &JSONTool[In, Out]{
		name:        name,
		description: description,
		inputSchema: inputSchema,
		handler:     handler,
	}
}

func (t *JSONTool[In, Out]) Name() string {
	return t.name
}

func (t *JSONTool[In, Out]) Description() string {
	return t.description
}

func (t *JSONTool[In, Out]) InputSchema() any {
	return t.inputSchema
}

func (t *JSONTool[In, Out]) Invoke(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	if t == nil || t.handler == nil {
		return nil, errors.New("tool handler is nil")
	}
	var decoded In
	if err := json.Unmarshal(input, &decoded); err != nil {
		return nil, fmt.Errorf("decode tool input for %s: %w", t.name, err)
	}
	output, err := t.handler(ctx, decoded)
	if err != nil {
		return nil, err
	}
	outputBytes, err := json.Marshal(output)
	if err != nil {
		return nil, fmt.Errorf("encode tool output for %s: %w", t.name, err)
	}
	return outputBytes, nil
}

type RunPersister interface {
	SaveAgentRun(ctx context.Context, record RunRecord) error
}

type TraceRecorder struct {
	taskID       string
	artifactRoot string
	persister    RunPersister
}

func NewTraceRecorder(taskID, artifactRoot string, persister RunPersister) *TraceRecorder {
	return &TraceRecorder{
		taskID:       taskID,
		artifactRoot: artifactRoot,
		persister:    persister,
	}
}

func (r *TraceRecorder) RecordCall(ctx context.Context, agentName string, input any, call func(ctx context.Context) (any, error)) (RunRecord, error) {
	startedAt := time.Now().UTC()
	record := RunRecord{
		ID:           fmt.Sprintf("agent_%d", startedAt.UnixNano()),
		TaskID:       r.taskID,
		AgentName:    agentName,
		InputSummary: jsonSummary(input),
		Status:       RunStatusRunning,
		StartedAt:    startedAt,
	}
	record.InputJSONPath = r.writeArtifact(agentName, startedAt, "input", input)

	output, callErr := call(ctx)

	finishedAt := time.Now().UTC()
	record.FinishedAt = &finishedAt
	if callErr != nil {
		record.Status = RunStatusFailed
		record.Error = callErr.Error()
	} else {
		record.Status = RunStatusSucceeded
		record.OutputSummary = jsonSummary(output)
		record.OutputJSONPath = r.writeArtifact(agentName, finishedAt, "output", output)
	}

	if r.persister != nil {
		_ = r.persister.SaveAgentRun(ctx, record)
	}
	return record, callErr
}

func (r *TraceRecorder) writeArtifact(agentName string, timestamp time.Time, suffix string, value any) string {
	dir := filepath.Join(r.artifactRoot, "agent-runs", r.taskID)
	_ = os.MkdirAll(dir, 0o755)
	filename := fmt.Sprintf("%s_%s_%d.json", agentName, suffix, timestamp.UnixNano())
	path := filepath.Join(dir, filename)
	bytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return ""
	}
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		return ""
	}
	return path
}

func jsonSummary(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		if len(v) > 200 {
			return v[:200] + "..."
		}
		return v
	case []byte:
		if len(v) > 200 {
			return string(v[:200]) + "..."
		}
		return string(v)
	case json.RawMessage:
		if len(v) > 200 {
			return string(v[:200]) + "..."
		}
		return string(v)
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		if len(bytes) > 200 {
			return string(bytes[:200]) + "..."
		}
		return string(bytes)
	}
}
