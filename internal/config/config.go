package config

import (
	"encoding/hex"
	"errors"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DBEncryptionKey string
	CORSOrigins     []string
	Dev             bool
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

	return &Config{DBEncryptionKey: key, CORSOrigins: origins, Dev: dev}, nil
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
