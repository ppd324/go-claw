package feishu

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"go-claw/internal/agent"
	"go-claw/internal/config"
	"go-claw/internal/storage"
)

// Message represents a parsed message from Feishu
type Message struct {
	MessageID   string
	ChatID      string
	ChatType    string
	SenderID    string
	SenderType  string
	MessageType string
	Content     string
}

// Bot represents a Feishu bot
type Bot struct {
	cfg          *config.Config
	agentManager *agent.Manager
	repo         *storage.Repository
	client       *http.Client
	mu           sync.RWMutex
	running      bool
	verifyToken  string
	signingKey   string
}

// Event types
const (
	EventTypeMessage       = "im.message"
	EventTypeMessageCreate = "message_created"
)

// NewBot creates a new Feishu bot
func NewBot(cfg *config.Config, agentManager *agent.Manager, repo *storage.Repository) (*Bot, error) {
	if !cfg.Feishu.Enabled {
		return nil, fmt.Errorf("feishu is not enabled")
	}

	return &Bot{
		cfg:          cfg,
		agentManager: agentManager,
		repo:         repo,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		verifyToken: cfg.Feishu.VerifyToken,
		signingKey:  cfg.Feishu.SigningKey,
	}, nil
}

// Start starts the Feishu bot webhook server
func (b *Bot) Start() error {
	b.mu.Lock()
	b.running = true
	b.mu.Unlock()

	log.Println("Feishu bot starting webhook server...")

	addr := fmt.Sprintf("%s:%d", b.cfg.Server.Host, b.cfg.Server.Port+1)
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/feishu", b.handleWebhook)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Feishu webhook server error: %v", err)
		}
	}()

	log.Printf("Feishu webhook server listening on %s", addr)
	return nil
}

// Stop stops the Feishu bot
func (b *Bot) Stop() {
	b.mu.Lock()
	b.running = false
	b.mu.Unlock()
	log.Println("Feishu bot stopped")
}

// IsRunning returns whether the bot is running
func (b *Bot) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running
}

// handleWebhook handles incoming Feishu webhooks
func (b *Bot) handleWebhook(w http.ResponseWriter, r *http.Request) {
	// Verify signature
	if !b.verifySignature(r) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Failed to read request body: %v", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Parse challenge for verification
	var challengeReq struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(body, &challengeReq); err == nil {
		if challengeReq.Type == "url_verification" {
			b.handleVerification(w, body)
			return
		}
	}

	// Parse event
	var event struct {
		Type    string `json:"type"`
		Event   string `json:"event"`
		MsgType string `json:"msg_type"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		log.Printf("Failed to parse event: %v", err)
		return
	}

	// Handle message events
	if event.Type == "event_callback" && event.Event == EventTypeMessageCreate {
		b.handleMessage(w, body)
		return
	}

	// Return success
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"code":0}`))
}

