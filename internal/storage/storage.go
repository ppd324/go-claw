package storage

import (
	"fmt"

	"go-claw/internal/config"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Init initializes the database connection
func Init(cfg *config.Config) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	// Configure GORM
	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Error),
	}

	switch cfg.Database.Type {
	case "sqlite":
		db, err = gorm.Open(sqlite.Open(cfg.Database.Path), gormConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
		}
	case "mysql":
		dsn := cfg.Database.GetDSN()
		db, err = gorm.Open(mysql.Open(dsn), gormConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to MySQL: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Database.Type)
	}

	return db, nil
}

// Migrate runs database migrations
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&User{},
		&Agent{},
		&Session{},
		&Message{},
		&AgentRun{},
		&ToolCallTrace{},
		&Skill{},
		&Workspace{},
		&ScheduledTask{},
		&TaskExecutionLog{},
	)
}

// generateID generates a unique ID
func generateID() string {
	return fmt.Sprintf("%d", nowNano())
}

var lastNano int64

func nowNano() int64 {
	// Add some randomness to avoid collisions
	if lastNano == 0 {
		lastNano = 1
	} else {
		lastNano++
	}
	return lastNano
}
