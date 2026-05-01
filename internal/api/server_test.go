package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func assertTool(t *testing.T, tools []agentruntime.ToolInfo, name string) {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return
		}
	}
	t.Fatalf("expected tool %q in %+v", name, tools)
}
