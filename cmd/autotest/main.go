package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"automated-test/internal/api"
	"automated-test/internal/app/orchestrator"
	"automated-test/internal/config"
	"automated-test/internal/infra/agentruntime"
	"automated-test/internal/infra/sqlite"
)

func main() {
	if err := config.LoadEnvLocal(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: failed to load .env.local:", err)
	}
	if err := run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) < 2 {
		printUsage()
		return nil
	}

	switch args[1] {
	case "init":
		if err := config.WriteDefault(".autotest/config.yaml"); err != nil {
			return err
		}
		db, err := openDB(ctx, config.Default())
		if err != nil {
			return err
		}
		defer db.Close()
		fmt.Println("initialized .autotest/config.yaml and sqlite database")
		return nil
	case "run":
		cfg := config.Default()
		db, err := openDB(ctx, cfg)
		if err != nil {
			return err
		}
		defer db.Close()
		useGraph := flagValue(args, "--graph") == "true"
		useWatch := flagValue(args, "--watch") == "true"
		useHeaded := flagValue(args, "--headed") == "true"
		useVisual := flagValue(args, "--visual") == "true"
		slowMoMS := intFlagValue(args, "--slow-mo", 0)
		if useVisual {
			useHeaded = true
			if slowMoMS == 0 {
				slowMoMS = 700
			}
		}
		baseURL := flagValue(args, "--base-url")
		repoPath := flagValue(args, "--repo-path")
		if repoPath == "" {
			repoPath = "."
		}
		taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
		req := orchestrator.RunRequest{
			ProjectID:             "local",
			RepoPath:              repoPath,
			Headed:                useHeaded,
			Visual:                useVisual,
			SlowMoMS:              slowMoMS,
			BaseURL:               baseURL,
			UseGraph:              useGraph,
			ForceExecutionFailure: flagValue(args, "--force-fail") == "true",
		}

		if useWatch {
			progressChan := make(chan orchestrator.ProgressEvent, 32)
			gr, grErr := orchestrator.NewGraphRunner(orchestrator.Options{
				Config: cfg, Repository: sqlite.NewRepository(db), ProgressChan: progressChan,
			})
			if grErr != nil {
				return grErr
			}
			go func() {
				gr.Run(ctx, req, taskID)
				close(progressChan)
			}()
			track := map[string]time.Time{}
			for evt := range progressChan {
				switch evt.Type {
				case orchestrator.ProgressPhase:
					track[evt.Phase] = time.Now()
					fmt.Printf("[%s] %s %s\n", evt.Phase, evt.Status, evt.Message)
				case orchestrator.ProgressAgent:
					if evt.Status == "RUNNING" {
						fmt.Printf("  ⏳ %s...\n", evt.AgentName)
					} else if evt.Status == "SUCCEEDED" {
						fmt.Printf("  ✅ %s\n", evt.AgentName)
					} else {
						fmt.Printf("  ❌ %s: %s\n", evt.AgentName, evt.Message)
					}
				case orchestrator.ProgressExec:
					fmt.Printf("  > %s\n", evt.Message)
				case orchestrator.ProgressDone:
					fmt.Printf("[DONE] status=%s\n", evt.Status)
				case orchestrator.ProgressError:
					fmt.Printf("[ERROR] %s\n", evt.Message)
				}
			}
			return nil
		}

		if useGraph {
			gr, grErr := orchestrator.NewGraphRunner(orchestrator.Options{Config: cfg, Repository: sqlite.NewRepository(db)})
			if grErr != nil {
				return grErr
			}
			state, runErr := gr.Run(ctx, req, taskID)
			if runErr != nil {
				return runErr
			}
			fmt.Printf("task=%s phase=%s status=%s (graph)\n", taskID, state.Phase, state.Status)
			return nil
		}
		orch := orchestrator.New(orchestrator.Options{Config: cfg, Repository: sqlite.NewRepository(db)})
		result, err := orch.Run(ctx, req)
		if err != nil {
			return err
		}
		fmt.Printf("task=%s phase=%s status=%s\n", result.TaskID, result.Phase, result.Status)
		return nil
	case "status":
		taskID := flagValue(args, "--task")
		if taskID == "" {
			return fmt.Errorf("missing --task")
		}
		cfg := config.Default()
		db, err := openDB(ctx, cfg)
		if err != nil {
			return err
		}
		defer db.Close()
		state, err := sqlite.NewRepository(db).GetTask(ctx, taskID)
		if err != nil {
			return err
		}
		fmt.Printf("task=%s project=%s phase=%s status=%s retry=%d/%d\n",
			state.TaskID,
			state.ProjectID,
			state.Phase,
			state.Status,
			state.RetryState.TotalRetryCount,
			state.RetryState.MaxTotalRetry,
		)
		return nil
	case "report":
		taskID := flagValue(args, "--task")
		if taskID == "" {
			return fmt.Errorf("missing --task")
		}
		cfg := config.Default()
		db, err := openDB(ctx, cfg)
		if err != nil {
			return err
		}
		defer db.Close()
		state, err := sqlite.NewRepository(db).GetTask(ctx, taskID)
		if err != nil {
			return err
		}
		runs, err := sqlite.NewRepository(db).GetAgentRuns(ctx, taskID)
		if err != nil {
			return err
		}
		fmt.Printf("task=%s phase=%s status=%s\n", state.TaskID, state.Phase, state.Status)
		if state.ExecutionResult != nil {
			fmt.Printf("execution passed=%v mode=%s result=%s\n", state.ExecutionResult.Passed, state.ExecutionResult.Mode, state.ExecutionResult.ResultPath)
			orch := orchestrator.New(orchestrator.Options{Config: cfg, Repository: sqlite.NewRepository(db)})
			summary := orch.SummarizeExecutionArtifacts(state)
			if summary.Trace != nil && summary.Trace.Exists {
				fmt.Printf("trace path=%s size=%d\n", summary.Trace.Path, summary.Trace.Size)
			}
			if summary.HtmlReport != nil && summary.HtmlReport.Exists {
				fmt.Printf("html report path=%s\n", summary.HtmlReport.Path)
			}
			if summary.Stdout != nil {
				fmt.Printf("stdout path=%s\n", summary.Stdout.Path)
			}
			if summary.Stderr != nil {
				fmt.Printf("stderr path=%s\n", summary.Stderr.Path)
			}
			if summary.ReportSummary != nil {
				fmt.Printf("playwright total=%d passed=%d failed=%d skipped=%d\n",
					summary.ReportSummary.Total, summary.ReportSummary.Passed,
					summary.ReportSummary.Failed, summary.ReportSummary.Skipped)
			}
			if len(summary.Screenshots) > 0 {
				fmt.Printf("screenshots count=%d\n", len(summary.Screenshots))
			}
		}
		for _, run := range runs {
			fmt.Printf("agent name=%s status=%s input=%s output=%s\n", run.AgentName, run.Status, run.InputSummary, run.OutputSummary)
			if run.InputJSONPath != "" || run.OutputJSONPath != "" {
				fmt.Printf("agent artifacts input=%s output=%s\n", run.InputJSONPath, run.OutputJSONPath)
			}
		}
		if state.DiagnosisResult != nil {
			fmt.Printf("diagnosis type=%s confidence=%.2f next=%s\n", state.DiagnosisResult.FailureType, state.DiagnosisResult.Confidence, state.DiagnosisResult.NextAction)
			fmt.Printf("root cause=%s\n", state.DiagnosisResult.RootCause)
		}
		for _, patch := range state.TestFixPatches {
			fmt.Printf("patch id=%s target=%s risk=%s applied=%v\n", patch.ID, patch.TargetPath, patch.RiskLevel, patch.Applied)
		}
		fmt.Printf("guard risk=%s patchAllowed=%v rerunAllowed=%v review=%v\n",
			state.GuardState.RiskLevel,
			state.GuardState.PatchAllowed,
			state.GuardState.RerunAllowed,
			state.GuardState.NeedsHumanReview,
		)
		return nil
	case "visualize":
		cfg := config.Default()
		db, err := openDB(ctx, cfg)
		if err != nil {
			return err
		}
		defer db.Close()
		gr, err := orchestrator.NewGraphRunner(orchestrator.Options{Config: cfg, Repository: sqlite.NewRepository(db)})
		if err != nil {
			return err
		}
		format := flagValue(args, "--format")
		output := flagValue(args, "--output")
		if output == "" {
			output = filepath.Join(cfg.Artifacts.Root, "workflow.dot")
		}
		if format == "svg" {
			if output == "" {
				output = filepath.Join(cfg.Artifacts.Root, "workflow.svg")
			}
			if err := gr.RenderImage(ctx, "svg", output); err != nil {
				return err
			}
			fmt.Printf("workflow graph saved to %s (SVG)\n", output)
		} else {
			if err := os.WriteFile(output, []byte(gr.DOT()), 0o644); err != nil {
				return err
			}
			fmt.Printf("workflow graph saved to %s (DOT)\n", output)
		}
		return nil
	case "trace":
		taskID := flagValue(args, "--task")
		if taskID == "" {
			return fmt.Errorf("missing --task")
		}
		cfg := config.Default()
		db, err := openDB(ctx, cfg)
		if err != nil {
			return err
		}
		defer db.Close()
		state, err := sqlite.NewRepository(db).GetTask(ctx, taskID)
		if err != nil {
			return err
		}
		if state.ExecutionResult == nil || state.ExecutionResult.TracePath == "" {
			return fmt.Errorf("no trace found for task %s", taskID)
		}
		tracePath := state.ExecutionResult.TracePath
		fmt.Printf("opening trace: %s\n", tracePath)
		cmd := exec.CommandContext(ctx, "npx", "playwright", "show-trace", tracePath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	case "serve":
		cfg := config.Default()
		return api.NewServer(cfg).ListenAndServe(ctx, "127.0.0.1:8080")
	case "tools":
		registry, err := agentruntime.NewDefaultToolRegistry()
		if err != nil {
			return err
		}
		for _, tool := range registry.List() {
			fmt.Printf("%s\t%s\n", tool.Name, tool.Description)
		}
		return nil
	default:
		printUsage()
		return nil
	}
}

