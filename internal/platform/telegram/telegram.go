package telegram

import (
	"fmt"

	"go-claw/internal/agent"
	"go-claw/internal/config"
	"go-claw/internal/storage"
)

// Bot represents a Telegram bot (stub - requires telebot dependency)
type Bot struct {
	cfg          *config.Config
	agentManager *agent.Manager
	repo         *storage.Repository
}

// NewBot creates a new Telegram bot
func NewBot(cfg *config.Config, agentManager *agent.Manager, repo *storage.Repository) (*Bot, error) {
	if !cfg.Telegram.Enabled {
		return nil, fmt.Errorf("telegram is not enabled")
	}

	// This is a stub - to enable, add telebot dependency:
	// go get github.com/telebot/telebot

	return &Bot{
		cfg:          cfg,
		agentManager: agentManager,
		repo:         repo,
	}, nil
}

// Start starts the Telegram bot
func (b *Bot) Start() error {
	return fmt.Errorf("Telegram bot requires telebot dependency. Run: go get github.com/telebot/telebot")
}

// Stop stops the Telegram bot
func (b *Bot) Stop() {}

// IsRunning returns whether the bot is running
func (b *Bot) IsRunning() bool {
	return false
}

// SendMessage sends a message to a chat
func (b *Bot) SendMessage(chatID int64, text string) error {
	return fmt.Errorf("Telegram bot not initialized")
}

// SendMessageToUser sends a message to a specific user
func (b *Bot) SendMessageToUser(userID int64, text string) error {
	return fmt.Errorf("Telegram bot not initialized")
}

// EditMessage edits an existing message
func (b *Bot) EditMessage(text string) error {
	return fmt.Errorf("Telegram bot not initialized")
}