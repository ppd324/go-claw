package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go-claw/internal/agent"
	"go-claw/internal/config"
	"go-claw/internal/llm"
	"go-claw/internal/storage"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512 * 1024
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for development
		return true
	},
}

// WebSocketServer represents the WebSocket server
type WebSocketServer struct {
	cfg          *config.Config
	manager      *ClientManager
	agentManager *agent.Manager
	repo         *storage.Repository
	server       *http.Server
	mu           sync.RWMutex
	clients      map[string]*Client
}

// NewWebSocketServer creates a new WebSocket server
func NewWebSocketServer(cfg *config.Config, agentManager *agent.Manager, repo *storage.Repository) *WebSocketServer {
	return &WebSocketServer{
		cfg:          cfg,
		manager:      NewClientManager(),
		agentManager: agentManager,
		repo:         repo,
		clients:      make(map[string]*Client),
	}
}

// Start starts the WebSocket server
func (s *WebSocketServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)

	mux := http.NewServeMux()
	mux.HandleFunc(s.cfg.Server.WSPath, s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)

	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Start client manager
	s.manager.Start()

	log.Printf("WebSocket server starting on %s", addr)
	return s.server.ListenAndServe()
}

// handleWebSocket handles WebSocket connections
func (s *WebSocketServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// fmt.Println("=== WebSocket request received! ===")
	// slog.Info("websocket upgrade request received", "path", r.URL.Path)

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		slog.Error("websocket upgrade failed", "error", err)
		fmt.Println("WebSocket upgrade error:", err)
		return
	}

	// fmt.Println("=== WebSocket upgraded successfully! ===")
	// slog.Info("websocket upgraded successfully")

	// Generate client ID
	clientID := generateClientID()
	client := &Client{
		ID:       clientID,
		Conn:     conn,
		Send:     make(chan []byte, 256),
		AgentID:  0,
		UserID:   0,
		Platform: "websocket",
	}

	// Register client
	s.mu.Lock()
	s.clients[clientID] = client
	s.mu.Unlock()
	s.manager.register <- client

	// slog.Info("websocket client connected", "client_id", clientID, "total_clients", len(s.clients))

	// fmt.Println("=== Starting readPump and writePump goroutines ===")

	// Start goroutines
	go s.writePump(client)
	go s.readPump(client)
}

// readPump reads messages from the WebSocket
func (s *WebSocketServer) readPump(client *Client) {
	defer func() {
		s.manager.unregister <- client
		s.mu.Lock()
		delete(s.clients, client.ID)
		s.mu.Unlock()
		client.Conn.Close()
		slog.Info("websocket client disconnected", "client_id", client.ID, "total_clients", len(s.clients))
	}()

	client.Conn.SetReadLimit(maxMessageSize)
	client.Conn.SetReadDeadline(time.Now().Add(pongWait))
	client.Conn.SetPongHandler(func(string) error {
		client.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			break
		}

		// fmt.Println("=== WebSocket message received! ===")
		// fmt.Println("Message content:", string(message))
		// slog.Info("websocket message received", "client_id", client.ID, "message", string(message))

		s.handleMessage(client, message)
	}
}

