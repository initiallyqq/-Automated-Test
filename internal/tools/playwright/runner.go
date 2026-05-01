package playwright

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Runner struct {
	NodePath   string
	ToolRoot   string
	OutputRoot string
}

type RunRequest struct {
	ProjectRoot      string                    `json:"projectRoot"`
	SpecPattern      string                    `json:"specPattern"`
	OutputDir        string                    `json:"outputDir"`
	PlaywrightConfig string                    `json:"playwrightConfig,omitempty"`
	Headed           bool                      `json:"headed,omitempty"`
	SlowMoMS         int                       `json:"slowMoMs,omitempty"`
	OnOutput         func(stream, line string) `json:"-"`
}

type RunResult struct {
	ExecutionID     string   `json:"executionId"`
	ExitCode        int      `json:"exitCode"`
	Passed          bool     `json:"passed"`
	ResultPath      string   `json:"resultPath"`
	TracePath       string   `json:"tracePath,omitempty"`
	HtmlReportPath  string   `json:"htmlReportPath,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	StdoutPath      string   `json:"stdoutPath,omitempty"`
	StderrPath      string   `json:"stderrPath,omitempty"`
	ScreenshotPaths []string `json:"screenshotPaths"`
	LogPath         string   `json:"logPath,omitempty"`
	Stdout          string   `json:"stdout,omitempty"`
	Stderr          string   `json:"stderr,omitempty"`
}

func NewRunner(nodePath, toolRoot, outputRoot string) *Runner {
	return &Runner{
		NodePath:   defaultString(nodePath, "node"),
		ToolRoot:   toolRoot,
		OutputRoot: outputRoot,
	}
}

func (r *Runner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if req.ProjectRoot == "" {
		return RunResult{}, errors.New("project root is required")
	}
	if req.SpecPattern == "" {
		req.SpecPattern = "e2e/specs/**/*.spec.ts"
	}
	toolRoot, err := filepath.Abs(r.ToolRoot)
	if err != nil {
		return RunResult{}, err
	}
	projectRoot, err := filepath.Abs(req.ProjectRoot)
	if err != nil {
		return RunResult{}, err
	}
	req.ProjectRoot = projectRoot

	if req.OutputDir == "" {
		req.OutputDir = filepath.Join(r.OutputRoot, "executions", time.Now().UTC().Format("20060102150405"))
	}
	req.OutputDir, err = filepath.Abs(req.OutputDir)
	if err != nil {
		return RunResult{}, err
	}
	if err := os.MkdirAll(req.OutputDir, 0o755); err != nil {
		return RunResult{}, err
	}

	requestPath := filepath.Join(req.OutputDir, "request.json")
	requestBytes, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return RunResult{}, err
	}
	if err := os.WriteFile(requestPath, requestBytes, 0o644); err != nil {
		return RunResult{}, err
	}

	scriptPath := filepath.Join(toolRoot, "src", "cli.mjs")
	if _, err := os.Stat(scriptPath); err != nil {
		return RunResult{}, fmt.Errorf("playwright runner entrypoint is missing: %s", scriptPath)
	}

	cmd := exec.CommandContext(ctx, r.NodePath, scriptPath, requestPath)
	cmd.Dir = toolRoot
	cmd.Env = append(os.Environ(), "NO_COLOR=1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return RunResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return RunResult{}, err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go captureLines(&wg, stdoutPipe, &stdout, "stdout", req.OnOutput)
	go captureLines(&wg, stderrPipe, &stderr, "stderr", req.OnOutput)
	err = cmd.Wait()
	wg.Wait()

	result := RunResult{
		ExecutionID:     filepath.Base(req.OutputDir),
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		ScreenshotPaths: []string{},
	}
	if err != nil {
		result.ExitCode = exitCode(err)
		result.Passed = false
		_ = writeLog(req.OutputDir, stdout.String(), stderr.String())
		return result, fmt.Errorf("run playwright runner: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return result, fmt.Errorf("parse playwright runner output: %w; output=%s", err, stdout.String())
	}
	result.ExecutionID = filepath.Base(req.OutputDir)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	if result.ExitCode == 0 && result.Passed {
		return result, nil
	}
	return result, nil
}

func captureLines(wg *sync.WaitGroup, reader interface{ Read([]byte) (int, error) }, buffer *bytes.Buffer, stream string, onOutput func(string, string)) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		buffer.WriteString(line)
		buffer.WriteByte('\n')
		if onOutput != nil && strings.TrimSpace(line) != "" {
			onOutput(stream, line)
		}
	}
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func writeLog(outputDir, stdout, stderr string) error {
	return os.WriteFile(filepath.Join(outputDir, "runner.log"), []byte(stdout+"\n"+stderr), 0o644)
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
