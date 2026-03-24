package dashboard

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go-claw/internal/agent"
	"go-claw/internal/config"
	"go-claw/internal/notify"
	"go-claw/internal/scheduler"
	"go-claw/internal/storage"
)

type WebSocketNotifier interface {
	BroadcastNewMessage(sessionID string, message map[string]interface{})
}

const dashboardUserPlatform = "dashboard"

type Server struct {
	cfg          *config.Config
	agentManager *agent.Manager
	repo         *storage.Repository
	scheduler    *scheduler.Manager
	templates    *template.Template
	wsServer     WebSocketNotifier
}

type dashboardStats struct {
	TotalAgents    int `json:"total_agents"`
	ActiveSessions int `json:"active_sessions"`
	TotalMessages  int `json:"total_messages"`
}

type sessionView struct {
	ID           uint   `json:"id"`
	SessionID    string `json:"session_id"`
	Title        string `json:"title"`
	Platform     string `json:"platform"`
	Status       string `json:"status"`
	AgentID      uint   `json:"agent_id"`
	AgentName    string `json:"agent_name"`
	MessageCount int    `json:"message_count"`
	CreatedAt    string `json:"created_at"`
}

type pageData struct {
	Title           string
	ContentTemplate string
	Agents          []*agent.Agent
	Sessions        []sessionView
	Stats           dashboardStats
	Config          *config.Config
	Tasks           []storage.ScheduledTask
	WorkDir         string
	Files           []workspaceFile
}

// NewServer creates a new dashboard server.
func NewServer(cfg *config.Config, agentManager *agent.Manager, repo *storage.Repository, wsServer WebSocketNotifier) *Server {
	templates := template.New("dashboard")
	templates.Funcs(template.FuncMap{
		"formatSize": formatSize,
	})
	templateFiles := []string{
		"internal/dashboard/templates/layout.html",
		"internal/dashboard/templates/dashboard.html",
		"internal/dashboard/templates/conversations.html",
		"internal/dashboard/templates/conversation.html",
		"internal/dashboard/templates/agents.html",
		"internal/dashboard/templates/settings.html",
		"internal/dashboard/templates/scheduled-tasks.html",
		"internal/dashboard/templates/workspace.html",
	}

	templates = template.Must(templates.ParseFiles(templateFiles...))

	sched := scheduler.NewManager(repo, agentManager)

	// Register scheduler tools
	scheduleTool := scheduler.NewCreateScheduleTool(repo, sched)
	if err := agentManager.GetToolRegistry().Register(scheduleTool); err != nil {
		slog.Warn("failed to register schedule tool", "error", err)
	}

	return &Server{
		cfg:          cfg,
		agentManager: agentManager,
		repo:         repo,
		scheduler:    sched,
		templates:    templates,
		wsServer:     wsServer,
	}
}

// Register registers dashboard routes.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/dashboard", s.handleDashboard)
	mux.HandleFunc("/conversations", s.handleConversations)
	mux.HandleFunc("/c/", s.handleConversationDetail)
	mux.HandleFunc("/agents", s.handleAgents)
	mux.HandleFunc("/scheduled-tasks", s.handleScheduledTasks)
	mux.HandleFunc("/workspace", s.handleWorkspace)
	mux.HandleFunc("/settings", s.handleSettings)

	mux.HandleFunc("/conversation/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id != "" {
			http.Redirect(w, r, "/c/?id="+id, http.StatusMovedPermanently)
			return
		}
		http.Redirect(w, r, "/conversations", http.StatusFound)
	})

	mux.HandleFunc("/api/agents", s.handleAPIAgents)
	mux.HandleFunc("/api/agent", s.handleAPIAgent)
	mux.HandleFunc("/api/sessions", s.handleAPISessions)
	mux.HandleFunc("/api/session", s.handleAPISession)
	mux.HandleFunc("/api/messages", s.handleAPIMessages)
	mux.HandleFunc("/api/config", s.handleAPIConfig)
	mux.HandleFunc("/api/dashboard/stats", s.handleAPIStats)
	mux.HandleFunc("/api/workspace/files", s.handleAPIWorkspaceFiles)
	mux.HandleFunc("/api/workspace/file", s.handleAPIWorkspaceFile)

	// Scheduled task API routes
	mux.HandleFunc("/api/scheduled-tasks", s.handleAPIScheduledTasks)
	mux.HandleFunc("/api/scheduled-task", s.handleAPIScheduledTask)
	mux.HandleFunc("/api/task-logs", s.handleAPITaskLogs)
}

