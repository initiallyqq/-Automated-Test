package apiscan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScannerDetectsHTTPRoutes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "internal", "api", "server.go"), `package api

func routes(mux *ServeMux) {
  mux.HandleFunc("GET /api/v1/health", health)
  mux.HandleFunc("/legacy", legacy)
  mux.HandleFunc("/api/notes", handleNotes)
}

func handleNotes(w http.ResponseWriter, r *http.Request) {
  switch r.Method {
  case http.MethodGet:
  case http.MethodPost:
  }
}
`)
	writeFile(t, filepath.Join(dir, "src", "server.ts"), `app.post("/api/login", login); router.get('/users/:id', showUser);`)
	writeFile(t, filepath.Join(dir, "src", "headers.ts"), `request.headers.get("Authorization");`)

	result, err := NewScanner().Scan(context.Background(), ScanRequest{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}

	assertEndpoint(t, result.Endpoints, "GET", "/api/v1/health")
	assertEndpoint(t, result.Endpoints, "UNKNOWN", "/legacy")
	assertEndpoint(t, result.Endpoints, "GET", "/api/notes")
	assertEndpoint(t, result.Endpoints, "POST", "/api/notes")
	assertEndpoint(t, result.Endpoints, "POST", "/api/login")
	assertEndpoint(t, result.Endpoints, "GET", "/users/:id")
	assertNoEndpoint(t, result.Endpoints, "GET", "/Authorization")
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

func assertEndpoint(t *testing.T, endpoints []Endpoint, method, path string) {
	t.Helper()
	for _, endpoint := range endpoints {
		if endpoint.Method == method && endpoint.Path == path {
			return
		}
	}
	t.Fatalf("expected endpoint %s %s in %+v", method, path, endpoints)
}

func assertNoEndpoint(t *testing.T, endpoints []Endpoint, method, path string) {
	t.Helper()
	for _, endpoint := range endpoints {
		if endpoint.Method == method && endpoint.Path == path {
			t.Fatalf("did not expect endpoint %s %s in %+v", method, path, endpoints)
		}
	}
}
