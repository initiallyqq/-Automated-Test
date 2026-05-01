package patch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"automated-test/internal/domain/workflow"
)

type Applier struct {
	AllowedPaths []string
	BlockedPaths []string
}

type ApplyRequest struct {
	ProjectRoot string
	Patches     []workflow.Patch
}

type ApplyResult struct {
	Patches []workflow.Patch
}

func NewApplier(allowed, blocked []string) *Applier {
	return &Applier{AllowedPaths: allowed, BlockedPaths: blocked}
}

func (a *Applier) Apply(ctx context.Context, req ApplyRequest) (ApplyResult, error) {
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	if req.ProjectRoot == "" {
		req.ProjectRoot = "."
	}
	root, err := filepath.Abs(req.ProjectRoot)
	if err != nil {
		return ApplyResult{}, err
	}
	updated := make([]workflow.Patch, 0, len(req.Patches))
	for _, patch := range req.Patches {
		path := filepath.ToSlash(patch.TargetPath)
		if matchesAny(path, a.BlockedPaths) {
			return ApplyResult{}, errors.New("patch touches blocked path: " + path)
		}
		if !matchesAny(path, a.AllowedPaths) {
			return ApplyResult{}, errors.New("patch outside allowed paths: " + path)
		}
		abs := filepath.Join(root, filepath.FromSlash(path))
		changed, err := applyUnifiedDiff(abs, patch.Diff)
		if err != nil {
			return ApplyResult{}, err
		}
		patch.Applied = changed
		updated = append(updated, patch)
	}
	if len(updated) > 0 {
		changed := false
		for _, patch := range updated {
			if patch.Applied {
				changed = true
				break
			}
		}
		if !changed {
			return ApplyResult{}, errors.New("patch produced no file changes")
		}
	}
	return ApplyResult{Patches: updated}, nil
}

func applyUnifiedDiff(path, diff string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(diff) == "" {
		return false, nil
	}
	current := splitLines(string(content))
	hunks := parseHunks(diff)
	for _, hunk := range hunks {
		next, err := applyHunk(current, hunk)
		if err != nil {
			return false, err
		}
		current = next
	}
	nextContent := strings.Join(current, "\n") + "\n"
	if string(content) == nextContent {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(nextContent), 0o644)
}

type hunkLine struct {
	op   byte
	text string
}

func parseHunks(diff string) [][]hunkLine {
	lines := splitLines(diff)
	hunks := [][]hunkLine{}
	var current []hunkLine
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "@@"):
			if current != nil {
				hunks = append(hunks, current)
			}
			current = []hunkLine{}
		case current != nil && line != "":
			op := line[0]
			if op == ' ' || op == '+' || op == '-' {
				current = append(current, hunkLine{op: op, text: line[1:]})
			}
		}
	}
	if current != nil {
		hunks = append(hunks, current)
	}
	return hunks
}

func applyHunk(lines []string, hunk []hunkLine) ([]string, error) {
	oldBlock := make([]string, 0, len(hunk))
	newBlock := make([]string, 0, len(hunk))
	for _, line := range hunk {
		switch line.op {
		case ' ':
			oldBlock = append(oldBlock, line.text)
			newBlock = append(newBlock, line.text)
		case '-':
			oldBlock = append(oldBlock, line.text)
		case '+':
			newBlock = append(newBlock, line.text)
		}
	}

	index := findBlock(lines, oldBlock)
	if index < 0 {
		if len(oldBlock) == 0 {
			return append(lines, newBlock...), nil
		}
		return nil, fmt.Errorf("patch context not found: %q", strings.Join(oldBlock, "\\n"))
	}
	if blockExists(lines, newBlock) {
		return lines, nil
	}
	out := make([]string, 0, len(lines)-len(oldBlock)+len(newBlock))
	out = append(out, lines[:index]...)
	out = append(out, newBlock...)
	out = append(out, lines[index+len(oldBlock):]...)
	return out, nil
}

func findBlock(lines, block []string) int {
	if len(block) == 0 {
		return len(lines)
	}
	for i := 0; i <= len(lines)-len(block); i++ {
		ok := true
		for j := range block {
			if lines[i+j] != block[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

func blockExists(lines, block []string) bool {
	return findBlock(lines, block) >= 0
}

func splitLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
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