func (s *Server) SetNotifyRegistry(registry *notify.Registry) {
	s.scheduler.SetNotifyRegistry(registry)
}

func (s *Server) StartScheduler() {
	if s.scheduler != nil {
		// notifyFunc := func(taskID uint, taskName, sessionID, status, output string) {
		// 	if s.wsServer != nil {
		// 		payload := map[string]interface{}{
		// 			"task_id":    taskID,
		// 			"task_name":  taskName,
		// 			"session_id": sessionID,
		// 			"status":     status,
		// 			"output":     output,
		// 		}
		// 		s.wsServer.BroadcastNewMessage(sessionID, payload)
		// 		slog.Info("task notification sent", "task_id", taskID, "session_id", sessionID)
		// 	}
		// }
		// s.scheduler.SetNotifyFunc(notifyFunc)
		s.scheduler.Start()
	}
}

// StopScheduler stops the scheduler
func (s *Server) StopScheduler() {
	if s.scheduler != nil {
		s.scheduler.Stop()
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	agents, _ := s.agentManager.ListAgents()
	stats, _ := s.collectStats()
	_ = s.templates.ExecuteTemplate(w, "dashboard.html", pageData{
		Title:           "Dashboard",
		ContentTemplate: "dashboard.content",
		Agents:          agents,
		Stats:           stats,
	})
}

func (s *Server) handleConversations(w http.ResponseWriter, r *http.Request) {
	sessions, _ := s.listSessionViews()
	agents, _ := s.agentManager.ListAgents()
	_ = s.templates.ExecuteTemplate(w, "conversations.html", pageData{
		Title:           "Conversations",
		ContentTemplate: "conversations.content",
		Sessions:        sessions,
		Agents:          agents,
	})
}

func (s *Server) handleConversationDetail(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("id")

	type conversationPageData struct {
		Title           string
		ContentTemplate string
		SessionID       string
	}

	_ = s.templates.ExecuteTemplate(w, "conversation.html", conversationPageData{
		Title:           "Conversation",
		ContentTemplate: "conversation.content",
		SessionID:       sessionID,
	})
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	agents, _ := s.agentManager.ListAgents()
	_ = s.templates.ExecuteTemplate(w, "agents.html", pageData{
		Title:           "Agents",
		ContentTemplate: "agents.content",
		Agents:          agents,
	})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	_ = s.templates.ExecuteTemplate(w, "settings.html", pageData{
		Title:           "Settings",
		ContentTemplate: "settings.content",
		Config:          s.cfg,
	})
}

func (s *Server) handleScheduledTasks(w http.ResponseWriter, r *http.Request) {
	slog.Info("handling scheduled tasks page")

	tasks, err := s.repo.GetScheduledTasks()
	if err != nil {
		slog.Error("failed to load tasks", "error", err)
		http.Error(w, "Failed to load tasks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("loaded tasks", "count", len(tasks))

	agents, err := s.agentManager.ListAgents()
	if err != nil {
		slog.Error("failed to load agents", "error", err)
		http.Error(w, "Failed to load agents: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("loaded agents", "count", len(agents))

	err = s.templates.ExecuteTemplate(w, "scheduled-tasks.html", pageData{
		Title:           "Scheduled Tasks",
		ContentTemplate: "scheduled-tasks.content",
		Tasks:           tasks,
		Agents:          agents,
	})

	if err != nil {
		slog.Error("template execution failed", "error", err)
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("scheduled tasks page rendered successfully")
}

func (s *Server) handleAPIAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		agents, err := s.agentManager.ListAgents()
		if err != nil {
			s.JSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.JSON(w, agents)
	case http.MethodPost:
		payload, err := s.decodeAgentPayload(r)
		if err != nil {
			s.JSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		a, err := s.agentManager.CreateAgent(payload.Name, payload.Description, payload.Model, payload.Prompt)
		if err != nil {
			s.JSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.JSON(w, a)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIAgent(w http.ResponseWriter, r *http.Request) {
	id, err := s.parseUintQuery(r, "id")
	if err != nil {
		s.JSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		a, err := s.agentManager.GetAgent(id)
		if err != nil {
			s.JSONError(w, "agent not found", http.StatusNotFound)
			return
		}
		s.JSON(w, a)
	case http.MethodPut:
		current, err := s.agentManager.GetAgent(id)
		if err != nil {
			s.JSONError(w, "agent not found", http.StatusNotFound)
			return
		}
		payload, err := s.decodeAgentPayload(r)
		if err != nil {
			s.JSONError(w, err.Error(), http.StatusBadRequest)
			return
		}
		current.Name = payload.Name
		current.Description = payload.Description
		current.Model = payload.Model
		current.Prompt = payload.Prompt
		if payload.Status != "" {
			current.Status = payload.Status
		}
		if err := s.agentManager.UpdateAgent(current); err != nil {
			s.JSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.JSON(w, current)
	case http.MethodDelete:
		if err := s.agentManager.DeleteAgent(id); err != nil {
			s.JSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.JSON(w, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPISessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessions, err := s.listSessionViews()
		if err != nil {
			s.JSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.JSON(w, sessions)
	case http.MethodPost:
		var req struct {
			AgentID uint   `json:"agent_id"`
			Title   string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.JSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.AgentID == 0 {
			s.JSONError(w, "agent_id required", http.StatusBadRequest)
			return
		}
		if _, err := s.agentManager.GetAgent(req.AgentID); err != nil {
			s.JSONError(w, "agent not found", http.StatusNotFound)
			return
		}
		user, err := s.ensureDashboardUser()
		if err != nil {
			s.JSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		title := strings.TrimSpace(req.Title)
		if title == "" {
			title = "New Conversation"
		}
		session := &storage.Session{
			SessionID:      generateSessionID(),
			Title:          title,
			UserID:         user.ID,
			AgentID:        req.AgentID,
			Platform:       dashboardUserPlatform,
			PlatformChatID: fmt.Sprintf("dashboard-user-%d", user.ID),
			Status:         "active",
		}
		if err := s.repo.CreateSession(session); err != nil {
			s.JSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.JSON(w, session)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPISession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("id")
	if sessionID == "" {
		s.JSONError(w, "session_id required", http.StatusBadRequest)
		return
	}

	session, err := s.repo.GetSessionBySessionID(sessionID)
	if err != nil {
		s.JSONError(w, "session not found", http.StatusNotFound)
		return
	}

	messages, _ := s.repo.GetMessagesBySession(session.ID)

	// 为每条 assistant 消息附加工具调用
	messagesWithTools := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		msgData := map[string]any{
			"id":         msg.ID,
			"message_id": msg.MessageID,
			"content":    msg.Content,
			"role":       msg.Role,
			"created_at": msg.CreatedAt,
		}

		// 如果是 assistant 消息，查询关联的工具调用
		if msg.Role == "assistant" {
			toolCalls, _ := s.repo.GetToolCallsByMessageID(msg.ID)
			if len(toolCalls) > 0 {
				tools := make([]map[string]any, 0, len(toolCalls))
				for _, tc := range toolCalls {
					tools = append(tools, map[string]any{
						"tool_name":  tc.ToolName,
						"input":      tc.ToolInput,
						"output":     tc.ToolOutput,
						"success":    tc.Success,
						"created_at": tc.CreatedAt,
					})
				}
				msgData["tool_calls"] = tools
			}
		}

		messagesWithTools = append(messagesWithTools, msgData)
	}

	response := map[string]any{
		"session":  session,
		"messages": messagesWithTools,
	}
	s.JSON(w, response)
}

func (s *Server) handleAPIMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
		Content   string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.JSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Content = strings.TrimSpace(req.Content)
	if req.SessionID == "" || req.Content == "" {
		s.JSONError(w, "session_id and content required", http.StatusBadRequest)
		return
	}

	session, err := s.repo.GetSessionBySessionID(req.SessionID)
	if err != nil {
		s.JSONError(w, "session not found", http.StatusNotFound)
		return
	}

	userMsg := &storage.Message{
		MessageID: generateMessageID(),
		Content:   req.Content,
		Role:      "user",
		SessionID: session.ID,
	}

	a, err := s.agentManager.GetAgent(session.AgentID)
	if err != nil {
		s.JSONError(w, "agent not found", http.StatusNotFound)
		return
	}

	result, err := a.Execute(r.Context(), agent.ExecuteRequest{
		SessionID:        session.ID,
		Input:            req.Content,
		SaveInputMessage: true,
	})
	if err != nil {
		s.JSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	assistantMsg := &storage.Message{
		MessageID:    generateMessageID(),
		Content:      result.Content,
		Role:         "assistant",
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		SessionID:    session.ID,
	}

	response := map[string]any{
		"content":       result.Content,
		"input_tokens":  result.InputTokens,
		"output_tokens": result.OutputTokens,
		"stop_reason":   result.StopReason,
		"session_id":    session.SessionID,
		"user_message":  userMsg,
		"assistant_msg": assistantMsg,
		"tool_calls":    result.ToolCalls,
	}

	// Send WebSocket notification
	slog.Info("sending websocket notification", "session_id", session.SessionID)
	s.NotifyNewMessage(session.SessionID, response)

	s.JSON(w, response)
}

func (s *Server) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.JSON(w, s.cfg)
	case http.MethodPut:
		s.JSON(w, map[string]any{
			"ok":      true,
			"message": "runtime config persistence is not implemented yet",
		})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.collectStats()
	if err != nil {
		s.JSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.JSON(w, stats)
}

func (s *Server) collectStats() (dashboardStats, error) {
	agents, err := s.agentManager.ListAgents()
	if err != nil {
		return dashboardStats{}, err
	}
	sessions, err := s.repo.ListSessions()
	if err != nil {
		return dashboardStats{}, err
	}

	stats := dashboardStats{
		TotalAgents: len(agents),
	}
	for _, session := range sessions {
		if session.Status == "active" {
			stats.ActiveSessions++
		}
		messages, err := s.repo.GetMessagesBySession(session.ID)
		if err == nil {
			stats.TotalMessages += len(messages)
		}
	}
	return stats, nil
}

func (s *Server) listSessionViews() ([]sessionView, error) {
	sessions, err := s.repo.ListSessions()
	if err != nil {
		return nil, err
	}

	views := make([]sessionView, 0, len(sessions))
	for _, session := range sessions {
		agentName := "Unknown"
		if a, err := s.repo.GetAgent(session.AgentID); err == nil {
			agentName = a.Name
		}

		messageCount := 0
		if messages, err := s.repo.GetMessagesBySession(session.ID); err == nil {
			messageCount = len(messages)
		}

		views = append(views, sessionView{
			ID:           session.ID,
			SessionID:    session.SessionID,
			Title:        session.Title,
			Platform:     session.Platform,
			Status:       session.Status,
			AgentID:      session.AgentID,
			AgentName:    agentName,
			MessageCount: messageCount,
			CreatedAt:    session.CreatedAt.Format("2006-01-02 15:04"),
		})
	}

	return views, nil
}

func (s *Server) decodeAgentPayload(r *http.Request) (*struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Model       string `json:"model"`
	Prompt      string `json:"prompt"`
	Status      string `json:"status"`
}, error) {
	payload := &struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Model       string `json:"model"`
		Prompt      string `json:"prompt"`
		Status      string `json:"status"`
	}{}

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(payload); err != nil {
			return nil, fmt.Errorf("invalid json body")
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return nil, fmt.Errorf("invalid form body")
		}
		payload.Name = r.FormValue("name")
		payload.Description = r.FormValue("description")
		payload.Model = r.FormValue("model")
		payload.Prompt = r.FormValue("prompt")
		payload.Status = r.FormValue("status")
	}

	payload.Name = strings.TrimSpace(payload.Name)
	payload.Description = strings.TrimSpace(payload.Description)
	payload.Model = strings.TrimSpace(payload.Model)
	payload.Prompt = strings.TrimSpace(payload.Prompt)
	payload.Status = strings.TrimSpace(payload.Status)

	if payload.Name == "" {
		return nil, fmt.Errorf("name required")
	}
	return payload, nil
}

func (s *Server) ensureDashboardUser() (*storage.User, error) {
	user, err := s.repo.GetOrCreateUser(dashboardUserPlatform, "dashboard", "Dashboard User", "dashboard")
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *Server) parseUintQuery(r *http.Request, key string) (uint, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0, fmt.Errorf("%s required", key)
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return uint(parsed), nil
}

func (s *Server) JSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) JSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func generateSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UnixNano())
}

func generateMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}

// Scheduled Task API Handlers

func (s *Server) handleAPIScheduledTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tasks, err := s.repo.GetScheduledTasks()
		if err != nil {
			s.JSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.JSON(w, tasks)

	case http.MethodPost:
		var payload struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			AgentID     uint   `json:"agent_id"`
			CronExpr    string `json:"cron_expr"`
			Input       string `json:"input"`
			SessionID   uint   `json:"session_id"`
			Enabled     bool   `json:"enabled"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.JSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Validate cron expression
		if _, err := time.Parse("2006-01-02 15:04:05", time.Now().Format("2006-01-02")+" "+payload.CronExpr); err != nil {
			// Try parsing as standard cron (without seconds)
			fields := strings.Fields(payload.CronExpr)
			if len(fields) < 5 {
				s.JSONError(w, "invalid cron expression (required format: minute hour day month weekday)", http.StatusBadRequest)
				return
			}
		}

		task := &storage.ScheduledTask{
			Name:        payload.Name,
			Description: payload.Description,
			AgentID:     payload.AgentID,
			CronExpr:    payload.CronExpr,
			Input:       payload.Input,
			SessionID:   payload.SessionID,
			Enabled:     payload.Enabled,
		}

		if err := s.repo.CreateScheduledTask(task); err != nil {
			s.JSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Add to scheduler if enabled
		if task.Enabled {
			if err := s.scheduler.AddTask(task); err != nil {
				slog.Warn("failed to add task to scheduler", "task_name", task.Name, "task_id", task.ID, "error", err)
			}
		}

		s.JSON(w, task)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIScheduledTask(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		s.JSONError(w, "id required", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		s.JSONError(w, "invalid id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		task, err := s.repo.GetScheduledTask(uint(id))
		if err != nil {
			s.JSONError(w, "task not found", http.StatusNotFound)
			return
		}
		s.JSON(w, task)

	case http.MethodPut:
		task, err := s.repo.GetScheduledTask(uint(id))
		if err != nil {
			s.JSONError(w, "task not found", http.StatusNotFound)
			return
		}

		var payload struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			AgentID     uint   `json:"agent_id"`
			CronExpr    string `json:"cron_expr"`
			Input       string `json:"input"`
			SessionID   uint   `json:"session_id"`
			Enabled     bool   `json:"enabled"`
		}

		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.JSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}

		task.Name = payload.Name
		task.Description = payload.Description
		task.AgentID = payload.AgentID
		task.CronExpr = payload.CronExpr
		task.Input = payload.Input
		task.SessionID = payload.SessionID
		task.Enabled = payload.Enabled

		if err := s.repo.UpdateScheduledTask(task); err != nil {
			s.JSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Reload task in scheduler
		if err := s.scheduler.ReloadTask(task.ID); err != nil {
			slog.Warn("failed to reload task in scheduler", "task_name", task.Name, "task_id", task.ID, "error", err)
		}

		s.JSON(w, task)

	case http.MethodDelete:
		if err := s.repo.DeleteScheduledTask(uint(id)); err != nil {
			s.JSONError(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Remove from scheduler
		s.scheduler.RemoveTask(uint(id))

		s.JSON(w, map[string]any{"ok": true})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPITaskLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	taskIDStr := r.URL.Query().Get("task_id")
	limitStr := r.URL.Query().Get("limit")

	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	var logs []storage.TaskExecutionLog
	var err error

	if taskIDStr != "" {
		taskID, err := strconv.ParseUint(taskIDStr, 10, 64)
		if err != nil {
			s.JSONError(w, "invalid task_id", http.StatusBadRequest)
			return
		}
		logs, err = s.repo.GetTaskExecutionLogs(uint(taskID), limit)
	} else {
		logs, err = s.repo.GetRecentTaskExecutionLogs(limit)
	}

	if err != nil {
		s.JSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.JSON(w, logs)
}

// NotifyNewMessage notifies WebSocket clients about new messages
func (s *Server) NotifyNewMessage(sessionID string, message map[string]interface{}) {
	if s.wsServer != nil {
		s.wsServer.BroadcastNewMessage(sessionID, message)
	}
}

// handleWorkspace handles workspace files page
func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	slog.Info("handling workspace page")

	workDir := s.cfg.WorkDir
	files, err := s.getWorkspaceFiles(workDir)
	if err != nil {
		slog.Error("failed to list workspace files", "error", err, "work_dir", workDir)
		http.Error(w, "Failed to list workspace files: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pageData := pageData{
		Title:           "Workspace",
		ContentTemplate: "workspace.content",
		Config:          s.cfg,
		WorkDir:         workDir,
		Files:           files,
	}

	_ = s.templates.ExecuteTemplate(w, "layout.html", pageData)
}

type workspaceFile struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
	IsDir   bool   `json:"is_dir"`
}

// getWorkspaceFiles lists all markdown files in workspace directory
func (s *Server) getWorkspaceFiles(workDir string) ([]workspaceFile, error) {
	var files []workspaceFile

	err := filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Only include markdown files and directories
		if info.IsDir() || strings.HasSuffix(strings.ToLower(info.Name()), ".md") {
			relPath, _ := filepath.Rel(workDir, path)
			files = append(files, workspaceFile{
				Name:    info.Name(),
				Path:    relPath,
				Size:    info.Size(),
				ModTime: info.ModTime().Format("2006-01-02 15:04"),
				IsDir:   info.IsDir(),
			})
		}
		return nil
	})

	return files, err
}

// handleAPIWorkspaceFiles handles API requests for workspace files list
func (s *Server) handleAPIWorkspaceFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	workDir := s.cfg.WorkDir
	files, err := s.getWorkspaceFiles(workDir)
	if err != nil {
		slog.Error("failed to list workspace files", "error", err)
		s.JSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.JSON(w, map[string]interface{}{
		"work_dir": workDir,
		"files":    files,
	})
}

// handleAPIWorkspaceFile handles API requests for individual workspace file
func (s *Server) handleAPIWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getWorkspaceFile(w, r)
	case http.MethodPut:
		s.saveWorkspaceFile(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// getWorkspaceFile reads a workspace file content
func (s *Server) getWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		s.JSONError(w, "file path required", http.StatusBadRequest)
		return
	}

	// Security: prevent directory traversal attacks
	if strings.Contains(filePath, "..") {
		s.JSONError(w, "invalid file path", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(s.cfg.WorkDir, filePath)

	// Verify the file is within workspace directory
	// Use filepath.Clean for reliable comparison
	cleanWorkDir := filepath.Clean(s.cfg.WorkDir)
	cleanFullPath := filepath.Clean(fullPath)

	if !strings.HasPrefix(cleanFullPath, cleanWorkDir) {
		s.JSONError(w, "file must be within workspace directory", http.StatusForbidden)
		return
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		slog.Error("failed to read workspace file", "error", err, "file_path", filePath)
		s.JSONError(w, "Failed to read file: "+err.Error(), http.StatusNotFound)
		return
	}

	s.JSON(w, map[string]interface{}{
		"path":    filePath,
		"content": string(content),
	})
}

// saveWorkspaceFile saves a workspace file
func (s *Server) saveWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.JSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		s.JSONError(w, "file path required", http.StatusBadRequest)
		return
	}

	// Security: prevent directory traversal attacks
	if strings.Contains(req.Path, "..") {
		s.JSONError(w, "invalid file path", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(s.cfg.WorkDir, req.Path)

	// Verify the file is within workspace directory
	// Use filepath.Clean and filepath.Abs for reliable comparison
	cleanWorkDir := filepath.Clean(s.cfg.WorkDir)
	cleanFullPath := filepath.Clean(fullPath)

	if !strings.HasPrefix(cleanFullPath, cleanWorkDir) {
		s.JSONError(w, "file must be within workspace directory", http.StatusForbidden)
		return
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Error("failed to create directory", "error", err, "dir", dir)
		s.JSONError(w, "Failed to create directory: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(fullPath, []byte(req.Content), 0644); err != nil {
		slog.Error("failed to save workspace file", "error", err, "file_path", req.Path)
		s.JSONError(w, "Failed to save file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("workspace file saved successfully", "file_path", req.Path)
	s.JSON(w, map[string]interface{}{
		"success": true,
		"path":    req.Path,
	})
}

// formatSize formats file size in bytes to human readable format
func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}
