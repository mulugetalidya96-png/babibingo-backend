package config

import "os"

type Config struct {
	DatabaseURL string
	RedisURL    string
	BotToken    string
	WebAppURL   string
	JWTSecret   string
	Bot BotConfig `json:"bot"`
	VerifyAPIKey string `env:"VERIFY_API_KEY" default:""`
}
type BotConfig struct {
	Enabled         bool `json:"enabled"`
	MinBotsPerGame  int  `json:"min_bots_per_game"`
	MaxBotsPerGame  int  `json:"max_bots_per_game"`
	BotsPerTick     int  `json:"bots_per_tick"`
	ReserveInterval int  `json:"reserve_interval"` // seconds
}
func Load() *Config {
	return &Config{
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/babibingo?sslmode=disable"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379/0"),
		BotToken:    getEnv("TELEGRAM_BOT_TOKEN", ""),
		WebAppURL:   getEnv("WEBAPP_URL", "https://your-domain.com"),
		JWTSecret:   getEnv("JWT_SECRET", "your-secret-key-change-in-production"),
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
