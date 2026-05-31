package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the application.
type Config struct {
	DatabaseURL       string
	DataDir           string
	PublicBaseURL     string
	MaxMessageBytes   int64
	SessionCookieName string
	SessionTTLHours   int
	NormalUserPSK     string
	AdminPSK          string
	WorkerIngestPSK   string
	ServerAddr        string
	AppEnv            string
	TelegramBotToken  string
	TelegramChatID    string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	return &Config{
		DatabaseURL:       getEnv("DATABASE_URL", ""),
		DataDir:           getEnv("DATA_DIR", "./data"),
		PublicBaseURL:     getEnv("PUBLIC_BASE_URL", "http://localhost:8080"),
		MaxMessageBytes:   getEnvInt("MAX_MESSAGE_BYTES", 26214400),
		SessionCookieName: getEnv("SESSION_COOKIE_NAME", "lite_mail_session"),
		SessionTTLHours:   int(getEnvInt("SESSION_TTL_HOURS", 24)),
		NormalUserPSK:     getEnv("NORMAL_USER_PSK", ""),
		AdminPSK:          getEnv("ADMIN_PSK", ""),
		WorkerIngestPSK:   getEnv("WORKER_INGEST_PSK", ""),
		ServerAddr:        getEnv("SERVER_ADDR", ":8080"),
		AppEnv:            getEnv("APP_ENV", "production"),
		TelegramBotToken:  getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:    getEnv("TELEGRAM_CHAT_ID", ""),
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return i
		}
	}
	return defaultValue
}

// SessionTTL returns the session TTL as a time.Duration.
func (c *Config) SessionTTL() time.Duration {
	return time.Duration(c.SessionTTLHours) * time.Hour
}

// TelegramEnabled returns true only when both bot token and chat ID are configured.
func (c *Config) TelegramEnabled() bool {
	return c.TelegramBotToken != "" && c.TelegramChatID != ""
}
