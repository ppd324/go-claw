package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"go-claw/internal/agent"
	"go-claw/internal/config"
	"go-claw/internal/dashboard"
	"go-claw/internal/log"
	"go-claw/internal/platform/feishu"
	"go-claw/internal/platform/telegram"
	"go-claw/internal/server"
	"go-claw/internal/storage"
)

func main() {
	// Initialize logger
	logger := log.InitLogger()
	logger.Info().Msg("Starting Go-Claw")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to load configuration")
	}

	// Initialize database
	db, err := storage.Init(cfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize database")
	}

	// Run migrations
	if err := storage.Migrate(db); err != nil {
		logger.Fatal().Err(err).Msg("Failed to run migrations")
	}

	// Initialize storage repository
	repo := storage.NewRepository(db)

	// Ensure work directory exists
	if err := os.MkdirAll(cfg.WorkDir, 0755); err != nil {
		logger.Fatal().Err(err).Str("work_dir", cfg.WorkDir).Msg("Failed to create work directory")
	}

	// Ensure database directory exists
	if cfg.Database.Type == "sqlite" && cfg.Database.Path != "" {
		dbDir := filepath.Dir(cfg.Database.Path)
		if err := os.MkdirAll(dbDir, 0755); err != nil {
			logger.Fatal().Err(err).Str("db_dir", dbDir).Msg("Failed to create database directory")
		}
	}

	logger.Info().Str("work_dir", cfg.WorkDir).Str("db_path", cfg.Database.Path).Msg("Work directory and database initialized")

	// Initialize agent manager
	agentManager := agent.NewManager(cfg, repo, cfg.WorkDir)

	// Initialize WebSocket server
	wsServer := server.NewWebSocketServer(cfg, agentManager, repo)

	// Initialize dashboard
	dash := dashboard.NewServer(cfg, agentManager, repo, wsServer)

	// Create combined HTTP server
	httpServer := server.NewHTTPServer(cfg, wsServer, dash)

	// Initialize Telegram bot (if enabled)
	var telegramBot *telegram.Bot
	if cfg.Telegram.Enabled {
		telegramBot, err = telegram.NewBot(cfg, agentManager, repo)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to initialize Telegram bot")
		} else {
			go telegramBot.Start()
		}
	}

	// Initialize Feishu bot (if enabled)
	var feishuBot *feishu.Bot
	if cfg.Feishu.Enabled {
		feishuBot, err = feishu.NewBot(cfg, agentManager, repo)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to initialize Feishu bot")
		} else {
			go feishuBot.Start()
		}
	}

	// Start scheduler (initialized within dashboard)
	dash.StartScheduler()

	// Start servers
	go func() {
		if err := httpServer.Start(); err != nil {
			logger.Error().Err(err).Msg("HTTP server error")
		}
	}()

	logger.Info().
		Int("port", cfg.Server.Port).
		Str("dashboard", fmt.Sprintf("http://%s:%d/dashboard", cfg.Server.Host, cfg.Server.Port)).
		Bool("telegram", cfg.Telegram.Enabled).
		Bool("feishu", cfg.Feishu.Enabled).
		Bool("scheduler", true).
		Msg("Server started")

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info().Msg("Shutting down...")

	// Cleanup
	if telegramBot != nil {
		telegramBot.Stop()
	}
	if feishuBot != nil {
		feishuBot.Stop()
	}
	dash.StopScheduler()
	wsServer.Close()

	// Close database
	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.Close()
	}

	logger.Info().Msg("Go-Claw stopped")

	// Cancel context
	agentManager.Shutdown(context.Background())
}
