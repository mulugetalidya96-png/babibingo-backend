package config

import "os"

type Config struct {
	DatabaseURL string
	RedisURL    string
	BotToken    string
	WebAppURL   string
	JWTSecret   string
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
