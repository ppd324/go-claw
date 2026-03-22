package log

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

func InitLogger() zerolog.Logger {
	// Get log level from config or environment
	level := getLogLevel()
	format := getLogFormat()

	var output io.Writer = os.Stdout
	if format == "json" {
		output = &zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	logger := zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Caller().
		Str("service", "go-claw").
		Logger()

	return logger
}

func getLogLevel() zerolog.Level {
	levelStr := os.Getenv("GO_CLAW_LOG_LEVEL")
	if levelStr == "" {
		levelStr = "info"
	}

	level, err := zerolog.ParseLevel(levelStr)
	if err != nil {
		level = zerolog.InfoLevel
	}
	return level
}

func getLogFormat() string {
	format := os.Getenv("GO_CLAW_LOG_FORMAT")
	if format == "" {
		format = "console"
	}
	return format
}