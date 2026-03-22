package storage

import (
	"time"

	"gorm.io/gorm"
)

// User represents a user in the system
type User struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// Platform-specific user ID
	Platform       string `gorm:"size:50;not null" json:"platform"` // telegram, discord, etc.
	PlatformUserID string `gorm:"size:255;not null" json:"platform_user_id"`

	// User info
	Name     string `gorm:"size:255" json:"name"`
	Username string `gorm:"size:255" json:"username"`
	Language string `gorm:"size:10" json:"language"`

	// Settings
	Settings string `gorm:"type:text" json:"settings"` // JSON blob

	// Relations
	Agents   []Agent   `gorm:"foreignKey:OwnerID" json:"agents,omitempty"`
	Sessions []Session `gorm:"foreignKey:UserID" json:"sessions,omitempty"`
}

// Agent represents an AI agent instance
type Agent struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// Agent identity
	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description"`

	// Agent configuration
	Provider      string  `gorm:"size:50" json:"provider"`
	Model         string  `gorm:"size:100" json:"model"`
	Prompt        string  `gorm:"type:text" json:"prompt"`
	Tools         string  `gorm:"type:text" json:"tools"`  // legacy JSON array of tool definitions
	Skills        string  `gorm:"type:text" json:"skills"` // legacy JSON array of skill names
	EnabledTools  string  `gorm:"type:text" json:"enabled_tools"`
	EnabledSkills string  `gorm:"type:text" json:"enabled_skills"`
	Temperature   float64 `json:"temperature"`

	// Runtime state
	Status       string `gorm:"size:50" json:"status"`  // active, paused, stopped
	State        string `gorm:"type:text" json:"state"` // legacy JSON blob for runtime state
	RuntimeState string `gorm:"type:text" json:"runtime_state"`

	// Ownership
	OwnerID uint `gorm:"not null" json:"owner_id"`
	Owner   User `gorm:"foreignKey:OwnerID" json:"owner,omitempty"`

	// Routing info
	RoutingKey string `gorm:"size:255;index" json:"routing_key"` // For message routing

	// Relations
	Sessions []Session `gorm:"foreignKey:AgentID" json:"sessions,omitempty"`
}

// Session represents a conversation session
type Session struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// Session info
	SessionID string `gorm:"size:255;not null;uniqueIndex" json:"session_id"`
	Title     string `gorm:"size:255" json:"title"`

	// Related entities
	UserID  uint  `gorm:"not null;index" json:"user_id"`
	User    User  `gorm:"foreignKey:UserID" json:"user,omitempty"`
	AgentID uint  `gorm:"not null;index" json:"agent_id"`
	Agent   Agent `gorm:"foreignKey:AgentID" json:"agent,omitempty"`

	// Platform info
	Platform       string `gorm:"size:50" json:"platform"`
	PlatformChatID string `gorm:"size:255" json:"platform_chat_id"`

	// Session state
	Status string `gorm:"size:50" json:"status"`  // active, paused, closed
	State  string `gorm:"type:text" json:"state"` // JSON blob

	// Metadata
	Metadata string `gorm:"type:text" json:"metadata"` // JSON blob

	// Relations
	Messages []Message `gorm:"foreignKey:SessionID" json:"messages,omitempty"`
}

// Message represents a message in a session
type Message struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// Message content
	MessageID string `gorm:"size:255;not null;index" json:"message_id"`
	Content   string `gorm:"type:text;not null" json:"content"`
	Role      string `gorm:"size:50;not null" json:"role"` // user, assistant, system
	Kind      string `gorm:"size:50;index" json:"kind"`

	// Message metadata
	Type        string `gorm:"size:50" json:"type"` // text, image, voice, etc.
	MediaURL    string `gorm:"size:500" json:"media_url"`
	ToolName    string `gorm:"size:255;index" json:"tool_name"`
	ToolCallID  string `gorm:"size:255;index" json:"tool_call_id"`
	ToolPayload string `gorm:"type:text" json:"tool_payload"`

	// Token usage
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`

	// Related entities
	SessionID uint    `gorm:"not null;index" json:"session_id"`
	Session   Session `gorm:"foreignKey:SessionID" json:"session,omitempty"`

	// Platform message ID (for reference)
	PlatformMessageID string `gorm:"size:255" json:"platform_message_id"`
}

// AgentRun stores a full agent execution trace.
type AgentRun struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	SessionIDRef uint       `gorm:"not null;index" json:"session_id_ref"`
	AgentID      uint       `gorm:"not null;index" json:"agent_id"`
	Status       string     `gorm:"size:50;index" json:"status"`
	Model        string     `gorm:"size:100" json:"model"`
	Provider     string     `gorm:"size:50" json:"provider"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	Error        string     `gorm:"type:text" json:"error"`
}

