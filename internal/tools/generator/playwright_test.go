package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"automated-test/internal/domain/workflow"
)

func TestPlaywrightGeneratorWritesSmokeAssets(t *testing.T) {
	dir := t.TempDir()
	result, err := NewPlaywrightGenerator().Generate(context.Background(), GenerateRequest{
		ProjectRoot: dir,
		ProjectProfile: &workflow.ProjectProfile{
			ProjectType: "fullstack",
			Frameworks:  []string{"react", "gin"},
			PackageTool: "npm",
		},
		PageGraph: &workflow.PageGraph{
			Pages: []workflow.PageNode{{ID: "dashboard", Name: "Dashboard", Route: "/dashboard"}},
		},
		ApiGraph: &workflow.ApiGraph{
			Endpoints: []workflow.ApiEndpoint{
				{Method: "GET", Path: "/api/v1/health", Name: "health"},
				{Method: "GET", Path: "/api/users", Name: "users"},
				{Method: "POST", Path: "/api/users", Name: "users"},
			},
		},
		DataModelGraph: &workflow.DataModelGraph{
			Models: []workflow.DataModel{{Name: "users", Fields: []string{"id", "email"}}},
		},
		Scenarios: []workflow.Scenario{
			{
				ID:            "fullstack-smoke",
				Name:          "Fullstack smoke flow",
				Preconditions: []string{"seed demo user"},
				Steps:         []string{"open application", "review dashboard", "submit primary action"},
				Assertions:    []string{"dashboard is visible", "primary action succeeds"},
			},
		},
		BaseURL: "http://127.0.0.1:3000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(result.Artifacts))
	}
	assertFileExists(t, filepath.Join(dir, "e2e", "playwright.config.ts"))
	specPath := filepath.Join(dir, "e2e", "specs", "smoke", "fullstack-smoke.spec.ts")
	assertFileExists(t, specPath)
	content, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(content)
	assertContains(t, spec, "seed a deterministic app shell for the generated scenario")
	assertContains(t, spec, "review dashboard")
	assertContains(t, spec, "primary action succeeds")
	assertContains(t, spec, "scenario.preconditions")
	assertContains(t, spec, "scenario.projectType")
	assertContains(t, spec, "scenario.apis")
	assertContains(t, spec, "scenario.models")
	assertContains(t, spec, "react")
	assertContains(t, spec, "/api/v1/health")
	assertContains(t, spec, "Data write via app API: POST /api/users")
	assertContains(t, spec, "samplePayloadFor(\"POST\", path)")
	assertContains(t, spec, "created data should be visible through the application read API")
	if strings.Contains(spec, "/api/notes") {
		t.Fatalf("expected generic API generation without notes-specific paths:\n%s", spec)
	}
}

func TestPlaywrightGeneratorDerivesFallbackScenarioFromPageGraph(t *testing.T) {
	dir := t.TempDir()
	result, err := NewPlaywrightGenerator().Generate(context.Background(), GenerateRequest{
		ProjectRoot: dir,
		PageGraph: &workflow.PageGraph{
			Pages: []workflow.PageNode{{ID: "home-page", Name: "Home", Route: "/"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(result.Artifacts))
	}
	specPath := filepath.Join(dir, "e2e", "specs", "smoke", "home-page.spec.ts")
	assertFileExists(t, specPath)
	content, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	spec := string(content)
	assertContains(t, spec, `route: "/"`)
	assertContains(t, spec, "open /")
}

func TestPlaywrightGeneratorRemovesStaleSmokeSpecs(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "e2e", "specs", "smoke", "old-flow.spec.ts")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := NewPlaywrightGenerator().Generate(context.Background(), GenerateRequest{
		ProjectRoot: dir,
		Scenarios: []workflow.Scenario{{
			ID:         "fresh-flow",
			Name:       "Fresh flow",
			Steps:      []string{"open application"},
			Assertions: []string{"page renders"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected stale spec to be removed, got %v", err)
	}
	assertFileExists(t, filepath.Join(dir, "e2e", "specs", "smoke", "fresh-flow.spec.ts"))
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
}

func assertContains(t *testing.T, value, want string) {
	t.Helper()
	if !strings.Contains(value, want) {
		t.Fatalf("expected %q in:\n%s", want, value)
	}
}