// handleVerification handles URL verification
func (b *Bot) handleVerification(w http.ResponseWriter, body []byte) {
	var req struct {
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return
	}

	resp := map[string]string{
		"challenge": req.Challenge,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleMessage handles incoming messages
func (b *Bot) handleMessage(w http.ResponseWriter, body []byte) {
	var event struct {
		Event struct {
			Message struct {
				MessageID string `json:"message_id"`
				ChatID    string `json:"chat_id"`
				ChatType  string `json:"chat_type"`
				Sender    struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"sender"`
				MessageType string `json:"message_type"`
				Content     string `json:"content"`
			} `json:"message"`
		} `json:"event"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		log.Printf("Failed to parse message event: %v", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	msg := event.Event.Message

	// Skip bot messages
	if msg.Sender.Type == "app" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse message content
	var content struct {
		Text string `json:"text"`
	}
	text := ""
	if msg.MessageType == "text" {
		if err := json.Unmarshal([]byte(msg.Content), &content); err != nil {
			log.Printf("Failed to parse content: %v", err)
			w.WriteHeader(http.StatusOK)
			return
		}
		text = content.Text
	}

	// Create message struct
	feishuMsg := Message{
		MessageID:   msg.MessageID,
		ChatID:      msg.ChatID,
		ChatType:    msg.ChatType,
		SenderID:    msg.Sender.ID,
		SenderType:  msg.Sender.Type,
		MessageType: msg.MessageType,
		Content:     msg.Content,
	}

	// Process message
	go b.processMessage(feishuMsg, text)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"code":0}`))
}

// processMessage processes the message through the agent
func (b *Bot) processMessage(msg Message, text string) {
	// Get or create user
	platform := "feishu"
	platformUserID := msg.SenderID

	user, err := b.repo.GetOrCreateUser(platform, platformUserID, "", "")
	if err != nil {
		log.Printf("Failed to get/create user: %v", err)
		return
	}

	// Get or create session
	session, err := b.getOrCreateSession(user, msg.ChatID)
	if err != nil {
		log.Printf("Failed to get/create session: %v", err)
		return
	}

	// Save user message
	userMsg := &storage.Message{
		MessageID:         msg.MessageID,
		Content:           text,
		Role:              "user",
		SessionID:         session.ID,
		PlatformMessageID: msg.MessageID,
	}
	if err := b.repo.CreateMessage(userMsg); err != nil {
		log.Printf("Failed to save message: %v", err)
	}

	// Get agent
	agentInstance, err := b.agentManager.GetAgent(session.AgentID)
	if err != nil {
		log.Printf("Failed to get agent: %v", err)
		return
	}

	// Process through agent
	ctx := context.Background()
	response, err := agentInstance.ProcessMessage(ctx, text, session.ID)
	if err != nil {
		log.Printf("Agent error: %v", err)
		response = fmt.Sprintf("Error: %v", err)
	}

	// Send response back to Feishu
	if err := b.sendMessage(msg.ChatID, response); err != nil {
		log.Printf("Failed to send response: %v", err)
	}

	// Save assistant message
	assistantMsg := &storage.Message{
		MessageID: fmt.Sprintf("resp_%d", time.Now().Unix()),
		Content:   response,
		Role:      "assistant",
		SessionID: session.ID,
	}
	if err := b.repo.CreateMessage(assistantMsg); err != nil {
		log.Printf("Failed to save response: %v", err)
	}
}

// getOrCreateSession gets or creates a session for the user
func (b *Bot) getOrCreateSession(user *storage.User, chatID string) (*storage.Session, error) {
	// Try to get active session
	sessions, err := b.repo.GetSessionsByUser(user.ID)
	if err == nil && len(sessions) > 0 {
		for _, s := range sessions {
			if s.Status == "active" && s.PlatformChatID == chatID {
				return &s, nil
			}
		}
	}

	// Get default agent
	agents, err := b.agentManager.ListAgents()
	if err != nil || len(agents) == 0 {
		return nil, fmt.Errorf("no agents available")
	}

	// Create new session
	session := &storage.Session{
		SessionID:      fmt.Sprintf("feishu_%d", time.Now().UnixNano()),
		Title:          "Feishu Chat",
		UserID:         user.ID,
		AgentID:        agents[0].ID,
		Platform:       "feishu",
		PlatformChatID: chatID,
		Status:         "active",
	}

	if err := b.repo.CreateSession(session); err != nil {
		return nil, err
	}

	return session, nil
}

// verifySignature verifies the request signature
func (b *Bot) verifySignature(r *http.Request) bool {
	if b.signingKey == "" {
		return true // Skip verification if no key
	}

	signature := r.Header.Get("X-Lark-Signature")
	timestamp := r.Header.Get("X-Lark-Timestamp")

	if signature == "" || timestamp == "" {
		return false
	}

	// Build signature string
	stringToSign := timestamp + "\n" + b.signingKey

	// HMAC-SHA256
	h := hmac.New(sha256.New, []byte(b.signingKey))
	h.Write([]byte(stringToSign))
	sign := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return sign == signature
}

// sendMessage sends a message to Feishu
func (b *Bot) sendMessage(chatID, text string) error {
	apiURL := fmt.Sprintf("https://open.feishu.cn/open-apis/im/v1/messages?receive_id_type=chat_id")

	payload := map[string]interface{}{
		"receive_id": chatID,
		"msg_type":   "text",
		"content":    map[string]string{"text": text},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(string(body)))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.cfg.Feishu.AppAccessToken)

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to send message: %s", body)
	}

	return nil
}

// SendMessage sends a message to a chat
func (b *Bot) SendMessage(chatID, text string) error {
	return b.sendMessage(chatID, text)
}

// RegisterWebhook registers the webhook with Feishu
func (b *Bot) RegisterWebhook(webhookURL string) error {
	apiURL := "https://open.feishu.cn/open-apis/bot/v3/hook"

	payload := map[string]interface{}{
		"url": webhookURL,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", apiURL, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
