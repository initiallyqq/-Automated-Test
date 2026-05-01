package apiscan

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
	Endpoints []Endpoint `json:"endpoints"`
	Warnings  []string   `json:"warnings,omitempty"`
}

type Endpoint struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
	Source string `json:"source"`
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
		if entry.IsDir() || !isSourceFile(rel) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			result.Warnings = append(result.Warnings, "failed to read "+rel)
			return nil
		}
		for _, endpoint := range extractEndpoints(rel, string(content)) {
			key := endpoint.Method + " " + endpoint.Path
			if seen[key] {
				continue
			}
			seen[key] = true
			result.Endpoints = append(result.Endpoints, endpoint)
		}
		return nil
	})
	if err != nil {
		return ScanResult{}, err
	}
	sort.Slice(result.Endpoints, func(i, j int) bool {
		left := result.Endpoints[i].Path + " " + result.Endpoints[i].Method
		right := result.Endpoints[j].Path + " " + result.Endpoints[j].Method
		return left < right
	})
	return result, nil
}

var (
	methodCallPattern = regexp.MustCompile(`(?i)\.(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s*\(\s*["']([^"']+)["']`)
	handleFuncPattern = regexp.MustCompile(`HandleFunc\s*\(\s*["'](?:(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+)?([^"']+)["']\s*,\s*([A-Za-z_][A-Za-z0-9_]*)`)
	muxPattern        = regexp.MustCompile(`["'](GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(/[A-Za-z0-9_./{}:\-]*)["']`)
)

func extractEndpoints(source string, content string) []Endpoint {
	endpoints := []Endpoint{}
	handlerMethods := extractGoHandlerMethods(content)
	for _, match := range methodCallPattern.FindAllStringSubmatch(content, -1) {
		if !strings.HasPrefix(match[2], "/") {
			continue
		}
		endpoints = append(endpoints, Endpoint{
			Method: strings.ToUpper(match[1]),
			Path:   normalizePath(match[2]),
			Name:   nameFromPath(match[2]),
			Source: source,
		})
	}
	for _, match := range handleFuncPattern.FindAllStringSubmatch(content, -1) {
		method := strings.ToUpper(match[1])
		handler := ""
		if len(match) > 3 {
			handler = match[3]
		}
		methods := []string{method}
		if method == "" && handler != "" {
			methods = handlerMethods[handler]
		}
		if len(methods) == 0 || methods[0] == "" {
			method = "UNKNOWN"
			methods = []string{method}
		}
		for _, method := range methods {
			endpoints = append(endpoints, Endpoint{
				Method: strings.ToUpper(method),
				Path:   normalizePath(match[2]),
				Name:   nameFromPath(match[2]),
				Source: source,
			})
		}
	}
	for _, match := range muxPattern.FindAllStringSubmatch(content, -1) {
		endpoints = append(endpoints, Endpoint{
			Method: strings.ToUpper(match[1]),
			Path:   normalizePath(match[2]),
			Name:   nameFromPath(match[2]),
			Source: source,
		})
	}
	return endpoints
}

func extractGoHandlerMethods(content string) map[string][]string {
	out := map[string][]string{}
	funcPattern := regexp.MustCompile(`func\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	casePattern := regexp.MustCompile(`(?i)case\s+http\.Method(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)`)
	matches := funcPattern.FindAllStringSubmatchIndex(content, -1)
	for i, match := range matches {
		name := content[match[2]:match[3]]
		start := match[0]
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := content[start:end]
		methods := []string{}
		seen := map[string]bool{}
		for _, c := range casePattern.FindAllStringSubmatch(body, -1) {
			method := strings.ToUpper(c[1])
			if !seen[method] {
				seen[method] = true
				methods = append(methods, method)
			}
		}
		if len(methods) > 0 {
			out[name] = methods
		}
	}
	return out
}

func normalizePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		return "/" + value
	}
	return value
}

func nameFromPath(value string) string {
	value = strings.Trim(normalizePath(value), "/")
	if value == "" {
		return "root"
	}
	return strings.NewReplacer("/", "_", "{", "", "}", "", ":", "").Replace(value)
}

func isSourceFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx":
		return true
	default:
		return false
	}
}

func defaultExcludes() []string {
	return []string{".git", ".cache", ".autotest", "artifacts", "node_modules", "dist", "build", "coverage", "e2e"}
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
