package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQwenClientGenerateJSON(t *testing.T) {
	var captured struct {
		Model          string              `json:"model"`
		Messages       []map[string]string `json:"messages"`
		ResponseFormat map[string]string   `json:"response_format"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"scenarios\":[{\"id\":\"smoke\"}]}"}}]}`))
	}))
	defer server.Close()

	client, err := NewQwenClientWithOptions("test-key", "qwen-plus", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.GenerateJSON(context.Background(), "plan scenarios", map[string]any{"scenarios": []any{}})
	if err != nil {
		t.Fatal(err)
	}

	if !json.Valid(result) {
		t.Fatalf("expected valid json: %s", result)
	}
	if captured.Model != "qwen-plus" {
		t.Fatalf("unexpected model: %s", captured.Model)
	}
	if captured.ResponseFormat["type"] != "json_object" {
		t.Fatalf("expected json response format, got %+v", captured.ResponseFormat)
	}
	if len(captured.Messages) != 2 || !strings.Contains(captured.Messages[1]["content"], "plan scenarios") {
		t.Fatalf("unexpected messages: %+v", captured.Messages)
	}
}

func TestQwenClientGenerateJSONRejectsNonJSONContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"not json"}}]}`))
	}))
	defer server.Close()

	client, err := NewQwenClientWithOptions("test-key", "qwen-plus", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GenerateJSON(context.Background(), "plan scenarios", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "non-json") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQwenClientGenerateJSONReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	client, err := NewQwenClientWithOptions("test-key", "qwen-plus", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GenerateJSON(context.Background(), "plan scenarios", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status=401") {
		t.Fatalf("unexpected error: %v", err)
	}
}
