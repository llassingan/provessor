package config

import (
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DBEncryptionKey     string
	CORSOrigins         []string
	Dev                 bool
	LogFile             string
	LoginMaxAttempts    int
	LoginLockoutMinutes int
}

func Load() (*Config, error) {
	_ = godotenv.Load() // optional — env vars may come from Docker/system env instead

	key := os.Getenv("DB_ENCRYPTION_KEY")
	if key == "" {
		return nil, errors.New("DB_ENCRYPTION_KEY environment variable is required")
	}
	if len(key) != 64 {
		return nil, errors.New("DB_ENCRYPTION_KEY must be exactly 64 hex characters (32 bytes)")
	}
	if _, err := hex.DecodeString(key); err != nil {
		return nil, errors.New("DB_ENCRYPTION_KEY must be valid hex string")
	}

	origins := parseOrigins(os.Getenv("CORS_ORIGINS"))
	dev := strings.ToLower(os.Getenv("DEV")) == "true"

	logFile := os.Getenv("LOG_FILE")
	maxAttempts := parseIntEnv("LOGIN_MAX_ATTEMPTS", 5)
	lockoutMinutes := parseIntEnv("LOGIN_LOCKOUT_MINUTES", 15)

	return &Config{
		DBEncryptionKey:     key,
		CORSOrigins:         origins,
		Dev:                 dev,
		LogFile:             logFile,
		LoginMaxAttempts:    maxAttempts,
		LoginLockoutMinutes: lockoutMinutes,
	}, nil
}

func parseIntEnv(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.Atoi(val)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

func parseOrigins(raw string) []string {
	if raw == "" {
		return []string{"http://localhost:5173", "http://localhost:10001"}
	}
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			origins = append(origins, p)
		}
	}
	if len(origins) == 0 {
		return []string{"http://localhost:5173", "http://localhost:10001"}
	}
	return origins
}
