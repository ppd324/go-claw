package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go-claw/internal/storage"
)

type SessionState struct {
	Mode         ExecutionMode `json:"mode"`
	PlanApproved bool          `json:"plan_approved"`
	PendingPlan  string        `json:"pending_plan,omitempty"`
	TurnCount    int           `json:"turn_count"`
	LastActivity time.Time     `json:"last_activity"`
	SystemPrompt string        `json:"system_prompt,omitempty"`
}

type SessionManager struct {
	repo       *storage.Repository
	maxHistory int
}

type SessionInfo struct {
	Session      *storage.Session
	Messages     []storage.Message
	State        SessionState
	MessageCount int
}

func NewSessionManager(repo *storage.Repository) *SessionManager {
	return &SessionManager{
		repo:       repo,
		maxHistory: 100,
	}
}

func (sm *SessionManager) CreateSession(userID, agentID uint, title, platform string) (*storage.Session, error) {
	session := &storage.Session{
		SessionID: generateSessionID(),
		Title:     title,
		UserID:    userID,
		AgentID:   agentID,
		Platform:  platform,
		Status:    "active",
		State:     sm.defaultState(),
	}

	if err := sm.repo.CreateSession(session); err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	return session, nil
}

func (sm *SessionManager) GetSession(id uint) (*storage.Session, error) {
	return sm.repo.GetSession(id)
}

func (sm *SessionManager) GetSessionBySessionID(sessionID string) (*storage.Session, error) {
	return sm.repo.GetSessionBySessionID(sessionID)
}

func (sm *SessionManager) GetOrCreateSession(userID, agentID uint, platform, platformChatID string) (*storage.Session, error) {
	if platformChatID != "" {
		sessions, err := sm.repo.GetSessionsByAgent(agentID)
		if err == nil {
			for _, s := range sessions {
				if s.Platform == platform && s.PlatformChatID == platformChatID {
					return &s, nil
				}
			}
		}
	}

	return sm.CreateSession(userID, agentID, "", platform)
}

func (sm *SessionManager) UpdateSession(session *storage.Session) error {
	return sm.repo.UpdateSession(session)
}

func (sm *SessionManager) ListUserSessions(userID uint) ([]storage.Session, error) {
	return sm.repo.GetSessionsByUser(userID)
}

func (sm *SessionManager) ListAgentSessions(agentID uint) ([]storage.Session, error) {
	return sm.repo.GetSessionsByAgent(agentID)
}

func (sm *SessionManager) DeleteSession(id uint) error {
	return sm.repo.DeleteSession(id)
}

func (sm *SessionManager) AddMessage(sessionID uint, role, content string) (*storage.Message, error) {
	msg := &storage.Message{
		SessionID: sessionID,
		Role:      role,
		Content:   content,
	}

	if err := sm.repo.CreateMessage(msg); err != nil {
		return nil, fmt.Errorf("failed to add message: %w", err)
	}

	session, err := sm.repo.GetSession(sessionID)
	if err == nil && session != nil {
		session.UpdatedAt = time.Now()
		sm.repo.UpdateSession(session)
	}

	return msg, nil
}

func (sm *SessionManager) GetMessages(sessionID uint) ([]storage.Message, error) {
	return sm.repo.GetMessagesBySession(sessionID)
}

func (sm *SessionManager) GetSessionInfo(sessionID uint) (*SessionInfo, error) {
	session, err := sm.repo.GetSession(sessionID)
	if err != nil {
		return nil, err
	}

	messages, err := sm.repo.GetMessagesBySession(sessionID)
	if err != nil {
		return nil, err
	}

	state := sm.parseState(session.State)

	return &SessionInfo{
		Session:      session,
		Messages:     messages,
		State:        state,
		MessageCount: len(messages),
	}, nil
}

func (sm *SessionManager) BuildConversationHistory(sessionID uint) (string, error) {
	messages, err := sm.repo.GetMessagesBySession(sessionID)
	if err != nil {
		return "", err
	}

	if len(messages) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for _, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		sb.WriteString(fmt.Sprintf("%s: %s\n\n", role, msg.Content))
	}

	return sb.String(), nil
}

func (sm *SessionManager) UpdateSessionState(sessionID uint, update func(*SessionState)) error {
	session, err := sm.repo.GetSession(sessionID)
	if err != nil {
		return err
	}

	state := sm.parseState(session.State)
	update(&state)
	session.State = sm.serializeState(state)

	return sm.repo.UpdateSession(session)
}

func (sm *SessionManager) SetExecutionMode(sessionID uint, mode ExecutionMode) error {
	return sm.UpdateSessionState(sessionID, func(s *SessionState) {
		s.Mode = mode
	})
}

func (sm *SessionManager) ApprovePlan(sessionID uint) error {
	return sm.UpdateSessionState(sessionID, func(s *SessionState) {
		s.PlanApproved = true
	})
}

func (sm *SessionManager) ClearPlan(sessionID uint) error {
	return sm.UpdateSessionState(sessionID, func(s *SessionState) {
		s.PlanApproved = false
		s.PendingPlan = ""
	})
}

func (sm *SessionManager) IncrementTurn(sessionID uint) error {
	return sm.UpdateSessionState(sessionID, func(s *SessionState) {
		s.TurnCount++
		s.LastActivity = time.Now()
	})
}

func (sm *SessionManager) SetSystemPrompt(sessionID uint, prompt string) error {
	return sm.UpdateSessionState(sessionID, func(s *SessionState) {
		s.SystemPrompt = prompt
	})
}

func (sm *SessionManager) GetSystemPrompt(sessionID uint) (string, error) {
	info, err := sm.GetSessionInfo(sessionID)
	if err != nil {
		return "", err
	}
	return info.State.SystemPrompt, nil
}

func (sm *SessionManager) TruncateHistory(sessionID uint, keepLast int) error {
	messages, err := sm.repo.GetMessagesBySession(sessionID)
	if err != nil {
		return err
	}

	if len(messages) <= keepLast {
		return nil
	}

	for _, msg := range messages[:len(messages)-keepLast] {
		if err := sm.repo.DeleteMessage(msg.ID); err != nil {
		}
	}

	return nil
}

func (sm *SessionManager) SetMaxHistory(max int) {
	sm.maxHistory = max
}

func (sm *SessionManager) defaultState() string {
	return sm.serializeState(SessionState{
		Mode:         ModeNormal,
		PlanApproved: false,
		TurnCount:    0,
		LastActivity: time.Now(),
	})
}

func (sm *SessionManager) parseState(data string) SessionState {
	if data == "" {
		return SessionState{
			Mode:         ModeNormal,
			PlanApproved: false,
			TurnCount:    0,
			LastActivity: time.Now(),
		}
	}

	var state SessionState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return SessionState{
			Mode:         ModeNormal,
			PlanApproved: false,
			TurnCount:    0,
			LastActivity: time.Now(),
		}
	}
	return state
}

func (sm *SessionManager) serializeState(state SessionState) string {
	data, err := json.Marshal(state)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func generateSessionID() string {
	return fmt.Sprintf("sess_%d_%d", time.Now().UnixMilli(), randomInt(1000, 9999))
}

func randomInt(min, max int) int {
	return min + int(time.Now().UnixNano()%int64(max-min))
}
