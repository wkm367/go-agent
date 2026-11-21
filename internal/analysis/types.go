package analysis

// types.go 定义 Agent 运行状态、事件类型，
// 以及 Run、Step、ToolCall、Artifact、QueryResult 等核心数据结构。
import "time"

const (
	// 以下常量是分析任务 Run 的生命周期状态。
	RunStatusQueued    = "queued"
	RunStatusRunning   = "running"
	RunStatusSucceeded = "succeeded"
	RunStatusFailed    = "failed"
	RunStatusCancelled = "cancelled"

	// 以下常量描述单个执行步骤的状态。
	StepStatusRunning   = "running"
	StepStatusSucceeded = "succeeded"
	StepStatusFailed    = "failed"
	StepStatusSkipped   = "skipped"

	// 以下常量描述单次工具调用的状态。
	ToolStatusRunning   = "running"
	ToolStatusSucceeded = "succeeded"
	ToolStatusFailed    = "failed"
)

const (
	// 以下常量是 SSE 和分析任务事件流使用的稳定事件类型。
	EventRunStarted      = "run.started"
	EventPlanCreated     = "plan.created"
	EventStepStarted     = "step.started"
	EventStepCompleted   = "step.completed"
	EventToolStarted     = "tool.started"
	EventToolCompleted   = "tool.completed"
	EventAnswerDelta     = "answer.delta"
	EventArtifactCreated = "artifact.created"
	EventRunCompleted    = "run.completed"
	EventRunFailed       = "run.failed"
	EventRunCancelled    = "run.cancelled"
)

// PlanStep 表示分析任务计划中的一个执行阶段。
type PlanStep struct {
	Index       int    `json:"index"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// Run 是一次完整分析任务的持久化记录。
type Run struct {
	ID        string `gorm:"type:char(36);primaryKey" json:"id"`
	UserID    uint64 `gorm:"not null;index" json:"user_id"`
	SessionID string `gorm:"type:char(36);not null;index" json:"session_id"`
	// DatasetID 为空时分析内置电商库，不为空时分析用户上传的数据集。
	DatasetID string `gorm:"type:char(36);index" json:"dataset_id,omitempty"`
	Question  string `gorm:"type:text;not null" json:"question"`
	// Status 使用 RunStatus* 常量。
	Status string `gorm:"type:varchar(32);not null;index" json:"status"`
	// PlanJSON 是数据库中的计划快照，Plan 是解码后的对外结构。
	PlanJSON    string     `gorm:"type:text" json:"-"`
	Plan        []PlanStep `gorm:"-" json:"plan"`
	FinalAnswer string     `gorm:"type:mediumtext" json:"final_answer"`
	Error       string     `gorm:"type:text" json:"error,omitempty"`
	CurrentStep int        `gorm:"not null;default:0" json:"current_step"`
	Model       string     `gorm:"type:varchar(128);not null" json:"model"`
	// PromptVersion 用于追踪生成该结果时使用的提示词版本。
	PromptVersion string `gorm:"type:varchar(32);not null" json:"prompt_version"`
	// 以下字段记录模型调用产生的 Token 用量。
	PromptTokens     int        `gorm:"not null;default:0" json:"prompt_tokens"`
	CompletionTokens int        `gorm:"not null;default:0" json:"completion_tokens"`
	TotalTokens      int        `gorm:"not null;default:0" json:"total_tokens"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// TableName 指定 GORM 使用的分析任务表名。
func (Run) TableName() string {
	return "agent_runs"
}

// RunStep 是一次分析任务中的执行步骤记录。
type RunStep struct {
	ID       string `gorm:"type:char(36);primaryKey" json:"id"`
	RunID    string `gorm:"type:char(36);not null;index:idx_agent_steps_run_seq,priority:1" json:"run_id"`
	Sequence int    `gorm:"not null;index:idx_agent_steps_run_seq,priority:2" json:"sequence"`
	Type     string `gorm:"type:varchar(32);not null" json:"type"`
	Title    string `gorm:"type:varchar(255);not null" json:"title"`
	// Status 使用 StepStatus* 常量。
	Status string `gorm:"type:varchar(32);not null" json:"status"`
	// InputJSON 和 OutputJSON 保存步骤的原始输入输出，不在 API 中直接暴露。
	InputJSON  string     `gorm:"type:text" json:"-"`
	OutputJSON string     `gorm:"type:text" json:"-"`
	Error      string     `gorm:"type:text" json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// TableName 指定 GORM 使用的分析步骤表名。
func (RunStep) TableName() string {
	return "agent_steps"
}

// ToolCall 记录 Agent 调用的一次工具及其执行结果。
type ToolCall struct {
	ID        string `gorm:"type:char(36);primaryKey" json:"id"`
	RunID     string `gorm:"type:char(36);not null;index" json:"run_id"`
	StepID    string `gorm:"type:char(36);not null;index" json:"step_id"`
	Name      string `gorm:"type:varchar(64);not null" json:"name"`
	Arguments string `gorm:"type:text" json:"arguments"`
	Result    string `gorm:"type:mediumtext" json:"result,omitempty"`
	// Status 使用 ToolStatus* 常量。
	Status string `gorm:"type:varchar(32);not null" json:"status"`
	Error  string `gorm:"type:text" json:"error,omitempty"`
	// DurationMS 是工具调用的耗时，单位为毫秒。
	DurationMS int64      `gorm:"not null;default:0" json:"duration_ms"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// TableName 指定 GORM 使用的工具调用表名。
func (ToolCall) TableName() string {
	return "tool_calls"
}

// Artifact 是分析任务生成的图表等结构化产物。
type Artifact struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	RunID     string    `gorm:"type:char(36);not null;index" json:"run_id"`
	StepID    string    `gorm:"type:char(36);index" json:"step_id,omitempty"`
	Type      string    `gorm:"type:varchar(32);not null" json:"type"`
	Title     string    `gorm:"type:varchar(255);not null" json:"title"`
	Payload   string    `gorm:"type:mediumtext;not null" json:"payload,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定 GORM 使用的分析产物表名。
func (Artifact) TableName() string {
	return "artifacts"
}

// RunDetail 聚合一次分析任务及其步骤、工具调用和产物。
type RunDetail struct {
	Run       Run        `json:"run"`
	Steps     []RunStep  `json:"steps"`
	ToolCalls []ToolCall `json:"tool_calls"`
	Artifacts []Artifact `json:"artifacts"`
}

// Event 是发送给 SSE 订阅者的分析任务事件。
type Event struct {
	RunID     string    `json:"run_id"`
	Type      string    `json:"type"`
	Sequence  int64     `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data,omitempty"`
}

// ToolResult 是工具执行后返回给 Agent 的统一结果。
type ToolResult struct {
	// QueryResult 仅在 SQL 查询工具执行成功时存在。
	QueryResult *QueryResult
	// Content 是工具结果的文本摘要，用于回填模型上下文。
	Content string
	// Artifact 是工具生成的图表等结构化产物。
	Artifact *Artifact
}

// QueryResult 是只读 SQL 查询的规范化结果。
type QueryResult struct {
	SQL        string           `json:"sql"`
	Columns    []string         `json:"columns"`
	Rows       []map[string]any `json:"rows"`
	RowCount   int              `json:"row_count"`
	Truncated  bool             `json:"truncated"`
	DurationMS int64            `json:"duration_ms"`
}
