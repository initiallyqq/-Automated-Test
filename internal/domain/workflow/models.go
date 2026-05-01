package workflow

type ProjectProfile struct {
	ProjectType string   `json:"projectType"`
	Languages   []string `json:"languages"`
	Frameworks  []string `json:"frameworks"`
	PackageTool string   `json:"packageTool,omitempty"`
	Risks       []string `json:"risks,omitempty"`
}

type PageGraph struct {
	Pages []PageNode `json:"pages"`
}

type PageNode struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Route string `json:"route"`
}

type ApiGraph struct {
	Endpoints []ApiEndpoint `json:"endpoints"`
}

type ApiEndpoint struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
}

type DataModelGraph struct {
	Models []DataModel `json:"models"`
}

type DataModel struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
}

type ScenarioPlan struct {
	Scenarios []Scenario `json:"scenarios"`
}

type Scenario struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Level         string   `json:"level"`
	Priority      int      `json:"priority"`
	Preconditions []string `json:"preconditions,omitempty"`
	Steps         []string `json:"steps"`
	Assertions    []string `json:"assertions"`
}

type TestArtifact struct {
	ID          string `json:"id"`
	ScenarioID  string `json:"scenarioId"`
	Type        string `json:"type"`
	Path        string `json:"path"`
	Language    string `json:"language"`
	ContentHash string `json:"contentHash,omitempty"`
}

type ExecutionResult struct {
	ExecutionID    string `json:"executionId"`
	Passed         bool   `json:"passed"`
	PassedCount    int    `json:"passedCount"`
	FailedCount    int    `json:"failedCount"`
	ResultPath     string `json:"resultPath,omitempty"`
	TracePath      string `json:"tracePath,omitempty"`
	HtmlReportPath string `json:"htmlReportPath,omitempty"`
	Mode           string `json:"mode,omitempty"`
	StdoutPath     string `json:"stdoutPath,omitempty"`
	StderrPath     string `json:"stderrPath,omitempty"`
	LogPath        string `json:"logPath,omitempty"`
}

type DiagnosisResult struct {
	FailureType string         `json:"failureType"`
	RootCause   string         `json:"rootCause"`
	Confidence  float64        `json:"confidence"`
	Evidence    []EvidenceItem `json:"evidence"`
	FixTargets  []string       `json:"fixTargets,omitempty"`
	NextAction  string         `json:"nextAction"`
}

type EvidenceItem struct {
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Ref     string `json:"ref,omitempty"`
}

type Patch struct {
	ID         string `json:"id"`
	TargetPath string `json:"targetPath"`
	Diff       string `json:"diff"`
	RiskLevel  string `json:"riskLevel"`
	Applied    bool   `json:"applied"`
	Rationale  string `json:"rationale"`
}
