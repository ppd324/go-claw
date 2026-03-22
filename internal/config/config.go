package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	Server      ServerConfig      `mapstructure:"server"`
	Database    DatabaseConfig    `mapstructure:"database"`
	LLMProvider LLMProviderConfig `mapstructure:"llm_provider"`
	Skills      SkillsConfig      `mapstructure:"skills"`
	Telegram    TelegramConfig    `mapstructure:"telegram"`
	Discord     DiscordConfig     `mapstructure:"discord"`
	Feishu      FeishuConfig      `mapstructure:"feishu"`
	Voice       VoiceConfig       `mapstructure:"voice"`
	Log         LogConfig         `mapstructure:"log"`
	WorkDir     string            `mapstructure:"work_dir"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	// WebSocket endpoint path
	WSPath string `mapstructure:"ws_path"`
}

type DatabaseConfig struct {
	Type     string `mapstructure:"type"` // sqlite, mysql
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
	Path     string `mapstructure:"path"` // For SQLite
}

type ClaudeConfig struct {
	APIKey    string        `mapstructure:"api_key"`
	Model     string        `mapstructure:"model"`
	MaxTokens int           `mapstructure:"max_tokens"`
	Timeout   time.Duration `mapstructure:"timeout"`
}

type LLMProviderConfig struct {
	Provider    string        `mapstructure:"provider"`
	Model       string        `mapstructure:"model"`
	BaseUrl     string        `mapstructure:"base_url"`
	MaxTokens   int           `mapstructure:"max_tokens"`
	Timeout     time.Duration `mapstructure:"timeout"`
	ApiKey      string        `mapstructure:"api_key"`
	Temperature float64       `mapstructure:"temperature"`
}

type SkillsConfig struct {
	Directory         string `mapstructure:"directory"`
	MaxInjectedSkills int    `mapstructure:"max_injected_skills"`
	MaxInjectionChars int    `mapstructure:"max_injection_chars"`
}

type TelegramConfig struct {
	Enabled   bool    `mapstructure:"enabled"`
	BotToken  string  `mapstructure:"bot_token"`
	APIServer string  `mapstructure:"api_server"` // For custom API server
	AllowList []int64 `mapstructure:"allow_list"` // User IDs allowed to use the bot
	AdminList []int64 `mapstructure:"admin_list"` // Admin user IDs
}

type DiscordConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	BotToken  string `mapstructure:"bot_token"`
	GuildID   string `mapstructure:"guild_id"`
	ChannelID string `mapstructure:"channel_id"`
}

type FeishuConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	AppID          string `mapstructure:"app_id"`
	AppSecret      string `mapstructure:"app_secret"`
	AppAccessToken string `mapstructure:"app_access_token"`
	VerifyToken    string `mapstructure:"verify_token"`
	SigningKey     string `mapstructure:"signing_key"`
	WebhookURL     string `mapstructure:"webhook_url"`
}

type VoiceConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	STTProvider   string `mapstructure:"stt_provider"` // openai, google
	TTSProvider   string `mapstructure:"tts_provider"` // openai, elevenlabs
	OpenAIKey     string `mapstructure:"openai_key"`
	ElevenLabsKey string `mapstructure:"elevenlabs_key"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"` // json, console
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	// viper.AddConfigPath(".")
	// viper.AddConfigPath("./configs")
	viper.AddConfigPath("$HOME/.go-claw/configs")

	// Set defaults
	viper.SetDefault("server.host", "127.0.0.1")
	viper.SetDefault("server.port", 18789)
	viper.SetDefault("server.ws_path", "/ws")

	viper.SetDefault("database.type", "sqlite")
	// Set default database path to ~/.go-claw/data/go-claw.db
	if homeDir, err := os.UserHomeDir(); err == nil {
		dbPath := filepath.ToSlash(filepath.Join(homeDir, ".go-claw", "data", "go-claw.db"))
		viper.SetDefault("database.path", dbPath)
	} else {
		viper.SetDefault("database.path", "./data/go-claw.db")
	}

	viper.SetDefault("llm_provider.provider", "ark")
	viper.SetDefault("llm_provider.model", "minimax-m2.5")
	viper.SetDefault("llm_provider.max_tokens", 200000)
	viper.SetDefault("llm_provider.timeout", 120*time.Second)
	viper.SetDefault("llm_provider.base_url", "")
	viper.SetDefault("llm_provider.api_key", "")
	viper.SetDefault("llm_provider.temperature", 0.2)

	viper.SetDefault("skills.directory", "./skills")
	viper.SetDefault("skills.max_injected_skills", 3)
	viper.SetDefault("skills.max_injection_chars", 4000)

	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "console")

	// Set default work directory to ~/.go-claw (same as OpenClaw)
	if homeDir, err := os.UserHomeDir(); err == nil {
		viper.SetDefault("work_dir", filepath.ToSlash(filepath.Join(homeDir, ".go-claw")))
	} else {
		viper.SetDefault("work_dir", ".")
	}

	// Load .env file if exists
	_ = godotenv.Load()

	// Environment variable overrides
	viper.SetEnvPrefix("GO_CLAW")
	viper.AutomaticEnv()

	// Try to read config file
	configNotFound := false
	if err := viper.ReadInConfig(); err != nil {
		// Config file not found, use defaults
		configNotFound = true
		fmt.Printf("Config file not found, using defaults: %v\n", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Normalize database path for SQLite (use forward slashes)
	if cfg.Database.Type == "sqlite" {
		cfg.Database.Path = filepath.ToSlash(cfg.Database.Path)
	}

	// If config file doesn't exist, create a default one
	if configNotFound {
		if err := createDefaultConfig(); err != nil {
			fmt.Printf("Warning: failed to create default config: %v\n", err)
		} else {
			fmt.Println("Default config file created at ~/.go-claw/configs/config.yaml")
		}
	}

	return &cfg, nil
}

// createDefaultConfig creates a default configuration file
func createDefaultConfig() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configDir := filepath.Join(homeDir, ".go-claw", "configs")
	configPath := filepath.Join(configDir, "config.yaml")

	// Create directory if not exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	// Check if config file already exists
	if _, err := os.Stat(configPath); err == nil {
		return nil // File already exists
	}

	// Set all default values
	viper.Set("server.host", "127.0.0.1")
	viper.Set("server.port", 18789)
	viper.Set("server.ws_path", "/ws")
	viper.Set("database.type", "sqlite")
	viper.Set("database.path", filepath.ToSlash(filepath.Join(homeDir, ".go-claw", "data", "go-claw.db")))
	viper.Set("llm_provider.provider", "ark")
	viper.Set("llm_provider.model", "minimax-m2.5")
	viper.Set("llm_provider.base_url", "")
	viper.Set("llm_provider.api_key", "")
	viper.Set("llm_provider.max_tokens", 200000)
	viper.Set("llm_provider.timeout", 120)
	viper.Set("llm_provider.temperature", 0.2)
	viper.Set("skills.directory", "./skills")
	viper.Set("skills.max_injected_skills", 3)
	viper.Set("skills.max_injection_chars", 4000)
	viper.Set("work_dir", filepath.ToSlash(filepath.Join(homeDir, ".go-claw")))
	viper.Set("telegram.enabled", false)
	viper.Set("telegram.bot_token", "")
	viper.Set("telegram.api_server", "")
	viper.Set("telegram.allow_list", []string{})
	viper.Set("telegram.admin_list", []string{})
	viper.Set("discord.enabled", false)
	viper.Set("discord.bot_token", "")
	viper.Set("discord.guild_id", "")
	viper.Set("discord.channel_id", "")
	viper.Set("feishu.enabled", false)
	viper.Set("feishu.app_id", "")
	viper.Set("feishu.app_secret", "")
	viper.Set("feishu.app_access_token", "")
	viper.Set("feishu.verify_token", "")
	viper.Set("feishu.signing_key", "")
	viper.Set("feishu.webhook_url", "")
	viper.Set("voice.enabled", false)
	viper.Set("voice.stt_provider", "openai")
	viper.Set("voice.tts_provider", "openai")
	viper.Set("voice.openai_key", "")
	viper.Set("voice.elevenlabs_key", "")
	viper.Set("log.level", "info")
	viper.Set("log.format", "console")

	// Write config to file
	viper.SetConfigFile(configPath)
	if err := viper.WriteConfig(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// GetDSN returns the database DSN based on the config
func (c *DatabaseConfig) GetDSN() string {
	if c.Type == "sqlite" {
		return c.Path
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.User, c.Password, c.Host, c.Port, c.DBName)
}
