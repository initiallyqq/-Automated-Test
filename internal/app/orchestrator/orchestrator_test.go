package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"automated-test/internal/config"
	"automated-test/internal/domain/workflow"
	"automated-test/internal/infra/agentruntime"
	"automated-test/internal/infra/sqlite"
)

func TestOrchestratorRunSuccess(t *testing.T) {
	projectRoot := makeTempProject(t)
	cfg := testConfig(t)
	cfg.Artifacts.Root = filepath.Join(t.TempDir(), "artifacts")

	result, err := New(Options{Config: cfg, JSONGenerator: scenarioGenerator()}).Run(context.Background(), RunRequest{
		ProjectID:             "project-success",
		RepoPath:              projectRoot,
		ForceExecutionFailure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != workflow.StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", result.Status)
	}
	if result.State.ExecutionResult == nil || !result.State.ExecutionResult.Passed {
		t.Fatalf("expected passed execution, got %+v", result.State.ExecutionResult)
	}
	if len(result.State.TestArtifacts) == 0 {
		t.Fatal("expected generated artifacts")
	}
	if result.State.ApiGraph == nil || len(result.State.ApiGraph.Endpoints) == 0 {
		t.Fatalf("expected scanned api endpoints, got %+v", result.State.ApiGraph)
	}
	if result.State.DataModelGraph == nil || len(result.State.DataModelGraph.Models) == 0 {
		t.Fatalf("expected scanned data models, got %+v", result.State.DataModelGraph)
	}
}

func TestOrchestratorPersistsProjectAnalysisToolRuns(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeTempProject(t)
	cfg := testConfig(t)
	cfg.Artifacts.Root = filepath.Join(t.TempDir(), "artifacts")
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "autotest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewRepository(db)

	result, err := New(Options{Config: cfg, Repository: repo, JSONGenerator: scenarioGenerator()}).Run(ctx, RunRequest{
		ProjectID:             "project-agent-runs",
		RepoPath:              projectRoot,
		ForceExecutionFailure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := repo.GetAgentRuns(ctx, result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	assertAgentRun(t, runs, agentruntime.ToolRepoScan)
	assertAgentRun(t, runs, agentruntime.ToolAPIScan)
	assertAgentRun(t, runs, agentruntime.ToolDBScan)
}

func TestOrchestratorUsesJSONGeneratorForScenarioPlanning(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeTempProject(t)
	cfg := testConfig(t)
	cfg.Artifacts.Root = filepath.Join(t.TempDir(), "artifacts")
	generator := &fakeJSONGenerator{
		response: []byte(`{
			"scenarios": [
				{
					"id": "Checkout Critical Flow!",
					"name": "Checkout critical flow",
					"level": "L0",
					"priority": 1,
					"steps": ["open cart", "submit order"],
					"assertions": ["order is accepted"]
				}
			]
		}`),
	}

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "autotest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewRepository(db)

	result, err := New(Options{Config: cfg, JSONGenerator: generator, Repository: repo}).Run(ctx, RunRequest{
		ProjectID: "project-llm-plan",
		RepoPath:  projectRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.State.ScenarioPlan.Scenarios) != 1 {
		t.Fatalf("expected one generated scenario, got %+v", result.State.ScenarioPlan)
	}
	scenario := result.State.ScenarioPlan.Scenarios[0]
	if scenario.ID != "checkout-critical-flow" {
		t.Fatalf("expected sanitized scenario id, got %q", scenario.ID)
	}
	if result.State.TestWorkspace == "" {
		t.Fatal("expected generated tests to live in isolated workspace")
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "e2e")); !os.IsNotExist(err) {
		t.Fatalf("expected product repo to stay clean, got %v", err)
	}
	specPath := filepath.Join(result.State.TestWorkspace, "e2e", "specs", "smoke", "checkout-critical-flow.spec.ts")
	content, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Checkout critical flow") {
		t.Fatalf("expected generated scenario name in spec:\n%s", content)
	}
	if generator.prompt == "" || !strings.Contains(generator.prompt, "Project context") {
		t.Fatalf("expected project context prompt, got %q", generator.prompt)
	}
	if !strings.Contains(generator.prompt, "Prioritize real routes from pageGraph") {
		t.Fatalf("expected strengthened route-planning guidance, got %q", generator.prompt)
	}
	if !strings.Contains(generator.prompt, "/api/v1/health") {
		t.Fatalf("expected real api endpoint in prompt, got %q", generator.prompt)
	}
	if !strings.Contains(generator.prompt, "users [id]") {
		t.Fatalf("expected real data model in prompt, got %q", generator.prompt)
	}
	if !strings.Contains(generator.prompt, "Home -> /") {
		t.Fatalf("expected real page graph summary in prompt, got %q", generator.prompt)
	}
	runs, err := repo.GetAgentRuns(ctx, result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	assertAgentRun(t, runs, "scenario.plan")
}

func TestOrchestratorRunsTargetResetAndSeedBeforeExecution(t *testing.T) {
	projectRoot := makeTempProject(t)
	cfg := testConfig(t)
	cfg.Artifacts.Root = filepath.Join(t.TempDir(), "artifacts")
	calls := []string{}
	workdirs := []string{}

	result, err := New(Options{
		Config:        cfg,
		JSONGenerator: scenarioGenerator(),
		TargetCommandRunner: func(_ context.Context, name, command, workdir string) (TargetCommandResult, error) {
			calls = append(calls, name+":"+command)
			workdirs = append(workdirs, workdir)
			return TargetCommandResult{Stdout: name + " ok"}, nil
		},
	}).Run(context.Background(), RunRequest{
		ProjectID: "project-target-hooks",
		RepoPath:  projectRoot,
		TargetConfig: &config.TargetConfig{
			Commands: config.TargetCommands{
				Reset: "npm run test:reset",
				Seed:  "npm run test:seed",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != workflow.StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", result.Status)
	}
	want := []string{"reset:npm run test:reset", "seed:npm run test:seed"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("expected target hooks %v, got %v", want, calls)
	}
	absProjectRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, workdir := range workdirs {
		if workdir != absProjectRoot {
			t.Fatalf("expected hook workdir %q, got %q", absProjectRoot, workdir)
		}
	}
}

func TestOrchestratorStartsTargetAppBeforeExecution(t *testing.T) {
	projectRoot := makeTempProject(t)
	cfg := testConfig(t)
	cfg.Artifacts.Root = filepath.Join(t.TempDir(), "artifacts")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	result, err := New(Options{
		Config:        cfg,
		JSONGenerator: scenarioGenerator(),
	}).Run(context.Background(), RunRequest{
		ProjectID: "project-target-start",
		RepoPath:  projectRoot,
		TargetConfig: &config.TargetConfig{
			BaseURL: server.URL,
			Commands: config.TargetCommands{
				Start: targetNoopCommand(),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status == workflow.StatusFailed {
		t.Fatalf("expected target startup to avoid workflow failure, got %s", result.Status)
	}
}

func TestOrchestratorFailsWhenAgentExecutorFails(t *testing.T) {
	projectRoot := makeTempProject(t)
	cfg := testConfig(t)
	cfg.Artifacts.Root = filepath.Join(t.TempDir(), "artifacts")

	_, err := New(Options{
		Config:        cfg,
		JSONGenerator: &fakeJSONGenerator{err: errors.New("llm unavailable")},
	}).Run(context.Background(), RunRequest{
		ProjectID: "project-llm-fallback",
		RepoPath:  projectRoot,
	})
	if err == nil || !strings.Contains(err.Error(), "llm unavailable") {
		t.Fatalf("expected llm unavailable error, got %v", err)
	}
}

func TestOrchestratorUsesJSONGeneratorForFailureDiagnosis(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeTempProject(t)
	cfg := testConfig(t)
	cfg.Artifacts.Root = filepath.Join(t.TempDir(), "artifacts")
	generator := &fakeJSONGenerator{
		responses: [][]byte{
			[]byte(`{"scenarios":[{"id":"fullstack-smoke","name":"Fullstack smoke flow","level":"L0","priority":1,"steps":["open application","call health api"],"assertions":["page renders","api returns ok"]}]}`),
			[]byte(`{
				"diagnosisResult": {
					"failureType": "test_code",
					"rootCause": "Generated health check assertion is too broad",
					"confidence": 0.83,
					"evidence": [{"type": "result", "summary": "forced failure result", "ref": "result.json"}],
					"fixTarget": "e2e/specs/smoke/fullstack-smoke.spec.ts",
					"nextAction": ""
				}
			}`),
			[]byte(`{
				"patches": [
					{
						"targetPath": "e2e/specs/smoke/fullstack-smoke.spec.ts",
						"diff": "--- a/e2e/specs/smoke/fullstack-smoke.spec.ts\n+++ b/e2e/specs/smoke/fullstack-smoke.spec.ts\n@@ -17,1 +17,2 @@\n });\n+// diagnosis flow patch\n",
						"riskLevel": "low",
						"rationale": "Append a test-only marker without touching product code."
					}
				]
			}`),
		},
	}

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "autotest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewRepository(db)

	result, err := New(Options{Config: cfg, JSONGenerator: generator, Repository: repo}).Run(ctx, RunRequest{
		ProjectID:             "project-llm-diagnosis",
		RepoPath:              projectRoot,
		ForceExecutionFailure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State.DiagnosisResult == nil {
		t.Fatal("expected diagnosis")
	}
	if result.State.DiagnosisResult.FailureType != "TEST_CODE" {
		t.Fatalf("expected normalized failure type, got %+v", result.State.DiagnosisResult)
	}
	if result.State.DiagnosisResult.RootCause != "Generated health check assertion is too broad" {
		t.Fatalf("unexpected root cause: %+v", result.State.DiagnosisResult)
	}
	if result.State.DiagnosisResult.NextAction != "TEST_FIXING" {
		t.Fatalf("expected inferred next action, got %+v", result.State.DiagnosisResult)
	}
	if len(generator.prompts) < 2 {
		t.Fatalf("expected diagnosis prompt history, got %+v", generator.prompts)
	}
	diagnosisPrompt := generator.prompts[1]
	if !strings.Contains(diagnosisPrompt, "Diagnosis rules") {
		t.Fatalf("expected diagnosis rules in prompt, got %q", diagnosisPrompt)
	}
	if !strings.Contains(diagnosisPrompt, "missing Playwright browsers") {
		t.Fatalf("expected environment diagnosis guidance in prompt, got %q", diagnosisPrompt)
	}
	runs, err := repo.GetAgentRuns(ctx, result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	assertAgentRun(t, runs, "failure.diagnosis")
}

func TestOrchestratorUsesJSONGeneratorForTestFixing(t *testing.T) {
	ctx := context.Background()
	projectRoot := makeTempProject(t)
	cfg := testConfig(t)
	cfg.Artifacts.Root = filepath.Join(t.TempDir(), "artifacts")
	generator := &fakeJSONGenerator{
		responses: [][]byte{
			[]byte(`{"scenarios":[{"id":"fullstack-smoke","name":"Fullstack smoke flow","level":"L0","priority":1,"steps":["open application","call health api"],"assertions":["page renders","api returns ok"]}]}`),
			[]byte(`{
				"diagnosisResult": {
					"failureType": "TEST_CODE",
					"rootCause": "Generated smoke spec needs a test-only marker",
					"confidence": 0.75,
					"evidence": [{"type": "result", "summary": "forced failure"}],
					"fixTarget": "e2e/specs/smoke/fullstack-smoke.spec.ts",
					"nextAction": "TEST_FIXING"
				}
			}`),
			[]byte(`{
				"patches": [
					{
						"targetPath": "e2e/specs/smoke/fullstack-smoke.spec.ts",
						"diff": "--- a/e2e/specs/smoke/fullstack-smoke.spec.ts\n+++ b/e2e/specs/smoke/fullstack-smoke.spec.ts\n@@ -17,1 +17,2 @@\n });\n+// llm generated test fix\n",
						"riskLevel": "low",
						"rationale": "Append a test-only marker without touching product code."
					}
				]
			}`),
		},
	}

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "autotest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := sqlite.NewRepository(db)

	result, err := New(Options{Config: cfg, JSONGenerator: generator, Repository: repo}).Run(ctx, RunRequest{
		ProjectID:             "project-llm-fix",
		RepoPath:              projectRoot,
		ForceExecutionFailure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.State.TestFixPatches) != 1 {
		t.Fatalf("expected one llm patch, got %+v", result.State.TestFixPatches)
	}
	patch := result.State.TestFixPatches[0]
	if patch.ID == "" || patch.RiskLevel != "LOW" || !patch.Applied {
		t.Fatalf("expected normalized applied low-risk patch, got %+v", patch)
	}
	specPath := filepath.Join(result.State.TestWorkspace, "e2e", "specs", "smoke", "fullstack-smoke.spec.ts")
	content, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "llm generated test fix") {
		t.Fatalf("expected llm patch marker in spec:\n%s", content)
	}
	if len(generator.prompts) < 3 {
		t.Fatalf("expected test-fix prompt history, got %+v", generator.prompts)
	}
	testFixPrompt := generator.prompts[2]
	if !strings.Contains(testFixPrompt, "targetFile") {
		t.Fatalf("expected target file context in prompt, got %q", testFixPrompt)
	}
	if !strings.Contains(testFixPrompt, "Never use placeholder lines like old/new") {
		t.Fatalf("expected stricter patch rule in prompt, got %q", testFixPrompt)
	}
	runs, err := repo.GetAgentRuns(ctx, result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	assertAgentRun(t, runs, "test.fix")
}

func TestNormalizePatchesRejectsPlaceholderDiff(t *testing.T) {
	_, err := normalizePatches([]workflow.Patch{{
		TargetPath: "e2e/specs/smoke/example.spec.ts",
		Diff:       "--- a/e2e/specs/smoke/example.spec.ts\n+++ b/e2e/specs/smoke/example.spec.ts\n@@ -1,1 +1,1 @@\n-old\n+new\n",
	}})
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("expected placeholder diff error, got %v", err)
	}
}

func TestNormalizePatchTargetsRelativeToRepo(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "test-app")
	patches := normalizePatchTargets(repo, []workflow.Patch{
		{TargetPath: filepath.Join(repo, "e2e", "specs", "smoke", "sample.spec.ts")},
		{TargetPath: "test-app/e2e/specs/smoke/other.spec.ts"},
		{TargetPath: "artifacts/projects/local/e2e/specs/smoke/from-artifact.spec.ts"},
	})
	wants := []string{
		"e2e/specs/smoke/sample.spec.ts",
		"e2e/specs/smoke/other.spec.ts",
		"e2e/specs/smoke/from-artifact.spec.ts",
	}
	for i, want := range wants {
		if patches[i].TargetPath != want {
			t.Fatalf("patch %d: expected %q, got %q", i, want, patches[i].TargetPath)
		}
	}
}

func TestOrchestratorRunForcedFailureFixesAndReruns(t *testing.T) {
	projectRoot := makeTempProject(t)
	cfg := testConfig(t)
	cfg.Artifacts.Root = filepath.Join(t.TempDir(), "artifacts")

	result, err := New(Options{Config: cfg, JSONGenerator: &fakeJSONGenerator{
		responses: [][]byte{
			[]byte(`{"scenarios":[{"id":"fullstack-smoke","name":"Fullstack smoke flow","level":"L0","priority":1,"steps":["open application","call health api"],"assertions":["page renders","api returns ok"]}]}`),
			[]byte(`{
				"diagnosisResult": {
					"failureType": "TEST_CODE",
					"rootCause": "Generated health check assertion is too broad",
					"confidence": 0.8,
					"evidence": [{"type": "execution", "summary": "forced failure", "ref": "result.json"}],
					"fixTarget": "e2e/specs/smoke/fullstack-smoke.spec.ts",
					"nextAction": "TEST_FIXING"
				}
			}`),
			[]byte(`{
				"patches": [
					{
						"targetPath": "e2e/specs/smoke/fullstack-smoke.spec.ts",
						"diff": "--- a/e2e/specs/smoke/fullstack-smoke.spec.ts\n+++ b/e2e/specs/smoke/fullstack-smoke.spec.ts\n@@ -17,1 +17,2 @@\n });\n+// trpc generated test fix\n",
						"riskLevel": "low",
						"rationale": "Append a test-only marker without touching product code."
					}
				]
			}`),
		},
	}}).Run(context.Background(), RunRequest{
		ProjectID:             "project-failure",
		RepoPath:              projectRoot,
		ForceExecutionFailure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != workflow.StatusSucceeded {
		t.Fatalf("expected succeeded after repaired rerun, got %s", result.Status)
	}
	if result.State.DiagnosisResult == nil || result.State.DiagnosisResult.FailureType != "TEST_CODE" {
		t.Fatalf("expected test-code diagnosis, got %+v", result.State.DiagnosisResult)
	}
	if len(result.State.TestFixPatches) != 1 || !result.State.TestFixPatches[0].Applied {
		t.Fatalf("expected applied patch, got %+v", result.State.TestFixPatches)
	}
	if result.State.ExecutionResult == nil || !result.State.ExecutionResult.Passed {
		t.Fatalf("expected rerun to pass, got %+v", result.State.ExecutionResult)
	}
	specPath := filepath.Join(result.State.TestWorkspace, "e2e", "specs", "smoke", "fullstack-smoke.spec.ts")
	content, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(content), "trpc generated test fix") {
		t.Fatalf("expected trpc patch marker in generated spec:\n%s", content)
	}
}

type fakeJSONGenerator struct {
	response  []byte
	responses [][]byte
	prompt    string
	prompts   []string
	err       error
}

func (f *fakeJSONGenerator) GenerateJSON(_ context.Context, prompt string, _ any) ([]byte, error) {
	f.prompt = prompt
	f.prompts = append(f.prompts, prompt)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.responses) > 0 {
		response := f.responses[0]
		f.responses = f.responses[1:]
		return response, nil
	}
	return f.response, nil
}

func scenarioGenerator() *fakeJSONGenerator {
	return &fakeJSONGenerator{
		responses: [][]byte{
			[]byte(`{"scenarios":[{"id":"fullstack-smoke","name":"Fullstack smoke flow","level":"L0","priority":1,"steps":["open application","call health api"],"assertions":["page renders","api returns ok"]}]}`),
			[]byte(`{
				"diagnosisResult": {
					"failureType": "TEST_CODE",
					"rootCause": "Generated smoke spec needs a test-only marker",
					"confidence": 0.8,
					"evidence": [{"type": "execution", "summary": "forced failure", "ref": "result.json"}],
					"fixTarget": "e2e/specs/smoke/fullstack-smoke.spec.ts",
					"nextAction": "TEST_FIXING"
				}
			}`),
			[]byte(`{
				"patches": [
					{
						"targetPath": "e2e/specs/smoke/fullstack-smoke.spec.ts",
						"diff": "--- a/e2e/specs/smoke/fullstack-smoke.spec.ts\n+++ b/e2e/specs/smoke/fullstack-smoke.spec.ts\n@@ -17,1 +17,2 @@\n });\n+// trpc generated test fix\n",
						"riskLevel": "low",
						"rationale": "Append a test-only marker without touching product code."
					}
				]
			}`),
		},
	}
}

func makeTempProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeProjectFile(t, filepath.Join(dir, "go.mod"), "module temp-project\n\ngo 1.22\n")
	writeProjectFile(t, filepath.Join(dir, "package.json"), `{"dependencies":{"react":"^18.0.0"}}`)
	writeProjectFile(t, filepath.Join(dir, "internal", "api", "server.go"), `package api

func routes(mux interface{ HandleFunc(string, any) }) {
  mux.HandleFunc("GET /api/v1/health", nil)
}
`)
	writeProjectFile(t, filepath.Join(dir, "schema.sql"), "create table users(id text primary key, email text not null);\n")
	return dir
}

func writeProjectFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.LLM.Provider = "none"
	cfg.Runner.PlaywrightRunnerDir = filepath.Join(repoRoot(t), "runner", "playwright")
	cfg.Database.DSN = filepath.Join(t.TempDir(), "autotest.db")
	return cfg
}

func targetNoopCommand() string {
	return "sleep 30"
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatal("go.mod not found")
		}
		wd = parent
	}
}

func contains(value, sub string) bool {
	return len(sub) == 0 || (len(value) >= len(sub) && index(value, sub) >= 0)
}

func assertStringSliceContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("expected %q in %v", want, values)
}

func assertAgentRun(t *testing.T, runs []agentruntime.RunRecord, name string) {
	t.Helper()
	for _, run := range runs {
		if run.AgentName == name && run.Status == agentruntime.RunStatusSucceeded {
			if run.InputSummary == "" || run.OutputSummary == "" {
				t.Fatalf("expected summaries for %s, got %+v", name, run)
			}
			if run.InputJSONPath == "" || run.OutputJSONPath == "" {
				t.Fatalf("expected json artifact paths for %s, got %+v", name, run)
			}
			if _, err := os.Stat(run.InputJSONPath); err != nil {
				t.Fatalf("expected input json artifact for %s: %v", name, err)
			}
			if _, err := os.Stat(run.OutputJSONPath); err != nil {
				t.Fatalf("expected output json artifact for %s: %v", name, err)
			}
			return
		}
	}
	t.Fatalf("expected successful agent run %q in %+v", name, runs)
}

func index(value, sub string) int {
	for i := 0; i+len(sub) <= len(value); i++ {
		if value[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
