package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"automated-test/internal/domain/workflow"
	"automated-test/internal/infra/agentruntime"
)

type Repository struct {
	db *DB
}

func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) UpsertProject(ctx context.Context, id, name, repoPath, projectType string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO projects(id, name, repo_path, project_type, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  name = excluded.name,
  repo_path = excluded.repo_path,
  project_type = excluded.project_type,
  updated_at = excluded.updated_at;`, id, name, repoPath, projectType, now, now)
	return err
}

func (r *Repository) SaveTask(ctx context.Context, state workflow.State) error {
	now := time.Now().UTC().Format(time.RFC3339)
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
INSERT INTO workflow_tasks(id, project_id, repo_version, phase, status, retry_count, state_json, last_error, created_at, updated_at, finished_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  phase = excluded.phase,
  status = excluded.status,
  retry_count = excluded.retry_count,
  state_json = excluded.state_json,
  last_error = excluded.last_error,
  updated_at = excluded.updated_at,
  finished_at = excluded.finished_at;`,
		state.TaskID,
		state.ProjectID,
		state.RepoVersion,
		string(state.Phase),
		string(state.Status),
		state.RetryState.TotalRetryCount,
		string(stateJSON),
		nullableString(state.LastError),
		now,
		now,
		finishedAt(state),
	)
	return err
}

func (r *Repository) SaveEvent(ctx context.Context, taskID string, from, to workflow.Phase, status workflow.Status, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	id := "evt_" + time.Now().UTC().Format("20060102150405.000000000")
	_, err := r.db.ExecContext(ctx, `
INSERT INTO workflow_events(id, task_id, from_phase, to_phase, status, reason, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?);`,
		id,
		taskID,
		string(from),
		string(to),
		string(status),
		reason,
		now,
	)
	return err
}

func (r *Repository) GetTask(ctx context.Context, taskID string) (workflow.State, error) {
	var stateJSON string
	if err := r.db.QueryRowContext(ctx, `SELECT state_json FROM workflow_tasks WHERE id = ?`, taskID).Scan(&stateJSON); err != nil {
		return workflow.State{}, err
	}
	var state workflow.State
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return workflow.State{}, err
	}
	return state, nil
}

