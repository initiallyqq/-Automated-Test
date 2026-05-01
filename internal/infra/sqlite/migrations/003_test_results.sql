CREATE TABLE IF NOT EXISTS test_artifacts (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  scenario_id TEXT,
  type TEXT NOT NULL,
  path TEXT NOT NULL,
  language TEXT,
  content_hash TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES workflow_tasks(id)
);

CREATE INDEX IF NOT EXISTS idx_test_artifacts_task_id ON test_artifacts(task_id);

CREATE TABLE IF NOT EXISTS executions (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  passed INTEGER NOT NULL,
  passed_count INTEGER NOT NULL DEFAULT 0,
  failed_count INTEGER NOT NULL DEFAULT 0,
  mode TEXT,
  result_path TEXT,
  trace_path TEXT,
  html_report_path TEXT,
  stdout_path TEXT,
  stderr_path TEXT,
  log_path TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES workflow_tasks(id)
);

CREATE INDEX IF NOT EXISTS idx_executions_task_id ON executions(task_id);
