package repo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Scanner struct{}

type ScanRequest struct {
	RepoPath string   `json:"repoPath"`
	Excludes []string `json:"excludes,omitempty"`
}

type ScanResult struct {
	RepoPath    string         `json:"repoPath"`
	Files       []FileInfo     `json:"files"`
	TechHints   []string       `json:"techHints"`
	Languages   []string       `json:"languages"`
	Frameworks  []string       `json:"frameworks"`
	PackageTool string         `json:"packageTool,omitempty"`
	Frontend    bool           `json:"frontend"`
	Backend     bool           `json:"backend"`
	Database    bool           `json:"database"`
	Risks       []string       `json:"risks,omitempty"`
	PackageJSON map[string]any `json:"packageJson,omitempty"`
}

type FileInfo struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

func NewScanner() *Scanner {
	return &Scanner{}
}

func (s *Scanner) Scan(ctx context.Context, req ScanRequest) (ScanResult, error) {
	if req.RepoPath == "" {
		return ScanResult{}, errors.New("repo path is required")
	}
	root, err := filepath.Abs(req.RepoPath)
	if err != nil {
		return ScanResult{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return ScanResult{}, err
	}
	if !info.IsDir() {
		return ScanResult{}, errors.New("repo path must be a directory")
	}

	result := ScanResult{RepoPath: root}
	seenHints := map[string]bool{}
	seenLanguages := map[string]bool{}
	seenFrameworks := map[string]bool{}
	excludes := append(defaultExcludes(), req.Excludes...)

	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if shouldExclude(rel, excludes) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}

		kind := classify(rel)
		if kind != "" {
			result.Files = append(result.Files, FileInfo{Path: rel, Kind: kind})
		}
		applyFileHints(rel, path, &result, seenHints, seenLanguages, seenFrameworks)
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}

	result.TechHints = sortedKeys(seenHints)
	result.Languages = sortedKeys(seenLanguages)
	result.Frameworks = sortedKeys(seenFrameworks)
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].Path < result.Files[j].Path })

	if result.Frontend && result.Backend {
		result.TechHints = appendUnique(result.TechHints, "fullstack")
	}
	if len(result.Files) == 0 {
		result.Risks = append(result.Risks, "no recognizable project files found")
	}
	return result, nil
}

func defaultExcludes() []string {
	return []string{
		".git",
		".cache",
		".autotest",
		"artifacts",
		"node_modules",
		"dist",
		"build",
		"coverage",
		"e2e",
	}
}

func shouldExclude(rel string, excludes []string) bool {
	for _, exclude := range excludes {
		exclude = strings.Trim(filepath.ToSlash(exclude), "/")
		if rel == exclude || strings.HasPrefix(rel, exclude+"/") {
			return true
		}
	}
	return false
}

func classify(rel string) string {
	base := filepath.Base(rel)
	switch base {
	case "go.mod":
		return "go-module"
	case "package.json":
		return "node-package"
	case "pnpm-lock.yaml":
		return "pnpm-lock"
	case "yarn.lock":
		return "yarn-lock"
	case "package-lock.json":
		return "npm-lock"
	case "playwright.config.ts", "playwright.config.js":
		return "playwright-config"
	case "openapi.json", "swagger.json":
		return "openapi"
	}
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".go":
		return "go-source"
	case ".ts", ".tsx":
		return "typescript-source"
	case ".js", ".jsx":
		return "javascript-source"
	case ".sql":
		return "sql-schema"
	case ".html":
		return "html-page"
	}
	return ""
}

func applyFileHints(rel, abs string, result *ScanResult, hints, languages, frameworks map[string]bool) {
	switch classify(rel) {
	case "go-module", "go-source":
		languages["go"] = true
		result.Backend = true
	case "typescript-source":
		languages["typescript"] = true
	case "javascript-source":
		languages["javascript"] = true
	case "sql-schema":
		result.Database = true
	case "html-page":
		languages["html"] = true
		result.Frontend = true
	}

	base := filepath.Base(rel)
	switch base {
	case "package.json":
		hints["node"] = true
		result.Frontend = true
		readPackageJSON(abs, result, frameworks)
	case "playwright.config.ts", "playwright.config.js":
		frameworks["playwright"] = true
	case "go.mod":
		hints["go-module"] = true
		frameworks["go"] = true
	}
}

func readPackageJSON(path string, result *ScanResult, frameworks map[string]bool) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		result.Risks = append(result.Risks, "failed to read package.json")
		return
	}
	var pkg map[string]any
	if err := json.Unmarshal(bytes, &pkg); err != nil {
		result.Risks = append(result.Risks, "failed to parse package.json")
		return
	}
	result.PackageJSON = pkg
	if _, ok := pkg["packageManager"].(string); ok {
		result.PackageTool = "packageManager"
	}
	deps := mergedDeps(pkg)
	for dep := range deps {
		switch dep {
		case "react", "next":
			frameworks[dep] = true
			result.Frontend = true
		case "vue", "nuxt":
			frameworks[dep] = true
			result.Frontend = true
		case "express", "koa", "fastify", "nestjs":
			frameworks[dep] = true
			result.Backend = true
		case "@playwright/test":
			frameworks["playwright"] = true
		case "prisma", "typeorm", "sequelize":
			frameworks[dep] = true
			result.Database = true
		}
	}
}

func mergedDeps(pkg map[string]any) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"dependencies", "devDependencies"} {
		raw, ok := pkg[key].(map[string]any)
		if !ok {
			continue
		}
		for name, version := range raw {
			if value, ok := version.(string); ok {
				out[name] = value
			}
		}
	}
	return out
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