// writePump writes messages to the WebSocket
func (s *WebSocketServer) writePump(client *Client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		client.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.Send:
			client.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Channel was closed
				client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := client.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current websocket message
			n := len(client.Send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-client.Send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			client.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage handles incoming WebSocket messages
func (s *WebSocketServer) handleMessage(client *Client, data []byte) {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		s.sendError(client, "invalid message format")
		return
	}

	switch msg.Type {
	case "auth":
		s.handleAuth(client, msg.Payload)
	case "message":
		s.handleChatMessage(client, msg.Payload)
	case "session":
		s.handleSession(client, msg.Payload)
	case "agent":
		s.handleAgentAction(client, msg.Payload)
	default:
		s.sendError(client, fmt.Sprintf("unknown message type: %s", msg.Type))
	}
}

// handleAuth handles authentication
func (s *WebSocketServer) handleAuth(client *Client, payload json.RawMessage) {
	var auth struct {
		UserID         uint   `json:"user_id"`
		Token          string `json:"token"`
		Platform       string `json:"platform"`
		PlatformChatID string `json:"platform_chat_id"`
	}

	if err := json.Unmarshal(payload, &auth); err != nil {
		slog.Error("failed to unmarshal auth payload", "error", err)
		s.sendError(client, "invalid auth payload")
		return
	}

	client.UserID = auth.UserID
	client.Platform = auth.Platform
	client.PlatformChatID = auth.PlatformChatID

	// slog.Info("websocket client authenticated", "client_id", client.ID, "platform", client.Platform, "platform_chat_id", client.PlatformChatID)

	// Send auth_ok response
	authOkMsg := map[string]interface{}{
		"client_id": client.ID,
	}
	// slog.Info("sending auth_ok response", "client_id", client.ID)
	s.sendMessage(client, "auth_ok", authOkMsg)
	// slog.Info("auth_ok response sent", "client_id", client.ID)
}

// handleChatMessage handles chat messages
func (s *WebSocketServer) handleChatMessage(client *Client, payload json.RawMessage) {
	var msg struct {
		Content   string `json:"content"`
		SessionID string `json:"session_id,omitempty"`
		AgentID   uint   `json:"agent_id,omitempty"`
		Stream    bool   `json:"stream,omitempty"`
	}

	if err := json.Unmarshal(payload, &msg); err != nil {
		s.sendError(client, "invalid message payload")
		return
	}

	session, err := s.getOrCreateSession(client, msg.SessionID, msg.AgentID)
	if err != nil {
		s.sendError(client, fmt.Sprintf("session error: %v", err))
		return
	}

	userMsg := &storage.Message{
		MessageID: generateMessageID(),
		Content:   msg.Content,
		Role:      "user",
		SessionID: session.ID,
	}
	if err := s.repo.CreateMessage(userMsg); err != nil {
		s.sendError(client, fmt.Sprintf("failed to save message: %v", err))
		return
	}

	agentInstance, err := s.agentManager.GetAgent(session.AgentID)
	if err != nil {
		s.sendError(client, "agent not found")
		return
	}

	ctx := context.Background()

	if msg.Stream {
		s.handleStreamMessage(ctx, client, agentInstance, msg.Content, session)
	} else {
		response, err := agentInstance.ProcessMessage(ctx, msg.Content, session.ID)
		if err != nil {
			s.sendError(client, fmt.Sprintf("agent error: %v", err))
			return
		}

		assistantMsg := &storage.Message{
			MessageID: generateMessageID(),
			Content:   response,
			Role:      "assistant",
			SessionID: session.ID,
		}
		if err := s.repo.CreateMessage(assistantMsg); err != nil {
			s.sendError(client, fmt.Sprintf("failed to save response: %v", err))
			return
		}

		s.sendMessage(client, "message", map[string]interface{}{
			"content":    response,
			"message_id": assistantMsg.MessageID,
			"session_id": session.SessionID,
		})
	}
}

func (s *WebSocketServer) handleStreamMessage(ctx context.Context, client *Client, agentInstance *agent.Agent, content string, session *storage.Session) {
	handler := llm.NewAgentStreamHandler(func(event *llm.AgentStreamEvent) {
		msg := WSMessage{
			Type:    "stream_event",
			Payload: mustMarshalJSON(event),
		}
		client.Send <- mustMarshalJSON(msg)
	})

	req := agent.ExecuteRequest{
		SessionID:        session.ID,
		Input:            content,
		SaveInputMessage: false,
	}

	if err := agentInstance.ExecuteStream(ctx, req, handler); err != nil {
		s.sendError(client, fmt.Sprintf("stream error: %v", err))
		return
	}
}

// handleSession handles session-related actions
func (s *WebSocketServer) handleSession(client *Client, payload json.RawMessage) {
	var action struct {
		Action    string `json:"action"` // create, list, get, close
		AgentID   uint   `json:"agent_id"`
		Title     string `json:"title"`
		SessionID string `json:"session_id"`
	}

	if err := json.Unmarshal(payload, &action); err != nil {
		s.sendError(client, "invalid session payload")
		return
	}

	switch action.Action {
	case "create":
		s.createSession(client, action.AgentID, action.Title)
	case "list":
		s.listSessions(client)
	case "get":
		s.getSession(client, action.SessionID)
	case "close":
		s.closeSession(client, action.SessionID)
	default:
		s.sendError(client, fmt.Sprintf("unknown session action: %s", action.Action))
	}
}

// handleAgentAction handles agent-related actions
func (s *WebSocketServer) handleAgentAction(client *Client, payload json.RawMessage) {
	var action struct {
		Action  string `json:"action"` // list, get, create, update
		Agent   *storage.Agent
		AgentID uint `json:"agent_id"`
	}

	if err := json.Unmarshal(payload, &action); err != nil {
		s.sendError(client, "invalid agent payload")
		return
	}

	switch action.Action {
	case "list":
		s.listAgents(client)
	case "get":
		s.getAgent(client, action.AgentID)
	case "create":
		s.createAgent(client, action.Agent)
	case "update":
		s.updateAgent(client, action.Agent)
	default:
		s.sendError(client, fmt.Sprintf("unknown agent action: %s", action.Action))
	}
}

// getOrCreateSession gets or creates a session
func (s *WebSocketServer) getOrCreateSession(client *Client, sessionID string, agentID uint) (*storage.Session, error) {
	if sessionID != "" {
		return s.repo.GetSessionBySessionID(sessionID)
	}

	// Create new session
	if agentID == 0 {
		return nil, fmt.Errorf("agent_id required for new session")
	}

	newSession := &storage.Session{
		SessionID:      generateSessionID(),
		Title:          "New Conversation",
		UserID:         client.UserID,
		AgentID:        agentID,
		Platform:       client.Platform,
		PlatformChatID: client.ID,
		Status:         "active",
	}

	if err := s.repo.CreateSession(newSession); err != nil {
		return nil, err
	}

	return newSession, nil
}

// sendMessage sends a typed message to a client
func (s *WebSocketServer) sendMessage(client *Client, msgType string, data interface{}) {
	msg := WSMessage{
		Type:    msgType,
		Payload: mustMarshalJSON(data),
	}

	client.Send <- mustMarshalJSON(msg)
}

// sendError sends an error message to a client
func (s *WebSocketServer) sendError(client *Client, errMsg string) {
	s.sendMessage(client, "error", map[string]string{
		"message": errMsg,
	})
}

// BroadcastNewMessage broadcasts a new message to all clients watching a session
func (s *WebSocketServer) BroadcastNewMessage(sessionID string, message map[string]interface{}) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	slog.Info("broadcasting new message", "session_id", sessionID, "clients_count", len(s.clients))

	payloadData := mustMarshalJSON(map[string]interface{}{
		"session_id": sessionID,
		"message":    message,
	})

	msg := WSMessage{
		Type:    "new_message",
		Payload: payloadData,
	}

	data := mustMarshalJSON(msg)

	// Send to all clients
	sent := 0
	for _, client := range s.clients {
		select {
		case client.Send <- data:
			sent++
		default:
			// Client channel full, skip
			slog.Warn("client channel full, skipping message", "client_id", client.ID)
		}
	}

	slog.Info("message broadcast completed", "sent", sent)
}

