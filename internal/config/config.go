package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the notification interceptor.
type Config struct {
	WhatsApp WhatsAppConfig `yaml:"whatsapp"`
	Telegram TelegramConfig `yaml:"telegram"`
	Slack    SlackConfig    `yaml:"slack"`
	Gmail    GmailConfig    `yaml:"gmail"`
	Pushover PushoverConfig `yaml:"pushover"`
	LLM      LLMConfig      `yaml:"llm"`
	Calendar CalendarConfig `yaml:"calendar"`
}

type WhatsAppConfig struct {
	Enabled     bool   `yaml:"enabled"`
	StoragePath string `yaml:"storage_path"`
}

type TelegramConfig struct {
	Enabled  bool   `yaml:"enabled"`
	AppID    int    `yaml:"app_id"`
	AppHash  string `yaml:"app_hash"`
	Phone    string `yaml:"phone"`
	DataPath string `yaml:"data_path"`
}

type SlackConfig struct {
	Enabled  bool   `yaml:"enabled"`
	AppToken string `yaml:"app_token"`
	BotToken string `yaml:"bot_token"`
}

type GmailConfig struct {
	Enabled         bool   `yaml:"enabled"`
	CredentialsPath string `yaml:"credentials_path"`
	TokenPath       string `yaml:"token_path"`
	PollInterval    int    `yaml:"poll_interval_seconds"`
}

type PushoverConfig struct {
	AppToken  string `yaml:"app_token"`
	UserToken string `yaml:"user_token"`
}

type LLMConfig struct {
	Provider string `yaml:"provider"` // "openai" or "gemini"
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
}

type CalendarConfig struct {
	Enabled                bool   `yaml:"enabled"`
	CredentialsPath        string `yaml:"credentials_path"`
	TokenPath              string `yaml:"token_path"`
	DefaultDurationMinutes int    `yaml:"default_duration_minutes"`
	CalendarID             string `yaml:"calendar_id"`
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Expand environment variables
	data = []byte(os.ExpandEnv(string(data)))

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		WhatsApp: WhatsAppConfig{
			Enabled:     true,
			StoragePath: "./data/whatsapp",
		},
		Telegram: TelegramConfig{
			Enabled:  true,
			DataPath: "./data/telegram",
		},
		Slack: SlackConfig{
			Enabled: true,
		},
		Gmail: GmailConfig{
			Enabled:         true,
			CredentialsPath: "./credentials.json",
			TokenPath:       "./token.json",
			PollInterval:    60,
		},
		Calendar: CalendarConfig{
			Enabled:                false,
			CredentialsPath:        "./credentials.json",
			TokenPath:              "./calendar-token.json",
			DefaultDurationMinutes: 30,
			CalendarID:             "primary",
		},
	}
}
