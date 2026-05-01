package guard

import (
	"context"
	"path/filepath"
	"strings"

	"automated-test/internal/domain/workflow"
)

type ReviewGuard struct {
	AllowedPaths []string
	BlockedPaths []string
}

type Decision struct {
	Decision         string   `json:"decision"`
	RiskLevel        string   `json:"riskLevel"`
	PatchAllowed     bool     `json:"patchAllowed"`
	RerunAllowed     bool     `json:"rerunAllowed"`
	NeedsHumanReview bool     `json:"needsHumanReview"`
	Reasons          []string `json:"reasons"`
}

func NewReviewGuard(allowed, blocked []string) *ReviewGuard {
	return &ReviewGuard{AllowedPaths: allowed, BlockedPaths: blocked}
}

func (g *ReviewGuard) Review(ctx context.Context, patches []workflow.Patch) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	if len(patches) == 0 {
		return Decision{
			Decision:         "REVIEW",
			RiskLevel:        "MEDIUM",
			PatchAllowed:     false,
			RerunAllowed:     false,
			NeedsHumanReview: true,
			Reasons:          []string{"no patches generated"},
		}, nil
	}
	reasons := []string{}
	for _, patch := range patches {
		path := filepath.ToSlash(patch.TargetPath)
		if matchesAny(path, g.BlockedPaths) {
			return Decision{
				Decision:         "BLOCK",
				RiskLevel:        "BLOCKED",
				PatchAllowed:     false,
				RerunAllowed:     false,
				NeedsHumanReview: true,
				Reasons:          []string{"patch touches blocked path: " + path},
			}, nil
		}
		if !matchesAny(path, g.AllowedPaths) {
			reasons = append(reasons, "patch path is outside allowlist: "+path)
		}
	}
	if len(reasons) > 0 {
		return Decision{
			Decision:         "REVIEW",
			RiskLevel:        "MEDIUM",
			PatchAllowed:     false,
			RerunAllowed:     false,
			NeedsHumanReview: true,
			Reasons:          reasons,
		}, nil
	}
	return Decision{
		Decision:         "ALLOW",
		RiskLevel:        "LOW",
		PatchAllowed:     true,
		RerunAllowed:     true,
		NeedsHumanReview: false,
		Reasons:          []string{"all patches are in allowed test paths"},
	}, nil
}

func matchesAny(path string, patterns []string) bool {
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(pattern)
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				return true
			}
			continue
		}
		if ok, _ := filepath.Match(pattern, path); ok {
			return true
		}
	}
	return false
}
