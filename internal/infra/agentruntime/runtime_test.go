package agentruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apiscantool "automated-test/internal/tools/apiscan"
	dbscantool "automated-test/internal/tools/dbscan"
	repotool "automated-test/internal/tools/repo"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

func TestDefaultToolRegistryRegistersProjectAnalysisTools(t *testing.T) {
	registry, err := NewDefaultToolRegistry()
	if err != nil {
		t.Fatal(err)
	}

	tools := registry.List()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %+v", tools)
	}
	assertTool(t, tools, ToolRepoScan)
	assertTool(t, tools, ToolAPIScan)
	assertTool(t, tools, ToolDBScan)
}

func TestDefaultToolRegistryCallsScanners(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module sample\n\ngo 1.22\n")
	writeFile(t, filepath.Join(dir, "package.json"), `{"dependencies":{"react":"^18.0.0"}}`)
	writeFile(t, filepath.Join(dir, "internal", "api", "server.go"), `package api

func routes(mux interface{ HandleFunc(string, any) }) {
  mux.HandleFunc("GET /api/v1/health", nil)
}
`)
	writeFile(t, filepath.Join(dir, "schema.sql"), "create table users(id text primary key, email text not null);\n")

	registry, err := NewDefaultToolRegistry()
	if err != nil {
		t.Fatal(err)
	}

	var repoResult repotool.ScanResult
	if err := registry.Call(context.Background(), ToolRepoScan, repotool.ScanRequest{RepoPath: dir}, &repoResult); err != nil {
		t.Fatal(err)
	}
	if !repoResult.Frontend || !repoResult.Backend {
		t.Fatalf("expected fullstack repo result, got %+v", repoResult)
	}

	var apiResult apiscantool.ScanResult
	if err := registry.Call(context.Background(), ToolAPIScan, apiscantool.ScanRequest{RepoPath: dir}, &apiResult); err != nil {
		t.Fatal(err)
	}
	if len(apiResult.Endpoints) != 1 || apiResult.Endpoints[0].Path != "/api/v1/health" {
		t.Fatalf("expected health endpoint, got %+v", apiResult)
	}

	var dbResult dbscantool.ScanResult
	if err := registry.Call(context.Background(), ToolDBScan, dbscantool.ScanRequest{RepoPath: dir}, &dbResult); err != nil {
		t.Fatal(err)
	}
	if len(dbResult.Models) != 1 || dbResult.Models[0].Name != "users" {
		t.Fatalf("expected users model, got %+v", dbResult)
	}
}

func TestBuildTRPCToolsSanitizesNames(t *testing.T) {
	registry, err := NewDefaultToolRegistry()
	if err != nil {
		t.Fatal(err)
	}
	tools := buildTRPCTools(registry)
	if len(tools) != 3 {
		t.Fatalf("expected 3 trpc tools, got %d", len(tools))
	}
	names := []string{}
	for _, tool := range tools {
		names = append(names, tool.Declaration().Name)
	}
	if !containsToolName(names, "repo_scan") || !containsToolName(names, "api_scan") || !containsToolName(names, "db_scan") {
		t.Fatalf("expected sanitized tool names, got %+v", names)
	}
}

func TestJSONExecutorModelGenerateContent(t *testing.T) {
	generator := &fakeJSONExecutor{
		response: []byte(`{"ok":true}`),
	}
	model := &JSONExecutorModel{name: "json-model", generator: generator}
	req := &trpcRequest{
		Messages: []trpcMessage{
			{Role: "system", Content: "Return structured output."},
			{Role: "user", Content: "Plan scenarios"},
		},
		StructuredOutput: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ok": map[string]any{"type": "boolean"},
			},
		},
	}
	output, err := model.generate(req)
	if err != nil {
		t.Fatal(err)
	}
	if output != `{"ok":true}` {
		t.Fatalf("unexpected output: %s", output)
	}
	if generator.prompt == "" || !strings.Contains(generator.prompt, "USER:\nPlan scenarios") {
		t.Fatalf("expected flattened prompt, got %q", generator.prompt)
	}
	if generator.schema == nil {
		t.Fatal("expected structured output schema")
	}
}

func TestTRPCAgentExecutorExecuteJSON(t *testing.T) {
	executor, err := NewTRPCAgentExecutorFromJSONExecutor("qwen-plus", &fakeJSONExecutor{
		response: []byte(`{"ok":true}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	output, err := executor.ExecuteJSON(context.Background(), "scenario.plan", "prompt", map[string]any{"shape": "object"})
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != `{"ok":true}` {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestTRPCAgentExecutorRequiresModel(t *testing.T) {
	_, err := (*TrPCAgentExecutor)(nil).ExecuteJSON(context.Background(), "scenario.plan", "prompt", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func assertTool(t *testing.T, tools []ToolInfo, name string) {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return
		}
	}
	t.Fatalf("expected tool %q in %+v", name, tools)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type fakeJSONExecutor struct {
	response []byte
	err      error
	prompt   string
	schema   any
}

func (f *fakeJSONExecutor) GenerateJSON(_ context.Context, prompt string, schema any) ([]byte, error) {
	f.prompt = prompt
	f.schema = schema
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

type trpcRequest struct {
	Messages         []trpcMessage
	StructuredOutput map[string]any
}

type trpcMessage struct {
	Role    string
	Content string
}

func (m *JSONExecutorModel) generate(req *trpcRequest) (string, error) {
	messages := make([]trpcmodel.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		messages = append(messages, trpcmodel.Message{
			Role:    trpcmodel.Role(msg.Role),
			Content: msg.Content,
		})
	}
	modelReq := &trpcmodel.Request{Messages: messages}
	if req.StructuredOutput != nil {
		modelReq.StructuredOutput = &trpcmodel.StructuredOutput{
			Type: trpcmodel.StructuredOutputJSONSchema,
			JSONSchema: &trpcmodel.JSONSchemaConfig{
				Name:   "runtime_output",
				Schema: req.StructuredOutput,
				Strict: true,
			},
		}
	}
	ch, err := m.GenerateContent(context.Background(), modelReq)
	if err != nil {
		return "", err
	}
	for res := range ch {
		if len(res.Choices) > 0 {
			return res.Choices[0].Message.Content, nil
		}
	}
	return "", nil
}

func containsToolName(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
