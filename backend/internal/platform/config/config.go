package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr            string
	DatabaseURL         string
	AllowedOrigins      []string
	DemoMode            bool
	DatabaseEnabled     bool
	StartupTimeout      time.Duration
	ShutdownTimeout     time.Duration
	SessionTTL          time.Duration
	SessionCookieSecure bool
}

// Load deliberately does not read .env files. The launcher supplies environment.
func Load(getenv func(string) string) (Config, error) {
	c := Config{HTTPAddr: ":8080", DatabaseEnabled: true, StartupTimeout: 15 * time.Second, ShutdownTimeout: 10 * time.Second, SessionTTL: 24 * time.Hour}
	if value := getenv("HTTP_ADDR"); value != "" {
		c.HTTPAddr = value
	}
	_, port, err := net.SplitHostPort(c.HTTPAddr)
	if err != nil {
		return Config{}, fmt.Errorf("HTTP_ADDR must be host:port")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 0 || p > 65535 {
		return Config{}, fmt.Errorf("HTTP_ADDR port must be 0..65535")
	}
	for key, target := range map[string]*bool{"DEMO_MODE": &c.DemoMode, "DATABASE_ENABLED": &c.DatabaseEnabled, "SESSION_COOKIE_SECURE": &c.SessionCookieSecure} {
		if value := getenv(key); value != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return Config{}, fmt.Errorf("%s must be a boolean", key)
			}
			*target = parsed
		}
	}
	if value := getenv("SESSION_TTL"); value != "" {
		ttl, err := time.ParseDuration(value)
		if err != nil || ttl < time.Second || ttl > 365*24*time.Hour {
			return Config{}, fmt.Errorf("SESSION_TTL must be between 1s and 8760h")
		}
		c.SessionTTL = ttl
	}
	for key, target := range map[string]*time.Duration{"STARTUP_TIMEOUT": &c.StartupTimeout, "SHUTDOWN_TIMEOUT": &c.ShutdownTimeout} {
		if value := getenv(key); value != "" {
			parsed, err := time.ParseDuration(value)
			if err != nil || parsed <= 0 {
				return Config{}, fmt.Errorf("%s must be a positive duration", key)
			}
			*target = parsed
		}
	}
	c.DatabaseURL = getenv("DATABASE_URL")
	if c.DatabaseEnabled {
		u, err := url.Parse(c.DatabaseURL)
		if err != nil || u == nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" || u.Path == "" || u.Path == "/" {
			return Config{}, fmt.Errorf("DATABASE_URL must be a PostgreSQL URL with host and database")
		}
	} else if !c.DemoMode {
		return Config{}, fmt.Errorf("DATABASE_ENABLED=false requires DEMO_MODE=true; only infrastructure endpoints are available")
	}
	origins := getenv("ALLOWED_ORIGINS")
	if origins == "" {
		origins = "http://localhost:5173,http://localhost:8080"
	}
	for _, raw := range strings.Split(origins, ",") {
		raw = strings.TrimSpace(raw)
		u, err := url.Parse(raw)
		if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
			return Config{}, fmt.Errorf("ALLOWED_ORIGINS must contain comma-separated HTTP origins without paths")
		}
		c.AllowedOrigins = append(c.AllowedOrigins, raw)
	}
	return c, nil
}
