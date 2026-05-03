package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"automated-test/internal/app/orchestrator"
	"automated-test/internal/config"
	"automated-test/internal/infra/agentruntime"
)

func TestListTools(t *testing.T) {
	server := NewServer(config.Default())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tools", nil)
	rec := httptest.NewRecorder()

	server.listTools(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var tools []agentruntime.ToolInfo
	if err := json.NewDecoder(rec.Body).Decode(&tools); err != nil {
		t.Fatal(err)
	}
	assertTool(t, tools, agentruntime.ToolRepoScan)
	assertTool(t, tools, agentruntime.ToolAPIScan)
	assertTool(t, tools, agentruntime.ToolDBScan)
}

func TestApplyTargetConfigDefaultsUsesBaseURLWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".autotest.yaml"), []byte("base_url: http://127.0.0.1:3000\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := orchestrator.RunRequest{RepoPath: dir}
	if err := applyTargetConfigDefaults(&req); err != nil {
		t.Fatal(err)
	}
	if req.BaseURL != "http://127.0.0.1:3000" {
		t.Fatalf("expected base url from target config, got %q", req.BaseURL)
	}
}

func TestApplyTargetConfigDefaultsKeepsExplicitBaseURL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".autotest.yaml"), []byte("base_url: http://127.0.0.1:3000\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := orchestrator.RunRequest{RepoPath: dir, BaseURL: "http://127.0.0.1:4000"}
	if err := applyTargetConfigDefaults(&req); err != nil {
		t.Fatal(err)
	}
	if req.BaseURL != "http://127.0.0.1:4000" {
		t.Fatalf("expected explicit base url to win, got %q", req.BaseURL)
	}
}

func assertTool(t *testing.T, tools []agentruntime.ToolInfo, name string) {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return
		}
	}
	t.Fatalf("expected tool %q in %+v", name, tools)
}
