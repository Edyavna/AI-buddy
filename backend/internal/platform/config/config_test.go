package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	valid := map[string]string{"DATABASE_URL": "postgres://buddy:secret@localhost:5432/buddy?sslmode=disable"}
	tests := []struct{ name, key, value, want string }{
		{"defaults", "", "", ""},
		{"http", "HTTP_ADDR", "127.0.0.1:9000", ""},
		{"bad address", "HTTP_ADDR", "localhost", "HTTP_ADDR"},
		{"bad port", "HTTP_ADDR", ":abc", "HTTP_ADDR"},
		{"large port", "HTTP_ADDR", ":65536", "HTTP_ADDR"},
		{"demo", "DEMO_MODE", "true", ""},
		{"bad demo", "DEMO_MODE", "wat", "DEMO_MODE"},
		{"bad db flag", "DATABASE_ENABLED", "wat", "DATABASE_ENABLED"},
		{"db missing", "DATABASE_URL", "", "DATABASE_URL"},
		{"db secret redacted", "DATABASE_URL", "postgres://user:secret%xx@host/db", "DATABASE_URL"},
		{"db path missing", "DATABASE_URL", "postgres://host", "DATABASE_URL"},
		{"db empty path", "DATABASE_URL", "postgres://host/", "DATABASE_URL"},
		{"db wrong scheme", "DATABASE_URL", "https://host/db", "DATABASE_URL"},
		{"db disabled requires demo", "DATABASE_ENABLED", "false", "requires DEMO_MODE"},
		{"origins", "ALLOWED_ORIGINS", "http://localhost:5173, https://buddy.example", ""},
		{"origin wildcard", "ALLOWED_ORIGINS", "*", "ALLOWED_ORIGINS"},
		{"origin path", "ALLOWED_ORIGINS", "http://localhost/", "ALLOWED_ORIGINS"},
		{"origin userinfo", "ALLOWED_ORIGINS", "https://user:secret@host", "ALLOWED_ORIGINS"},
		{"startup", "STARTUP_TIMEOUT", "25s", ""},
		{"bad startup", "STARTUP_TIMEOUT", "bad", "STARTUP_TIMEOUT"},
		{"zero shutdown", "SHUTDOWN_TIMEOUT", "0s", "SHUTDOWN_TIMEOUT"},
		{"session ttl", "SESSION_TTL", "2h", ""},
		{"bad session ttl", "SESSION_TTL", "bad", "SESSION_TTL"},
		{"short session ttl", "SESSION_TTL", "500ms", "SESSION_TTL"},
		{"long session ttl", "SESSION_TTL", "8761h", "SESSION_TTL"},
		{"secure cookie", "SESSION_COOKIE_SECURE", "true", ""},
		{"bad secure flag", "SESSION_COOKIE_SECURE", "bad", "SESSION_COOKIE_SECURE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]string{}
			for k, v := range valid {
				values[k] = v
			}
			values[tt.key] = tt.value
			cfg, err := Load(func(key string) string { return values[key] })
			if tt.want != "" {
				if err == nil || !strings.Contains(err.Error(), tt.want) || strings.Contains(err.Error(), "secret") {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTPAddr == "" || cfg.StartupTimeout <= 0 || len(cfg.AllowedOrigins) == 0 {
				t.Fatalf("incomplete config: %+v", cfg)
			}
		})
	}
	withoutDB, err := Load(func(key string) string {
		return map[string]string{"DATABASE_ENABLED": "false", "DEMO_MODE": "true"}[key]
	})
	if err != nil || withoutDB.DatabaseEnabled {
		t.Fatalf("no-db mode: %v", err)
	}
}
