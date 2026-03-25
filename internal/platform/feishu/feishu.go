package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go-claw/internal/agent"
	"go-claw/internal/config"
	"go-claw/internal/notify"
	"go-claw/internal/storage"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

type Bot struct {
	cfg          *config.Config
	agentManager *agent.Manager
	repo         *storage.Repository
	client       *lark.Client
	wsClient     *larkws.Client
	wsCancel     context.CancelFunc
	mu           sync.RWMutex
	running      bool
	server       *http.Server
}

func NewBot(cfg *config.Config, agentManager *agent.Manager, repo *storage.Repository) (*Bot, error) {
	if !cfg.Feishu.Enabled {
		return nil, fmt.Errorf("feishu is not enabled")
	}

	client := lark.NewClient(cfg.Feishu.AppID, cfg.Feishu.AppSecret,
		lark.WithReqTimeout(30*time.Second),
	)

	return &Bot{
		cfg:          cfg,
		agentManager: agentManager,
		repo:         repo,
		client:       client,
	}, nil
}

func (b *Bot) Start() error {
	b.mu.Lock()
	b.running = true
	b.mu.Unlock()

	slog.Info("Feishu bot starting...")

	if b.cfg.Feishu.WebhookURL != "" {
		return b.startWebhookMode()
	}

	return b.startWebSocketMode()
}

func (b *Bot) startWebSocketMode() error {
	slog.Info("Starting Feishu bot in WebSocket mode (long connection)")

	eventHandler := dispatcher.NewEventDispatcher("", "").
		OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
			return b.handleP2MessageReceiveV1(ctx, event)
		})

	wsClient := larkws.NewClient(b.cfg.Feishu.AppID, b.cfg.Feishu.AppSecret,
		larkws.WithEventHandler(eventHandler),
		larkws.WithLogLevel(larkcore.LogLevelInfo),
	)

	b.wsClient = wsClient

	ctx, cancel := context.WithCancel(context.Background())
	b.wsCancel = cancel

	go func() {
		if err := wsClient.Start(ctx); err != nil {
			slog.Error("Feishu WebSocket client error", "error", err)
		}
	}()

	slog.Info("Feishu bot started in WebSocket mode")
	return nil
}

func (b *Bot) startWebhookMode() error {
	slog.Info("Starting Feishu bot in Webhook mode")

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/feishu", b.handleWebhook)

	addr := fmt.Sprintf("%s:%d", b.cfg.Server.Host, b.cfg.Server.Port+1)
	b.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		slog.Info("Feishu webhook server listening", "addr", addr)
		if err := b.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Feishu webhook server error", "error", err)
		}
	}()

	return nil
}

func (b *Bot) handleP2MessageReceiveV1(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event.Event == nil {
		return nil
	}

	msg := event.Event.Message
	sender := event.Event.Sender
	fmt.Println("lark:", msg)

	if sender == nil || sender.SenderType == nil || *sender.SenderType == "app" {
		return nil
	}

	var text string
	if msg != nil && msg.MessageType != nil && *msg.MessageType == "text" {
		if msg.Content != nil {
			var content struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(*msg.Content), &content); err == nil {
				text = content.Text
			}
		}
	} else if msg != nil && msg.MessageType != nil && *msg.MessageType == "post" {
		if msg.Content != nil {
			text = b.extractPostContent(*msg.Content)
		}
	}

	if text == "" || msg == nil || msg.ChatId == nil {
		return nil
	}

	senderOpenID := ""
	if sender.SenderId != nil && sender.SenderId.OpenId != nil {
		senderOpenID = *sender.SenderId.OpenId
	}

	messageID := ""
	if msg.MessageId != nil {
		messageID = *msg.MessageId
	}

	chatID := *msg.ChatId
	chatType := ""
	if msg.ChatType != nil {
		chatType = *msg.ChatType
	}

	go b.processMessage(chatID, senderOpenID, messageID, text, chatType)

	return nil
}

