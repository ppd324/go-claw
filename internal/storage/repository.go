package storage

import (
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
)

// Repository provides data access methods
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new repository
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// User methods

func (r *Repository) CreateUser(user *User) error {
	return r.db.Create(user).Error
}

func (r *Repository) GetUser(id uint) (*User, error) {
	var user User
	err := r.db.First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) GetUserByPlatform(platform, platformUserID string) (*User, error) {
	var user User
	err := r.db.Where("platform = ? AND platform_user_id = ?", platform, platformUserID).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) UpdateUser(user *User) error {
	return r.db.Save(user).Error
}

func (r *Repository) DeleteUser(id uint) error {
	return r.db.Delete(&User{}, id).Error
}

func (r *Repository) ListUsers() ([]User, error) {
	var users []User
	err := r.db.Find(&users).Error
	return users, err
}

// Agent methods

func (r *Repository) CreateAgent(agent *Agent) error {
	return r.db.Create(agent).Error
}

func (r *Repository) GetAgent(id uint) (*Agent, error) {
	var agent Agent
	err := r.db.First(&agent, id).Error
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (r *Repository) GetAgentByRoutingKey(routingKey string) (*Agent, error) {
	var agent Agent
	err := r.db.Where("routing_key = ?", routingKey).First(&agent).Error
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (r *Repository) GetAgentsByOwner(ownerID uint) ([]Agent, error) {
	var agents []Agent
	err := r.db.Where("owner_id = ?", ownerID).Find(&agents).Error
	return agents, err
}

func (r *Repository) UpdateAgent(agent *Agent) error {
	return r.db.Save(agent).Error
}

func (r *Repository) DeleteAgent(id uint) error {
	return r.db.Delete(&Agent{}, id).Error
}

func (r *Repository) ListAgents() ([]Agent, error) {
	var agents []Agent
	err := r.db.Find(&agents).Error
	return agents, err
}

func (r *Repository) SaveAgentRun(run *AgentRun) error {
	return r.db.Save(run).Error
}

func (r *Repository) CreateAgentRun(run *AgentRun) error {
	return r.db.Create(run).Error
}

func (r *Repository) GetLatestAgentRun(agentID uint) (*AgentRun, error) {
	var run AgentRun
	err := r.db.Where("agent_id = ?", agentID).Order("created_at desc").First(&run).Error
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *Repository) CreateToolCallTrace(trace *ToolCallTrace) error {
	return r.db.Create(trace).Error
}

func (r *Repository) GetToolCallTracesByRun(runID uint) ([]ToolCallTrace, error) {
	var traces []ToolCallTrace
	err := r.db.Where("agent_run_id = ?", runID).Order("created_at asc").Find(&traces).Error
	return traces, err
}

// Session methods

func (r *Repository) CreateSession(session *Session) error {
	return r.db.Create(session).Error
}

func (r *Repository) GetSession(id uint) (*Session, error) {
	var session Session
	err := r.db.First(&session, id).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *Repository) GetSessionBySessionID(sessionID string) (*Session, error) {
	var session Session
	err := r.db.Where("session_id = ?", sessionID).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *Repository) GetSessionsByUser(userID uint) ([]Session, error) {
	var sessions []Session
	err := r.db.Where("user_id = ?", userID).Find(&sessions).Error
	return sessions, err
}

func (r *Repository) GetSessionsByAgent(agentID uint) ([]Session, error) {
	var sessions []Session
	err := r.db.Where("agent_id = ?", agentID).Find(&sessions).Error
	return sessions, err
}

func (r *Repository) UpdateSession(session *Session) error {
	return r.db.Save(session).Error
}

func (r *Repository) ListSessions() ([]Session, error) {
	var sessions []Session
	err := r.db.Order("created_at desc").Find(&sessions).Error
	return sessions, err
}

func (r *Repository) DeleteSession(id uint) error {
	return r.db.Delete(&Session{}, id).Error
}

// Message methods

func (r *Repository) CreateMessage(message *Message) error {
	return r.db.Create(message).Error
}

func (r *Repository) GetMessage(id uint) (*Message, error) {
	var message Message
	err := r.db.First(&message, id).Error
	if err != nil {
		return nil, err
	}
	return &message, nil
}

func (r *Repository) GetMessagesBySession(sessionID uint) ([]Message, error) {
	var messages []Message
	err := r.db.Where("session_id = ?", sessionID).Order("created_at asc").Find(&messages).Error
	return messages, err
}

func (r *Repository) GetToolCallsBySession(sessionID uint) ([]ToolCallTrace, error) {
	var traces []ToolCallTrace
	err := r.db.Where("session_id_ref = ?", sessionID).Order("created_at asc").Find(&traces).Error
	return traces, err
}

func (r *Repository) GetToolCallsByMessageID(messageID uint) ([]ToolCallTrace, error) {
	var traces []ToolCallTrace
	err := r.db.Where("message_id = ?", messageID).Order("created_at asc").Find(&traces).Error
	return traces, err
}

func (r *Repository) UpdateMessage(message *Message) error {
	return r.db.Save(message).Error
}

func (r *Repository) DeleteMessage(id uint) error {
	return r.db.Delete(&Message{}, id).Error
}

// Skill methods

func (r *Repository) CreateSkill(skill *Skill) error {
	return r.db.Create(skill).Error
}

func (r *Repository) GetSkill(id uint) (*Skill, error) {
	var skill Skill
	err := r.db.First(&skill, id).Error
	if err != nil {
		return nil, err
	}
	return &skill, nil
}

func (r *Repository) GetSkillByName(name string) (*Skill, error) {
	var skill Skill
	err := r.db.Where("name = ?", name).First(&skill).Error
	if err != nil {
		return nil, err
	}
	return &skill, nil
}

func (r *Repository) GetEnabledSkills() ([]Skill, error) {
	var skills []Skill
	err := r.db.Where("is_enabled = ?", true).Find(&skills).Error
	return skills, err
}

func (r *Repository) UpdateSkill(skill *Skill) error {
	return r.db.Save(skill).Error
}

func (r *Repository) DeleteSkill(id uint) error {
	return r.db.Delete(&Skill{}, id).Error
}

// Utility methods

func (r *Repository) GetOrCreateUser(platform, platformUserID, name, username string) (*User, error) {
	user, err := r.GetUserByPlatform(platform, platformUserID)
	if err == nil {
		return user, nil
	}

	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	// Create new user
	user = &User{
		Platform:       platform,
		PlatformUserID: platformUserID,
		Name:           name,
		Username:       username,
	}

	if err := r.CreateUser(user); err != nil {
		return nil, err
	}

	return user, nil
}

// JSON helpers
func (r *Repository) MarshalJSON(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	return string(data), err
}

func (r *Repository) UnmarshalJSON(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}

// GetDB returns the underlying database connection
func (r *Repository) GetDB() *gorm.DB {
	return r.db
}

// ScheduledTask methods

func (r *Repository) CreateScheduledTask(task *ScheduledTask) error {
	return r.db.Create(task).Error
}

func (r *Repository) GetScheduledTask(id uint) (*ScheduledTask, error) {
	var task ScheduledTask
	err := r.db.First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *Repository) GetScheduledTasks() ([]ScheduledTask, error) {
	var tasks []ScheduledTask
	err := r.db.Order("created_at desc").Find(&tasks).Error
	return tasks, err
}

func (r *Repository) GetEnabledScheduledTasks() ([]ScheduledTask, error) {
	var tasks []ScheduledTask
	err := r.db.Where("enabled = ?", true).Order("created_at desc").Find(&tasks).Error
	return tasks, err
}

func (r *Repository) UpdateScheduledTask(task *ScheduledTask) error {
	return r.db.Save(task).Error
}

func (r *Repository) DeleteScheduledTask(id uint) error {
	return r.db.Delete(&ScheduledTask{}, id).Error
}

// TaskExecutionLog methods

func (r *Repository) CreateTaskExecutionLog(log *TaskExecutionLog) error {
	return r.db.Create(log).Error
}

func (r *Repository) GetTaskExecutionLogs(taskID uint, limit int) ([]TaskExecutionLog, error) {
	var logs []TaskExecutionLog
	err := r.db.Where("task_id = ?", taskID).Order("created_at desc").Limit(limit).Find(&logs).Error
	return logs, err
}

func (r *Repository) GetRecentTaskExecutionLogs(limit int) ([]TaskExecutionLog, error) {
	var logs []TaskExecutionLog
	err := r.db.Order("created_at desc").Limit(limit).Find(&logs).Error
	return logs, err
}

// Transaction executes a function within a transaction
func (r *Repository) Transaction(fn func(*Repository) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		repo := &Repository{db: tx}
		return fn(repo)
	})
}

// Close closes the database connection
func (r *Repository) Close() error {
	sqlDB, err := r.db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database instance: %w", err)
	}
	return sqlDB.Close()
}