// ToolCallTrace stores a single tool invocation.
type ToolCallTrace struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	AgentRunID   uint   `gorm:"not null;index" json:"agent_run_id"`
	SessionIDRef uint   `gorm:"not null;index" json:"session_id_ref"`
	AgentID      uint   `gorm:"not null;index" json:"agent_id"`
	MessageID    uint   `gorm:"not null;index" json:"message_id"` // 关联到触发工具调用的消息 ID
	ToolName     string `gorm:"size:255;index" json:"tool_name"`
	CallID       string `gorm:"size:255;index" json:"call_id"`
	ToolInput    string `gorm:"type:text" json:"tool_input"`
	ToolOutput   string `gorm:"type:text" json:"tool_output"`
	Success      bool   `json:"success"`
	LatencyMs    int64  `json:"latency_ms"`
	Error        string `gorm:"type:text" json:"error"`
}

// Skill represents a skill definition
type Skill struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// Skill identity
	Name        string `gorm:"size:255;not null;uniqueIndex" json:"name"`
	Description string `gorm:"type:text" json:"description"`
	Version     string `gorm:"size:50" json:"version"`

	// Skill definition (YAML/JSON)
	Definition string `gorm:"type:text;not null" json:"definition"`

	// Skill source (file path or URL)
	Source string `gorm:"size:500" json:"source"`

	// Status
	IsEnabled bool   `gorm:"default:true" json:"is_enabled"`
	Category  string `gorm:"size:100" json:"category"`
}

// Workspace represents a workspace
type Workspace struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// Workspace identity
	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description"`

	// Workspace data (JSON)
	Data string `gorm:"type:text" json:"data"`
}

// ScheduledTask represents a scheduled job
type ScheduledTask struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	SessionID uint           `json:"session_id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	// Task identity
	Name        string `gorm:"size:255;not null;index" json:"name"`
	Description string `gorm:"type:text" json:"description"`

	// Task configuration
	AgentID uint   `gorm:"not null;index" json:"agent_id"`
	Kind    string `gorm:"size:20;not null;default:'cron'" json:"kind"` // "at", "every", "cron"

	// Schedule configuration
	CronExpr    string     `gorm:"size:100" json:"cron_expr"`         // For kind="cron"
	Interval    string     `gorm:"size:50" json:"interval"`           // For kind="every" (e.g., "1h", "30m")
	ScheduledAt *time.Time `gorm:"index" json:"scheduled_at"`         // For kind="at"
	DeleteAfter bool       `gorm:"default:false" json:"delete_after"` // Delete after successful run

	// Session configuration
	SessionTarget string `gorm:"size:100" json:"session_target"` // "main", "isolated", "current", "session:xxx"

	// Payload configuration
	PayloadKind string `gorm:"size:50;not null;default:'systemEvent'" json:"payload_kind"` // "systemEvent", "agentTurn"
	Input       string `gorm:"type:text" json:"input"`

	// State
	Enabled   bool       `gorm:"default:true" json:"enabled"`
	LastRunAt *time.Time `gorm:"index" json:"last_run_at"`
	NextRunAt *time.Time `gorm:"index" json:"next_run_at"`

	// Statistics
	TotalRuns   int `gorm:"default:0" json:"total_runs"`
	SuccessRuns int `gorm:"default:0" json:"success_runs"`
	FailedRuns  int `gorm:"default:0" json:"failed_runs"`
}

// TaskExecutionLog stores task execution history
type TaskExecutionLog struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	TaskID     uint       `gorm:"not null;index" json:"task_id"`
	SessionID  uint       `gorm:"not null;index" json:"session_id"`
	Status     string     `gorm:"size:50;index" json:"status"` // success, failed, running
	Input      string     `gorm:"type:text" json:"input"`
	Output     string     `gorm:"type:text" json:"output"`
	Error      string     `gorm:"type:text" json:"error"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	DurationMs int64      `json:"duration_ms"`
}
