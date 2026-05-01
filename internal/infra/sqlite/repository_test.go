package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"automated-test/internal/domain/workflow"
	"automated-test/internal/infra/agentruntime"
)

func TestRepositorySavesAndReadsTask(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "autotest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(db)
	if err := repo.UpsertProject(ctx, "project-1", "Project 1", ".", "fullstack"); err != nil {
		t.Fatal(err)
	}
	state := workflow.NewState("task-1", "project-1", 2)
	state.Phase = workflow.PhaseDone
	state.Status = workflow.StatusSucceeded
	if err := repo.SaveTask(ctx, state); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetTask(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "task-1" || got.Status != workflow.StatusSucceeded {
		t.Fatalf("unexpected state: %+v", got)
	}
}

func TestRepositorySavesDiagnosisAndPatch(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "autotest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(db)
	if err := repo.UpsertProject(ctx, "project-1", "Project 1", ".", "fullstack"); err != nil {
		t.Fatal(err)
	}
	state := workflow.NewState("task-1", "project-1", 2)
	if err := repo.SaveTask(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveDiagnosis(ctx, "task-1", workflow.DiagnosisResult{
		FailureType: "TEST_CODE",
		RootCause:   "test failure",
		Confidence:  0.5,
		NextAction:  "TEST_FIXING",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SavePatch(ctx, "task-1", workflow.Patch{
		ID:         "patch-1",
		TargetPath: "e2e/specs/smoke/fullstack-smoke.spec.ts",
		RiskLevel:  "LOW",
		Applied:    true,
		Rationale:  "test",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRepositorySavesAndReadsAgentRuns(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "autotest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(db)
	if err := repo.UpsertProject(ctx, "project-1", "Project 1", ".", "fullstack"); err != nil {
		t.Fatal(err)
	}
	state := workflow.NewState("task-1", "project-1", 2)
	if err := repo.SaveTask(ctx, state); err != nil {
		t.Fatal(err)
	}

	startedAt := time.Now().UTC()
	finishedAt := startedAt.Add(10 * time.Millisecond)
	if err := repo.SaveAgentRun(ctx, agentruntime.RunRecord{
		ID:             "agent-1",
		TaskID:         "task-1",
		AgentName:      agentruntime.ToolRepoScan,
		InputSummary:   `{"repoPath":"."}`,
		OutputSummary:  `{"frontend":true}`,
		InputJSONPath:  "artifacts/agent-runs/task-1/repo.scan_input.json",
		OutputJSONPath: "artifacts/agent-runs/task-1/repo.scan_output.json",
		Status:         agentruntime.RunStatusSucceeded,
		StartedAt:      startedAt,
		FinishedAt:     &finishedAt,
	}); err != nil {
		t.Fatal(err)
	}

	runs, err := repo.GetAgentRuns(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one agent run, got %+v", runs)
	}
	run := runs[0]
	if run.ID != "agent-1" || run.AgentName != agentruntime.ToolRepoScan || run.Status != agentruntime.RunStatusSucceeded {
		t.Fatalf("unexpected agent run: %+v", run)
	}
	if run.FinishedAt == nil {
		t.Fatalf("expected finished_at, got %+v", run)
	}
	if run.InputJSONPath == "" || run.OutputJSONPath == "" {
		t.Fatalf("expected json paths, got %+v", run)
	}
}