func printUsage() {
	fmt.Print(`autotest

Usage:
  autotest init                  Initialize local config
  autotest run                   Run MVP workflow for current repo
  autotest run --watch true      Run with live progress streaming
  autotest run --headed true     Run with visible browser (Playwright headed)
  autotest run --visual true     Run MCP-like visible browser actions without MCP
  autotest run --slow-mo 700     Slow down visual Playwright actions in milliseconds
  autotest run --graph true      Run with graph-based workflow
  autotest run --repo-path PATH  Run workflow against a target project path
  autotest run --force-fail true  Force failure to test repair flow
  autotest status --task ID      Print saved workflow status
  autotest report --task ID      Print execution and diagnosis report
  autotest visualize             Generate workflow graph DOT file
  autotest visualize --format svg --output workflow.svg
  autotest trace --task ID       Open Playwright trace viewer
  autotest tools                 List registered Agent Runtime tools
  autotest serve                 Start HTTP API on 127.0.0.1:8080
`)
}

func openDB(ctx context.Context, cfg config.Config) (*sqlite.DB, error) {
	db, err := sqlite.Open(ctx, cfg.Database.DSN)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func flagValue(args []string, name string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}

func intFlagValue(args []string, name string, fallback int) int {
	value := flagValue(args, name)
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}
