package dbscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Scanner struct{}

type ScanRequest struct {
	RepoPath string   `json:"repoPath"`
	Excludes []string `json:"excludes,omitempty"`
}

type ScanResult struct {
	Models   []Model  `json:"models"`
	Warnings []string `json:"warnings,omitempty"`
}

type Model struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields,omitempty"`
	Source string   `json:"source"`
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

	result := ScanResult{}
	seen := map[string]bool{}
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
		if entry.IsDir() || !isSchemaFile(rel) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			result.Warnings = append(result.Warnings, "failed to read "+rel)
			return nil
		}
		for _, model := range extractModels(rel, string(content)) {
			key := model.Source + ":" + model.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			result.Models = append(result.Models, model)
		}
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}
	sort.Slice(result.Models, func(i, j int) bool {
		return result.Models[i].Name < result.Models[j].Name
	})
	return result, nil
}

var (
	sqlTablePattern    = regexp.MustCompile(`(?is)create\s+table\s+(?:if\s+not\s+exists\s+)?["'\[]?([A-Za-z_][\w.]*)["'\]]?\s*\((.*?)\)`)
	prismaModelPattern = regexp.MustCompile(`(?is)\bmodel\s+([A-Za-z_][\w]*)\s*\{(.*?)\}`)
)

func extractModels(source string, content string) []Model {
	models := []Model{}
	for _, match := range sqlTablePattern.FindAllStringSubmatch(content, -1) {
		models = append(models, Model{
			Name:   cleanIdentifier(match[1]),
			Fields: extractSQLFields(match[2]),
			Source: source,
		})
	}
	for _, match := range prismaModelPattern.FindAllStringSubmatch(content, -1) {
		models = append(models, Model{
			Name:   match[1],
			Fields: extractPrismaFields(match[2]),
			Source: source,
		})
	}
	return models
}

func extractSQLFields(body string) []string {
	fields := []string{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, ","))
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "PRIMARY ") || strings.HasPrefix(upper, "FOREIGN ") || strings.HasPrefix(upper, "CONSTRAINT ") || strings.HasPrefix(upper, "UNIQUE ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			fields = append(fields, cleanIdentifier(parts[0]))
		}
	}
	return uniqueSorted(fields)
}

func extractPrismaFields(body string) []string {
	fields := []string{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "@@") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) > 0 {
			fields = append(fields, parts[0])
		}
	}
	return uniqueSorted(fields)
}

func cleanIdentifier(value string) string {
	return strings.Trim(value, "`\"[]")
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func isSchemaFile(path string) bool {
	base := filepath.Base(path)
	return strings.EqualFold(base, "schema.prisma") || strings.EqualFold(filepath.Ext(path), ".sql")
}

func defaultExcludes() []string {
	return []string{".git", ".cache", ".autotest", "artifacts", "node_modules", "dist", "build", "coverage"}
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
