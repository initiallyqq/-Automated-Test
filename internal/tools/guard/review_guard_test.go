package guard

import (
	"context"
	"testing"

	"automated-test/internal/domain/workflow"
)

func TestReviewGuardAllowsTestPatch(t *testing.T) {
	decision, err := NewReviewGuard([]string{"e2e/specs/**"}, []string{"src/core/**"}).Review(context.Background(), []workflow.Patch{
		{TargetPath: "e2e/specs/smoke/fullstack-smoke.spec.ts"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !decision.PatchAllowed || decision.RiskLevel != "LOW" {
		t.Fatalf("expected low-risk allow decision, got %+v", decision)
	}
}

func TestReviewGuardBlocksProtectedPath(t *testing.T) {
	decision, err := NewReviewGuard([]string{"e2e/specs/**"}, []string{"src/core/**"}).Review(context.Background(), []workflow.Patch{
		{TargetPath: "src/core/auth.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "BLOCK" || decision.PatchAllowed {
		t.Fatalf("expected block decision, got %+v", decision)
	}
}
