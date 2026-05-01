package workflow

type Phase string

const (
	PhasePending          Phase = "PENDING"
	PhaseProjectAnalysis  Phase = "PROJECT_ANALYSIS"
	PhaseScenarioPlanning Phase = "SCENARIO_PLANNING"
	PhaseTestGeneration   Phase = "TEST_GENERATION"
	PhaseTestExecution    Phase = "TEST_EXECUTION"
	PhaseFailureDiagnosis Phase = "FAILURE_DIAGNOSIS"
	PhaseTestFixing       Phase = "TEST_FIXING"
	PhaseReviewGuard      Phase = "REVIEW_GUARD"
	PhaseReRun            Phase = "RE_RUN"
	PhaseArchive          Phase = "ARCHIVE"
	PhaseDone             Phase = "DONE"
	PhaseFailed           Phase = "FAILED"
)

type Status string

const (
	StatusPending          Status = "PENDING"
	StatusRunning          Status = "RUNNING"
	StatusRetrying         Status = "RETRYING"
	StatusWaitingReview    Status = "WAITING_REVIEW"
	StatusSucceeded        Status = "SUCCEEDED"
	StatusPartialSucceeded Status = "PARTIAL_SUCCEEDED"
	StatusFailed           Status = "FAILED"
	StatusTerminated       Status = "TERMINATED"
)

type State struct {
	TaskID      string `json:"taskId"`
	ProjectID   string `json:"projectId"`
	RepoVersion string `json:"repoVersion"`

	Phase  Phase  `json:"phase"`
	Status Status `json:"status"`

	ProjectProfile  *ProjectProfile  `json:"projectProfile,omitempty"`
	PageGraph       *PageGraph       `json:"pageGraph,omitempty"`
	ApiGraph        *ApiGraph        `json:"apiGraph,omitempty"`
	DataModelGraph  *DataModelGraph  `json:"dataModelGraph,omitempty"`
	ScenarioPlan    *ScenarioPlan    `json:"scenarioPlan,omitempty"`
	TestWorkspace   string           `json:"testWorkspace,omitempty"`
	TestArtifacts   []TestArtifact   `json:"testArtifacts,omitempty"`
	ExecutionResult *ExecutionResult `json:"executionResult,omitempty"`
	DiagnosisResult *DiagnosisResult `json:"diagnosisResult,omitempty"`
	TestFixPatches  []Patch          `json:"testFixPatches,omitempty"`

	RetryState RetryState `json:"retryState"`
	GuardState GuardState `json:"guardState"`
	LastError  string     `json:"lastError,omitempty"`
}

type RetryState struct {
	TotalRetryCount     int `json:"totalRetryCount"`
	TestFixRetryCount   int `json:"testFixRetryCount"`
	ExecutionRetryCount int `json:"executionRetryCount"`
	DiagnosisRetryCount int `json:"diagnosisRetryCount"`
	MaxTotalRetry       int `json:"maxTotalRetry"`
	MaxTestFixRetry     int `json:"maxTestFixRetry"`
	MaxExecutionRetry   int `json:"maxExecutionRetry"`
}

type GuardState struct {
	RiskLevel        string `json:"riskLevel"`
	PatchAllowed     bool   `json:"patchAllowed"`
	RerunAllowed     bool   `json:"rerunAllowed"`
	NeedsHumanReview bool   `json:"needsHumanReview"`
	BlockedReason    string `json:"blockedReason,omitempty"`
}

func NewState(taskID, projectID string, maxFixRetry int) State {
	return State{
		TaskID:    taskID,
		ProjectID: projectID,
		Phase:     PhasePending,
		Status:    StatusPending,
		RetryState: RetryState{
			MaxTotalRetry:     5,
			MaxTestFixRetry:   maxFixRetry,
			MaxExecutionRetry: 2,
		},
		GuardState: GuardState{
			RiskLevel: "LOW",
		},
	}
}
