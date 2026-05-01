CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  repo_path TEXT NOT NULL,
  project_type TEXT NOT NULL DEFAULT 'fullstack',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workflow_tasks (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  repo_version TEXT,
  phase TEXT NOT NULL,
  status TEXT NOT NULL,
  retry_count INTEGER NOT NULL DEFAULT 0,
  state_json TEXT NOT NULL,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  finished_at TEXT,
  FOREIGN KEY(project_id) REFERENCES projects(id)
);

CREATE INDEX IF NOT EXISTS idx_workflow_tasks_project_id ON workflow_tasks(project_id);
CREATE INDEX IF NOT EXISTS idx_workflow_tasks_phase_status ON workflow_tasks(phase, status);

CREATE TABLE IF NOT EXISTS workflow_events (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  from_phase TEXT,
  to_phase TEXT NOT NULL,
  status TEXT NOT NULL,
  reason TEXT,
  payload_json TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES workflow_tasks(id)
);

CREATE INDEX IF NOT EXISTS idx_workflow_events_task_id ON workflow_events(task_id);

CREATE TABLE IF NOT EXISTS agent_runs (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  agent_name TEXT NOT NULL,
  input_summary TEXT,
  output_summary TEXT,
  input_json_path TEXT,
  output_json_path TEXT,
  status TEXT NOT NULL,
  error TEXT,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  FOREIGN KEY(task_id) REFERENCES workflow_tasks(id)
);

CREATE INDEX IF NOT EXISTS idx_agent_runs_task_id ON agent_runs(task_id);
CREATE INDEX IF NOT EXISTS idx_agent_runs_agent_name ON agent_runs(agent_name);

CREATE TABLE IF NOT EXISTS diagnoses (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  execution_id TEXT,
  failure_type TEXT NOT NULL,
  root_cause TEXT NOT NULL,
  confidence REAL NOT NULL,
  fix_target TEXT,
  next_action TEXT,
  diagnosis_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY(task_id) REFERENCES workflow_tasks(id),
  FOREIGN KEY(execution_id) REFERENCES executions(id)
);

CREATE INDEX IF NOT EXISTS idx_diagnoses_task_id ON diagnoses(task_id);

CREATE TABLE IF NOT EXISTS patches (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  diagnosis_id TEXT,
  target_path TEXT NOT NULL,
  patch_path TEXT NOT NULL,
  risk_level TEXT NOT NULL,
  applied INTEGER NOT NULL DEFAULT 0,
  rationale TEXT,
  created_at TEXT NOT NULL,
  applied_at TEXT,
  FOREIGN KEY(task_id) REFERENCES workflow_tasks(id),
  FOREIGN KEY(diagnosis_id) REFERENCES diagnoses(id)
);

CREATE INDEX IF NOT EXISTS idx_patches_task_id ON patches(task_id);