func (r *Repository) SaveAgentRun(ctx context.Context, record agentruntime.RunRecord) error {
	if record.ID == "" {
		record.ID = fmt.Sprintf("agent_%d", time.Now().UnixNano())
	}
	startedAt := record.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	status := record.Status
	if status == "" {
		status = agentruntime.RunStatusSucceeded
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO agent_runs(id, task_id, agent_name, input_summary, output_summary, input_json_path, output_json_path, status, error, started_at, finished_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  output_summary = excluded.output_summary,
  output_json_path = excluded.output_json_path,
  status = excluded.status,
  error = excluded.error,
  finished_at = excluded.finished_at;`,
		record.ID,
		record.TaskID,
		record.AgentName,
		nullableString(record.InputSummary),
		nullableString(record.OutputSummary),
		nullableString(record.InputJSONPath),
		nullableString(record.OutputJSONPath),
		status,
		nullableString(record.Error),
		startedAt.UTC().Format(time.RFC3339Nano),
		nullableTime(record.FinishedAt),
	)
	return err
}

func (r *Repository) GetAgentRuns(ctx context.Context, taskID string) ([]agentruntime.RunRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, task_id, agent_name, input_summary, output_summary, input_json_path, output_json_path, status, error, started_at, finished_at
FROM agent_runs
WHERE task_id = ?
ORDER BY started_at, id;`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []agentruntime.RunRecord{}
	for rows.Next() {
		var record agentruntime.RunRecord
		var inputSummary, outputSummary, inputJSONPath, outputJSONPath, errorText, finishedAt sqlNullString
		var startedAt string
		if err := rows.Scan(
			&record.ID,
			&record.TaskID,
			&record.AgentName,
			&inputSummary,
			&outputSummary,
			&inputJSONPath,
			&outputJSONPath,
			&record.Status,
			&errorText,
			&startedAt,
			&finishedAt,
		); err != nil {
			return nil, err
		}
		record.InputSummary = inputSummary.String
		record.OutputSummary = outputSummary.String
		record.InputJSONPath = inputJSONPath.String
		record.OutputJSONPath = outputJSONPath.String
		record.Error = errorText.String
		parsedStartedAt, err := time.Parse(time.RFC3339Nano, startedAt)
		if err != nil {
			return nil, err
		}
		record.StartedAt = parsedStartedAt
		if finishedAt.Valid {
			parsedFinishedAt, err := time.Parse(time.RFC3339Nano, finishedAt.String)
			if err != nil {
				return nil, err
			}
			record.FinishedAt = &parsedFinishedAt
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *Repository) SaveDiagnosis(ctx context.Context, taskID string, result workflow.DiagnosisResult) error {
	now := time.Now().UTC().Format(time.RFC3339)
	diagnosisJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}
	id := fmt.Sprintf("diag_%d", time.Now().UnixNano())
	_, err = r.db.ExecContext(ctx, `
INSERT INTO diagnoses(id, task_id, execution_id, failure_type, root_cause, confidence, fix_target, next_action, diagnosis_json, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`,
		id,
		taskID,
		nil,
		result.FailureType,
		result.RootCause,
		result.Confidence,
		nullableString(firstFixTarget(result.FixTargets)),
		result.NextAction,
		string(diagnosisJSON),
		now,
	)
	return err
}

func (r *Repository) SavePatch(ctx context.Context, taskID string, patch workflow.Patch) error {
	now := time.Now().UTC().Format(time.RFC3339)
	patchPath := "inline:" + patch.ID
	_, err := r.db.ExecContext(ctx, `
INSERT INTO patches(id, task_id, diagnosis_id, target_path, patch_path, risk_level, applied, rationale, created_at, applied_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  risk_level = excluded.risk_level,
  applied = excluded.applied,
  rationale = excluded.rationale,
  applied_at = excluded.applied_at;`,
		patch.ID,
		taskID,
		nil,
		patch.TargetPath,
		patchPath,
		patch.RiskLevel,
		boolInt(patch.Applied),
		patch.Rationale,
		now,
		appliedAt(patch),
	)
	return err
}

func (r *Repository) SaveTestArtifacts(ctx context.Context, taskID string, artifacts []workflow.TestArtifact) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := r.db.ExecContext(ctx, `DELETE FROM test_artifacts WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for _, artifact := range artifacts {
		id := artifact.ID
		if id == "" {
			id = fmt.Sprintf("artifact_%d", time.Now().UnixNano())
		}
		if _, err := r.db.ExecContext(ctx, `
INSERT INTO test_artifacts(id, task_id, scenario_id, type, path, language, content_hash, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?);`,
			id,
			taskID,
			nullableString(artifact.ScenarioID),
			artifact.Type,
			artifact.Path,
			nullableString(artifact.Language),
			nullableString(artifact.ContentHash),
			now,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) SaveExecution(ctx context.Context, taskID string, result workflow.ExecutionResult) error {
	now := time.Now().UTC().Format(time.RFC3339)
	id := result.ExecutionID
	if id == "" {
		id = fmt.Sprintf("exec_%d", time.Now().UnixNano())
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO executions(id, task_id, passed, passed_count, failed_count, mode, result_path, trace_path, html_report_path, stdout_path, stderr_path, log_path, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  passed = excluded.passed,
  passed_count = excluded.passed_count,
  failed_count = excluded.failed_count,
  mode = excluded.mode,
  result_path = excluded.result_path,
  trace_path = excluded.trace_path,
  html_report_path = excluded.html_report_path,
  stdout_path = excluded.stdout_path,
  stderr_path = excluded.stderr_path,
  log_path = excluded.log_path;`,
		id,
		taskID,
		boolInt(result.Passed),
		result.PassedCount,
		result.FailedCount,
		nullableString(result.Mode),
		nullableString(result.ResultPath),
		nullableString(result.TracePath),
		nullableString(result.HtmlReportPath),
		nullableString(result.StdoutPath),
		nullableString(result.StderrPath),
		nullableString(result.LogPath),
		now,
	)
	return err
}

func firstFixTarget(targets []string) string {
	if len(targets) == 0 {
		return ""
	}
	return targets[0]
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

type sqlNullString struct {
	String string
	Valid  bool
}

func (s *sqlNullString) Scan(value any) error {
	if value == nil {
		s.String = ""
		s.Valid = false
		return nil
	}
	switch typed := value.(type) {
	case string:
		s.String = typed
	case []byte:
		s.String = string(typed)
	default:
		s.String = fmt.Sprint(typed)
	}
	s.Valid = true
	return nil
}

func finishedAt(state workflow.State) any {
	switch state.Status {
	case workflow.StatusSucceeded, workflow.StatusFailed, workflow.StatusPartialSucceeded, workflow.StatusTerminated:
		return time.Now().UTC().Format(time.RFC3339)
	default:
		return nil
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func appliedAt(patch workflow.Patch) any {
	if patch.Applied {
		return time.Now().UTC().Format(time.RFC3339)
	}
	return nil
}
