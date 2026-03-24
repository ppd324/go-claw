package telegram

import (
	"context"
	"fmt"

	"go-claw/internal/agent"
	"go-claw/internal/config"
	"go-claw/internal/notify"
	"go-claw/internal/storage"
)

type Bot struct {
	cfg          *config.Config
	agentManager *agent.Manager
	repo         *storage.Repository
}

func NewBot(cfg *config.Config, agentManager *agent.Manager, repo *storage.Repository) (*Bot, error) {
	if !cfg.Telegram.Enabled {
		return nil, fmt.Errorf("telegram is not enabled")
	}

	return &Bot{
		cfg:          cfg,
		agentManager: agentManager,
		repo:         repo,
	}, nil
}

func (b *Bot) Start() error {
	return fmt.Errorf("Telegram bot requires telebot dependency. Run: go get github.com/telebot/telebot")
}

func (b *Bot) Stop() {}

func (b *Bot) IsRunning() bool {
	return false
}

func (b *Bot) SendMessage(chatID int64, text string) error {
	return fmt.Errorf("Telegram bot not initialized")
}

func (b *Bot) SendMessageToUser(userID int64, text string) error {
	return fmt.Errorf("Telegram bot not initialized")
}

func (b *Bot) EditMessage(text string) error {
	return fmt.Errorf("Telegram bot not initialized")
}

func (b *Bot) SendMessageWithContext(ctx context.Context, chatID, text string) error {
	return fmt.Errorf("Telegram bot not initialized")
}

func (b *Bot) GetPlatform() string {
	return "telegram"
}

var _ notify.Notifier = (*Bot)(nil)