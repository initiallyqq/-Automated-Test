package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"automated-test/internal/app/orchestrator"
	"automated-test/internal/config"
	"automated-test/internal/infra/agentruntime"
	"automated-test/internal/infra/sqlite"
)

type Server struct {
	config       config.Config
	repo         *sqlite.Repository
	toolRegistry *agentruntime.Registry
}

func NewServer(cfg config.Config) *Server {
	registry, _ := agentruntime.NewDefaultToolRegistry()
	return &Server{config: cfg, toolRegistry: registry}
}

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	db, err := sqlite.Open(ctx, s.config.Database.DSN)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return err
	}
	s.repo = sqlite.NewRepository(db)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/tools", s.listTools)
	mux.HandleFunc("POST /api/v1/tasks/run", s.runTask)
	mux.HandleFunc("GET /api/v1/tasks/{taskId}", s.getTask)
	mux.HandleFunc("GET /api/v1/tasks/{taskId}/report", s.getReport)
	mux.HandleFunc("GET /api/v1/tasks/{taskId}/stream", s.streamTask)
	mux.HandleFunc("POST /api/v1/tasks/{taskId}/stream", s.streamTask)
	mux.HandleFunc("GET /api/v1/workflow/graph", s.workflowGraph)

	server := &http.Server{Addr: addr, Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		_ = server.Shutdown(context.Background())
		return ctx.Err()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "repository is not initialized"})
		return
	}
	state, err := s.repo.GetTask(r.Context(), r.PathValue("taskId"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) getReport(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "repository is not initialized"})
		return
	}
	taskID := r.PathValue("taskId")
	state, err := s.repo.GetTask(r.Context(), taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	runs, err := s.repo.GetAgentRuns(r.Context(), taskID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	orch := orchestrator.New(orchestrator.Options{Config: s.config, Repository: s.repo, ToolRegistry: s.toolRegistry})
	artifacts := orch.SummarizeExecutionArtifacts(state)
	writeJSON(w, http.StatusOK, map[string]any{
		"state":     state,
		"agentRuns": runs,
		"artifacts": artifacts,
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listTools(w http.ResponseWriter, _ *http.Request) {
	if s.toolRegistry == nil {
		writeJSON(w, http.StatusOK, []agentruntime.ToolInfo{})
		return
	}
	writeJSON(w, http.StatusOK, s.toolRegistry.List())
}

func (s *Server) runTask(w http.ResponseWriter, r *http.Request) {
	var req orchestrator.RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.ProjectID == "" {
		req.ProjectID = "local"
	}
	if req.RepoPath == "" {
		req.RepoPath = "."
	}
	if req.Visual {
		req.Headed = true
		if req.SlowMoMS == 0 {
			req.SlowMoMS = 700
		}
	}

	var result orchestrator.RunResult
	var err error
	if req.UseGraph {
		gr, grErr := orchestrator.NewGraphRunner(orchestrator.Options{Config: s.config, Repository: s.repo, ToolRegistry: s.toolRegistry})
		if grErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": grErr.Error()})
			return
		}
		taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
		state, runErr := gr.Run(r.Context(), req, taskID)
		err = runErr
		result = orchestrator.RunResult{TaskID: taskID, Phase: state.Phase, Status: state.Status, State: state}
	} else {
		result, err = orchestrator.New(orchestrator.Options{Config: s.config, Repository: s.repo, ToolRegistry: s.toolRegistry}).Run(r.Context(), req)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) streamTask(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskId")
	if taskID == "" {
		taskID = fmt.Sprintf("task_%d", time.Now().UnixNano())
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	var req orchestrator.RunRequest
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	if req.ProjectID == "" {
		req.ProjectID = "local"
	}
	if req.RepoPath == "" {
		req.RepoPath = "."
	}
	if value := r.URL.Query().Get("projectId"); value != "" {
		req.ProjectID = value
	}
	if value := r.URL.Query().Get("repoPath"); value != "" {
		req.RepoPath = value
	}
	if value := r.URL.Query().Get("baseUrl"); value != "" {
		req.BaseURL = value
	}
	if value := r.URL.Query().Get("headed"); value == "true" {
		req.Headed = true
	}
	if value := r.URL.Query().Get("slowMoMs"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			req.SlowMoMS = parsed
		}
	}
	if value := r.URL.Query().Get("visual"); value == "true" {
		req.Visual = true
		req.Headed = true
		if req.SlowMoMS == 0 {
			req.SlowMoMS = 700
		}
	}
	if req.Visual {
		req.Headed = true
		if req.SlowMoMS == 0 {
			req.SlowMoMS = 700
		}
	}
	req.UseGraph = true

	progressChan := make(chan orchestrator.ProgressEvent, 32)
	opts := orchestrator.Options{
		Config:       s.config,
		Repository:   s.repo,
		ToolRegistry: s.toolRegistry,
		ProgressChan: progressChan,
	}

	gr, err := orchestrator.NewGraphRunner(opts)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		gr.Run(ctx, req, taskID)
		close(progressChan)
	}()

	for evt := range progressChan {
		fmt.Fprint(w, evt.SSE())
		flusher.Flush()
		if evt.Type == orchestrator.ProgressDone || evt.Type == orchestrator.ProgressError {
			return
		}
	}
}

func (s *Server) workflowGraph(w http.ResponseWriter, r *http.Request) {
	gr, err := orchestrator.NewGraphRunner(orchestrator.Options{Config: s.config, Repository: s.repo, ToolRegistry: s.toolRegistry})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	format := r.URL.Query().Get("format")
	if format == "svg" {
		outputPath := filepath.Join(s.config.Artifacts.Root, "workflow.svg")
		if err := gr.RenderImage(r.Context(), "svg", outputPath); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		http.ServeFile(w, r, outputPath)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(gr.DOT()))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
