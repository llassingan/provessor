package config

import "testing"

const testEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestLoadRejectsHTTPAPIURLWhenNotDev(t *testing.T) {
	t.Setenv("DB_ENCRYPTION_KEY", testEncryptionKey)
	t.Setenv("DEV", "false")
	t.Setenv("API_URL", "http://api.example.com")

	_, err := Load()
	if err == nil {
		t.Fatal("expected non-dev HTTP API_URL to be rejected")
	}
}

func TestLoadAllowsHTTPSAPIURLWhenNotDev(t *testing.T) {
	t.Setenv("DB_ENCRYPTION_KEY", testEncryptionKey)
	t.Setenv("DEV", "false")
	t.Setenv("API_URL", "https://api.example.com/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.APIURL != "https://api.example.com" {
		t.Fatalf("expected trimmed HTTPS APIURL, got %q", cfg.APIURL)
	}
}

func TestLoadAllowsDefaultHTTPAPIURLInDev(t *testing.T) {
	t.Setenv("DB_ENCRYPTION_KEY", testEncryptionKey)
	t.Setenv("DEV", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.APIURL != "http://localhost:10000" {
		t.Fatalf("expected default dev APIURL, got %q", cfg.APIURL)
	}
}