func (b *Bot) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("Failed to read request body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var baseEvent struct {
		Schema string `json:"schema"`
		Header struct {
			EventID    string `json:"event_id"`
			EventType  string `json:"event_type"`
			CreateTime string `json:"create_time"`
			Token      string `json:"token"`
			AppID      string `json:"app_id"`
			TenantKey  string `json:"tenant_key"`
		} `json:"header"`
	}

	if err := json.Unmarshal(body, &baseEvent); err != nil {
		slog.Error("Failed to parse base event", "error", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	if baseEvent.Header.EventType == "url_verification" || baseEvent.Schema == "" {
		b.handleURLVerification(w, body)
		return
	}

	if baseEvent.Header.EventType == "im.message.receive_v1" {
		b.handleWebhookMessageEvent(r.Context(), body)
	}

	w.WriteHeader(http.StatusOK)
}

func (b *Bot) handleURLVerification(w http.ResponseWriter, body []byte) {
	var req struct {
		Challenge string `json:"challenge"`
		Token     string `json:"token"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		slog.Error("Failed to parse URL verification", "error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"challenge": req.Challenge})
}

func (b *Bot) handleWebhookMessageEvent(ctx context.Context, body []byte) {
	var event struct {
		Sender struct {
			SenderID struct {
				UnionID string `json:"union_id"`
				UserID  string `json:"user_id"`
				OpenID  string `json:"open_id"`
			} `json:"sender_id"`
			SenderType string `json:"sender_type"`
			TenantKey  string `json:"tenant_key"`
		} `json:"sender"`
		Message struct {
			MessageID   string `json:"message_id"`
			RootID      string `json:"root_id"`
			ParentID    string `json:"parent_id"`
			CreateTime  string `json:"create_time"`
			ChatID      string `json:"chat_id"`
			ChatType    string `json:"chat_type"`
			MessageType string `json:"message_type"`
			Content     string `json:"content"`
			Mentions    []struct {
				Key string `json:"key"`
				ID  struct {
					UnionID string `json:"union_id"`
					UserID  string `json:"user_id"`
					OpenID  string `json:"open_id"`
				} `json:"id"`
				Name      string `json:"name"`
				TenantKey string `json:"tenant_key"`
			} `json:"mentions"`
		} `json:"message"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		slog.Error("Failed to parse message event", "error", err)
		return
	}

	msg := event.Message
	sender := event.Sender

	if sender.SenderType == "app" {
		return
	}

	var text string
	if msg.MessageType == "text" {
		var content struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(msg.Content), &content); err == nil {
			text = content.Text
		}
	} else if msg.MessageType == "post" {
		text = b.extractPostContent(msg.Content)
	}

	if text == "" {
		return
	}

	go b.processMessage(msg.ChatID, sender.SenderID.OpenID, msg.MessageID, text, msg.ChatType)
}

func (b *Bot) extractPostContent(content string) string {
	var post struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(content), &post); err == nil {
		return post.Title + "\n" + post.Content
	}
	return ""
}

func (b *Bot) processMessage(chatID, senderOpenID, messageID, text, chatType string) {
	ctx := context.Background()
	ctx = notify.WithPlatform(ctx, "feishu", chatID)

	user, err := b.repo.GetOrCreateUser("feishu", senderOpenID, "", "")
	if err != nil {
		slog.Error("Failed to get/create user", "error", err)
		return
	}

	session, err := b.getOrCreateSession(user, chatID)
	if err != nil {
		slog.Error("Failed to get/create session", "error", err)
		return
	}

	userMsg := &storage.Message{
		MessageID:         messageID,
		Content:           text,
		Role:              "user",
		SessionID:         session.ID,
		PlatformMessageID: messageID,
	}
	if err := b.repo.CreateMessage(userMsg); err != nil {
		slog.Error("Failed to save message", "error", err)
	}

	reactionID := b.AddTypingReaction(messageID)

	agentInstance, err := b.agentManager.GetAgent(session.AgentID)
	if err != nil {
		slog.Error("Failed to get agent", "error", err)
		b.RemoveTypingReaction(messageID, reactionID)
		return
	}

	response, err := agentInstance.ProcessMessage(ctx, text, session.ID)
	if err != nil {
		slog.Error("Agent error", "error", err)
		response = fmt.Sprintf("Error: %v", err)
	}

	b.RemoveTypingReaction(messageID, reactionID)

	if err := b.SendTextMessage(chatID, response); err != nil {
		slog.Error("Failed to send response", "error", err)
	}

	assistantMsg := &storage.Message{
		MessageID: fmt.Sprintf("resp_%d", time.Now().UnixNano()),
		Content:   response,
		Role:      "assistant",
		SessionID: session.ID,
	}
	if err := b.repo.CreateMessage(assistantMsg); err != nil {
		slog.Error("Failed to save response", "error", err)
	}
}

