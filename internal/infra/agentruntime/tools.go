package agentruntime

import (
	"context"

	apiscantool "automated-test/internal/tools/apiscan"
	dbscantool "automated-test/internal/tools/dbscan"
	repotool "automated-test/internal/tools/repo"
)

const (
	ToolRepoScan = "repo.scan"
	ToolAPIScan  = "api.scan"
	ToolDBScan   = "db.scan"
)

func NewDefaultToolRegistry() (*Registry, error) {
	registry := NewRegistry()
	for _, tool := range []Tool{
		repoScanTool(),
		apiScanTool(),
		dbScanTool(),
	} {
		if err := registry.Register(tool); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func repoScanTool() Tool {
	return NewJSONTool(ToolRepoScan, "Scan repository files and detect languages, frameworks, package tooling, and project risks.", scanRequestSchema(), func(ctx context.Context, req repotool.ScanRequest) (repotool.ScanResult, error) {
		return repotool.NewScanner().Scan(ctx, req)
	})
}

func apiScanTool() Tool {
	return NewJSONTool(ToolAPIScan, "Scan source files for common HTTP route declarations.", scanRequestSchema(), func(ctx context.Context, req apiscantool.ScanRequest) (apiscantool.ScanResult, error) {
		return apiscantool.NewScanner().Scan(ctx, req)
	})
}

func dbScanTool() Tool {
	return NewJSONTool(ToolDBScan, "Scan SQL and Prisma schema files for data models and fields.", scanRequestSchema(), func(ctx context.Context, req dbscantool.ScanRequest) (dbscantool.ScanResult, error) {
		return dbscantool.NewScanner().Scan(ctx, req)
	})
}

func scanRequestSchema() map[string]any {
	return map[string]any{
		"repoPath": "absolute or relative repository path",
		"excludes": []string{"optional/path/to/exclude"},
	}
}
