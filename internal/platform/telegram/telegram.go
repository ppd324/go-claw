package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"go-claw/internal/agent"
	"go-claw/internal/config"
	"go-claw/internal/notify"
	"go-claw/internal/storage"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	cfg          *config.Config
	agentManager *agent.Manager
	repo         *storage.Repository
	api          *tgbotapi.BotAPI
	mu           sync.RWMutex
	running      bool
	stopChan     chan struct{}
}

func NewBot(cfg *config.Config, agentManager *agent.Manager, repo *storage.Repository) (*Bot, error) {
	if !cfg.Telegram.Enabled {
		return nil, fmt.Errorf("telegram is not enabled")
	}

	if cfg.Telegram.BotToken == "" {
		return nil, fmt.Errorf("telegram bot token is required")
	}

	var botAPI *tgbotapi.BotAPI
	var err error

	if cfg.Telegram.APIServer != "" {
		botAPI, err = tgbotapi.NewBotAPIWithAPIEndpoint(cfg.Telegram.BotToken, cfg.Telegram.APIServer)
	} else {
		botAPI, err = tgbotapi.NewBotAPI(cfg.Telegram.BotToken)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	slog.Info("Telegram bot authorized", "username", botAPI.Self.UserName)

	return &Bot{
		cfg:          cfg,
		agentManager: agentManager,
		repo:         repo,
		api:          botAPI,
		stopChan:     make(chan struct{}),
	}, nil
}

func (b *Bot) Start() error {
	b.mu.Lock()
	b.running = true
	b.mu.Unlock()

	slog.Info("Telegram bot starting...")

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	go func() {
		for {
			select {
			case <-b.stopChan:
				slog.Info("Telegram bot stopped")
				return
			case update := <-updates:
				if update.Message == nil {
					continue
				}
				go b.handleMessage(update)
			}
		}
	}()

	slog.Info("Telegram bot started")
	return nil
}

func (b *Bot) Stop() {
	b.mu.Lock()
	b.running = false
	b.mu.Unlock()

	close(b.stopChan)
	slog.Info("Telegram bot stopped")
}

func (b *Bot) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running
}

func (b *Bot) handleMessage(update tgbotapi.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}

	userID := msg.From.ID
	chatID := msg.Chat.ID
	text := msg.Text

	if text == "" {
		return
	}

	if len(b.cfg.Telegram.AllowList) > 0 {
		allowed := false
		for _, id := range b.cfg.Telegram.AllowList {
			if id == userID {
				allowed = true
				break
			}
		}
		if !allowed {
			slog.Warn("Unauthorized user attempted to use bot", "user_id", userID)
			return
		}
	}

	ctx := context.Background()
	ctx = notify.WithPlatform(ctx, "telegram", strconv.FormatInt(chatID, 10))

	user, err := b.repo.GetOrCreateUser("telegram", strconv.FormatInt(userID, 10), msg.From.UserName, msg.From.FirstName)
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
		MessageID:         strconv.Itoa(msg.MessageID),
		Content:           text,
		Role:              "user",
		SessionID:         session.ID,
		PlatformMessageID: strconv.Itoa(msg.MessageID),
	}
	if err := b.repo.CreateMessage(userMsg); err != nil {
		slog.Error("Failed to save message", "error", err)
	}

	b.SendChatAction(chatID, "typing")

	agentInstance, err := b.agentManager.GetAgent(session.AgentID)
	if err != nil {
		slog.Error("Failed to get agent", "error", err)
		b.SendMessage(chatID, "Error: Agent not found")
		return
	}

	response, err := agentInstance.ProcessMessage(ctx, text, session.ID)
	if err != nil {
		slog.Error("Agent error", "error", err)
		response = fmt.Sprintf("Error: %v", err)
	}

	if err := b.SendMessage(chatID, response); err != nil {
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

func (b *Bot) getOrCreateSession(user *storage.User, chatID int64) (*storage.Session, error) {
	sessions, err := b.repo.GetSessionsByUser(user.ID)
	if err == nil && len(sessions) > 0 {
		for i := range sessions {
			if sessions[i].Status == "active" && sessions[i].PlatformChatID == strconv.FormatInt(chatID, 10) {
				return &sessions[i], nil
			}
		}
	}

	agents, err := b.agentManager.ListAgents()
	if err != nil || len(agents) == 0 {
		return nil, fmt.Errorf("no agents available")
	}

	session := &storage.Session{
		SessionID:      fmt.Sprintf("telegram_%d", time.Now().UnixNano()),
		Title:          "Telegram Chat",
		UserID:         user.ID,
		AgentID:        agents[0].ID,
		Platform:       "telegram",
		PlatformChatID: strconv.FormatInt(chatID, 10),
		Status:         "active",
	}

	if err := b.repo.CreateSession(session); err != nil {
		return nil, err
	}

	return session, nil
}

func (b *Bot) SendMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"

	_, err := b.api.Send(msg)
	if err != nil {
		msg.ParseMode = ""
		_, err = b.api.Send(msg)
		if err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
	}

	return nil
}

func (b *Bot) SendMessageWithContext(ctx context.Context, chatIDStr, text string) error {
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}
	return b.SendMessage(chatID, text)
}

func (b *Bot) SendChatAction(chatID int64, action string) error {
	typing := tgbotapi.NewChatAction(chatID, action)
	_, err := b.api.Request(typing)
	return err
}

func (b *Bot) SendMessageToUser(userID int64, text string) error {
	return b.SendMessage(userID, text)
}

func (b *Bot) EditMessage(chatID int64, messageID int, text string) error {
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = "Markdown"
	_, err := b.api.Send(edit)
	return err
}

func (b *Bot) GetPlatform() string {
	return "telegram"
}

var _ notify.Notifier = (*Bot)(nil)
