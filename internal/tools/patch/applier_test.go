package patch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"automated-test/internal/domain/workflow"
)

func TestApplierAppliesUnifiedDiff(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "e2e", "specs", "smoke", "sample.spec.ts")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := NewApplier([]string{"e2e/specs/**"}, nil).Apply(context.Background(), ApplyRequest{
		ProjectRoot: dir,
		Patches: []workflow.Patch{
			{
				ID:         "patch_test",
				TargetPath: "e2e/specs/smoke/sample.spec.ts",
				Diff: "--- a/e2e/specs/smoke/sample.spec.ts\n" +
					"+++ b/e2e/specs/smoke/sample.spec.ts\n" +
					"@@ -2,1 +2,2 @@\n" +
					" line two\n" +
					"+line three\n",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "line three") {
		t.Fatalf("expected patched content, got:\n%s", content)
	}
}

func TestApplierRejectsNoOpPatch(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "e2e", "specs", "smoke", "sample.spec.ts")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := NewApplier([]string{"e2e/specs/**"}, nil).Apply(context.Background(), ApplyRequest{
		ProjectRoot: dir,
		Patches: []workflow.Patch{
			{
				ID:         "patch_noop",
				TargetPath: "e2e/specs/smoke/sample.spec.ts",
				Diff: "--- a/e2e/specs/smoke/sample.spec.ts\n" +
					"+++ b/e2e/specs/smoke/sample.spec.ts\n" +
					"@@ -1,2 +1,2 @@\n" +
					" line one\n" +
					" line two\n",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "no file changes") {
		t.Fatalf("expected no-op patch error, got %v", err)
	}
}