// BroadcastSessionUpdate broadcasts a session update to all clients
func (s *WebSocketServer) BroadcastSessionUpdate(sessionID string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	payloadData := mustMarshalJSON(map[string]interface{}{
		"session_id": sessionID,
		"action":     "refresh",
	})

	msg := WSMessage{
		Type:    "session_update",
		Payload: payloadData,
	}

	data := mustMarshalJSON(msg)

	// Send to all clients
	for _, client := range s.clients {
		select {
		case client.Send <- data:
		default:
			// Client channel full, skip
		}
	}
}

// Session actions
func (s *WebSocketServer) createSession(client *Client, agentID uint, title string) {
	session := &storage.Session{
		SessionID:      generateSessionID(),
		Title:          title,
		UserID:         client.UserID,
		AgentID:        agentID,
		Platform:       client.Platform,
		PlatformChatID: client.ID,
		Status:         "active",
	}

	if err := s.repo.CreateSession(session); err != nil {
		s.sendError(client, fmt.Sprintf("failed to create session: %v", err))
		return
	}

	s.sendMessage(client, "session_created", session)
}

func (s *WebSocketServer) listSessions(client *Client) {
	sessions, err := s.repo.GetSessionsByUser(client.UserID)
	if err != nil {
		s.sendError(client, fmt.Sprintf("failed to list sessions: %v", err))
		return
	}

	s.sendMessage(client, "sessions", sessions)
}

