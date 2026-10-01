// Package config holds runtime configuration. Everything comes from the
// environment with defaults; model and provider selection lives in
// settings.json instead (see internal/settings), because a desktop app is
// configured from its own UI, not from a deployment.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	// Home is the root for the database, workspaces, worklogs, memory and traces.
	Home string
	// Addr is where `hidane serve` listens.
	Addr string
	// APIToken guards /api/* in serve mode. The desktop webview needs none.
	APIToken string
	// WebhookSecret enables HMAC verification of /webhook/:name.
	WebhookSecret string

	HeartbeatInterval    time.Duration
	DistillInterval      time.Duration
	RouteTimeout         time.Duration
	WorkerTimeout        time.Duration
	MaxHops              int
	MaxWorkers           int
	MaxConcurrentTurns   int
	MaxExecutionsPerItem int
	AttributionThreshold float64

	FeishuAppID             string
	FeishuAppSecret         string
	FeishuVerificationToken string
	FeishuEncryptKey        string
}

func env(name string) string { return os.Getenv(name) }

func envInt(name string, def int) int {
	if v := env(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(name string, def float64) float64 {
	if v := env(name); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}

func envSec(name string, def int) time.Duration {
	return time.Duration(envInt(name, def)) * time.Second
}

// Load reads the environment.
func Load() *Config {
	home := env("HIDANE_HOME")
	if home == "" {
		dir, err := os.UserHomeDir()
		if err != nil {
			dir = "."
		}
		home = filepath.Join(dir, ".hidane")
	}
	addr := env("HIDANE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:2718"
	}
	return &Config{
		Home:                    home,
		Addr:                    addr,
		APIToken:                env("HIDANE_API_TOKEN"),
		WebhookSecret:           env("HIDANE_WEBHOOK_SECRET"),
		HeartbeatInterval:       envSec("HIDANE_HEARTBEAT_SEC", 300),
		DistillInterval:         envSec("HIDANE_DISTILL_SEC", 600),
		RouteTimeout:            envSec("HIDANE_ROUTE_TIMEOUT_SEC", 180),
		WorkerTimeout:           envSec("HIDANE_WORKER_TIMEOUT_SEC", 600),
		MaxHops:                 envInt("HIDANE_MAX_HOPS", 24),
		MaxWorkers:              envInt("HIDANE_MAX_WORKERS", 3),
		MaxConcurrentTurns:      envInt("HIDANE_MAX_TURNS", 4),
		MaxExecutionsPerItem:    envInt("HIDANE_MAX_EXECUTIONS_PER_ITEM", 12),
		AttributionThreshold:    envFloat("HIDANE_ATTRIBUTION_THRESHOLD", 0.6),
		FeishuAppID:             env("FEISHU_APP_ID"),
		FeishuAppSecret:         env("FEISHU_APP_SECRET"),
		FeishuVerificationToken: env("FEISHU_VERIFICATION_TOKEN"),
		FeishuEncryptKey:        env("FEISHU_ENCRYPT_KEY"),
	}
}

// ForTest is a configuration rooted at dir with production defaults.
func ForTest(dir string) *Config {
	c := Load()
	c.Home = dir
	c.APIToken = ""
	c.WebhookSecret = ""
	return c
}

func (c *Config) DBPath() string        { return filepath.Join(c.Home, "hidane.db") }
func (c *Config) WorkspacesDir() string { return filepath.Join(c.Home, "workspaces") }
func (c *Config) WorklogsDir() string   { return filepath.Join(c.Home, "worklogs") }
func (c *Config) SessionsDir() string   { return filepath.Join(c.Home, "sessions") }
func (c *Config) SettingsPath() string  { return filepath.Join(c.Home, "settings.json") }
func (c *Config) RuntimeDir() string    { return filepath.Join(c.Home, "runtime") }