func (b *Bot) getOrCreateSession(user *storage.User, chatID string) (*storage.Session, error) {
	sessions, err := b.repo.GetSessionsByUser(user.ID)
	if err == nil && len(sessions) > 0 {
		for i := range sessions {
			if sessions[i].Status == "active" && sessions[i].PlatformChatID == chatID {
				return &sessions[i], nil
			}
		}
	}

	agents, err := b.agentManager.ListAgents()
	if err != nil || len(agents) == 0 {
		return nil, fmt.Errorf("no agents available")
	}

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

func (b *Bot) SendTextMessage(chatID, text string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(larkim.MsgTypePost).
			Content(fmt.Sprintf(`{"zh_cn":{"title":"","content":[[{"tag":"text","text":%q}]]}}`, text)).
			Build()).
		Build()

	resp, err := b.client.Im.Message.Create(context.Background(), req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	if !resp.Success() {
		return fmt.Errorf("failed to send message: %s", resp.Msg)
	}

	return nil
}

func (b *Bot) SendCardMessage(chatID, title, content string) error {
	card := map[string]interface{}{
		"config": map[string]interface{}{
			"wide_screen_mode": true,
		},
		"elements": []map[string]interface{}{
			{
				"tag": "div",
				"text": map[string]interface{}{
					"tag":     "plain_text",
					"content": content,
				},
			},
		},
	}

	if title != "" {
		card["header"] = map[string]interface{}{
			"title": map[string]interface{}{
				"tag":     "plain_text",
				"content": title,
			},
		}
	}

	cardJSON, _ := json.Marshal(card)

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(larkim.MsgTypeInteractive).
			Content(string(cardJSON)).
			Build()).
		Build()

	resp, err := b.client.Im.Message.Create(context.Background(), req)
	if err != nil {
		return fmt.Errorf("failed to send card message: %w", err)
	}

	if !resp.Success() {
		return fmt.Errorf("failed to send card message: %s", resp.Msg)
	}

	return nil
}

func (b *Bot) Stop() {
	b.mu.Lock()
	b.running = false
	b.mu.Unlock()

	if b.wsCancel != nil {
		b.wsCancel()
	}

	if b.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		b.server.Shutdown(ctx)
	}

	slog.Info("Feishu bot stopped")
}

func (b *Bot) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running
}

func (b *Bot) AddTypingReaction(messageID string) string {
	ctx := context.Background()
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(&larkim.Emoji{
				EmojiType: larkcore.StringPtr("Typing"),
			}).
			Build()).
		Build()

	resp, err := b.client.Im.MessageReaction.Create(ctx, req)
	if err != nil {
		slog.Warn("Failed to add typing reaction", "error", err)
		return ""
	}

	if !resp.Success() {
		slog.Warn("Failed to add typing reaction", "msg", resp.Msg)
		return ""
	}

	if resp.Data != nil && resp.Data.ReactionId != nil {
		return *resp.Data.ReactionId
	}
	return ""
}

func (b *Bot) RemoveTypingReaction(messageID, reactionID string) {
	if reactionID == "" {
		return
	}

	ctx := context.Background()
	req := larkim.NewDeleteMessageReactionReqBuilder().
		MessageId(messageID).
		ReactionId(reactionID).
		Build()

	resp, err := b.client.Im.MessageReaction.Delete(ctx, req)
	if err != nil {
		slog.Warn("Failed to remove typing reaction", "error", err)
		return
	}

	if !resp.Success() {
		slog.Warn("Failed to remove typing reaction", "msg", resp.Msg)
	}
}

func (b *Bot) SendMessage(chatID, text string) error {
	return b.SendTextMessage(chatID, text)
}

func (b *Bot) SendMessageWithContext(ctx context.Context, chatID, text string) error {
	return b.SendTextMessage(chatID, text)
}

func (b *Bot) GetPlatform() string {
	return "feishu"
}

var _ notify.Notifier = (*Bot)(nil)