func (s *WebSocketServer) getSession(client *Client, sessionID string) {
	session, err := s.repo.GetSessionBySessionID(sessionID)
	if err != nil {
		s.sendError(client, "session not found")
		return
	}

	// Get messages
	messages, _ := s.repo.GetMessagesBySession(session.ID)

	s.sendMessage(client, "session", map[string]interface{}{
		"session":  session,
		"messages": messages,
	})
}

func (s *WebSocketServer) closeSession(client *Client, sessionID string) {
	session, err := s.repo.GetSessionBySessionID(sessionID)
	if err != nil {
		s.sendError(client, "session not found")
		return
	}

	session.Status = "closed"
	if err := s.repo.UpdateSession(session); err != nil {
		s.sendError(client, fmt.Sprintf("failed to close session: %v", err))
		return
	}

	s.sendMessage(client, "session_closed", session)
}

// Agent actions
func (s *WebSocketServer) listAgents(client *Client) {
	agents, err := s.repo.ListAgents()
	if err != nil {
		s.sendError(client, fmt.Sprintf("failed to list agents: %v", err))
		return
	}

	s.sendMessage(client, "agents", agents)
}

func (s *WebSocketServer) getAgent(client *Client, agentID uint) {
	agent, err := s.repo.GetAgent(agentID)
	if err != nil {
		s.sendError(client, "agent not found")
		return
	}

	s.sendMessage(client, "agent", agent)
}

func (s *WebSocketServer) createAgent(client *Client, a *storage.Agent) {
	if a == nil {
		s.sendError(client, "agent data required")
		return
	}

	a.OwnerID = client.UserID
	a.Status = "active"

	if err := s.repo.CreateAgent(a); err != nil {
		s.sendError(client, fmt.Sprintf("failed to create agent: %v", err))
		return
	}

	s.sendMessage(client, "agent_created", a)
}

func (s *WebSocketServer) updateAgent(client *Client, a *storage.Agent) {
	if a == nil {
		s.sendError(client, "agent data required")
		return
	}

	if err := s.repo.UpdateAgent(a); err != nil {
		s.sendError(client, fmt.Sprintf("failed to update agent: %v", err))
		return
	}

	s.sendMessage(client, "agent_updated", a)
}

// handleHealth handles health check requests
func (s *WebSocketServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"clients":   s.manager.ClientCount(),
		"timestamp": time.Now().Unix(),
	})
}

// Close closes the WebSocket server
func (s *WebSocketServer) Close() {
	if s.server != nil {
		s.server.Close()
	}
}

// Helper functions
func generateClientID() string {
	return fmt.Sprintf("client_%d", time.Now().UnixNano())
}

func generateSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UnixNano())
}

func generateMessageID() string {
	return fmt.Sprintf("msg_%d", time.Now().UnixNano())
}

func mustMarshalJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"type":"error","payload":{"message":"marshal error"}}`)
	}
	return data
}
