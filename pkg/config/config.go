package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Mode string

const (
	ModeAllInOne Mode = "all-in-one"
	ModeServer   Mode = "server"
	ModeWorker   Mode = "worker"
	ModeIngest   Mode = "ingest"
)

type SecurityMode string

const (
	SecurityModeFailOpen   SecurityMode = "fail-open"
	SecurityModeFailClosed SecurityMode = "fail-closed"
)

type Config struct {
	Mode         Mode
	Port         int
	DatabaseURL  string
	RedisURL     string
	ClickHouseURL string
	SecurityMode SecurityMode
	LogLevel     string

	// Encryption
	MasterEncryptionKey string

	// CORS allowlist. Empty disables CORS entirely (dashboard is served from
	// the same origin, so CORS is only needed for split-origin deployments).
	CORSOrigins []string

	// MaxRequestBodyBytes caps the size of JSON request bodies to management
	// endpoints. Gateway chat/completions has its own larger cap.
	MaxRequestBodyBytes int64

	// DataDir is the root for blob storage the OSS deployment owns
	// (workspace knowledge files, generated reports, etc.). Cloud swaps
	// the storage interface to S3; OSS uses local filesystem rooted here.
	// Created on first write if missing.
	DataDir string

	// PlatformHosts is the canonical hostname list the OSS server knows
	// itself by. Any inbound Host outside this set is treated as a
	// candidate custom domain (workspace branded chat); a match against
	// `workspace_domains` swaps in the branded handler. Empty list
	// disables custom-domain interception (sensible default for tests).
	PlatformHosts []string

	// IPListEnabled turns on the IP threat-list subsystem
	// (internal/security/iplist): background feed refresh plus a gateway
	// middleware that checks client IPs against FireHOL level1 and the
	// Tor exit-node list. Disabled by default — zero behavior change
	// unless explicitly enabled.
	IPListEnabled bool

	// IPListBlock switches the iplist middleware from annotate-only
	// (default: attach a verdict + synthetic finding to the request)
	// to blocking listed IPs with a 403.
	IPListBlock bool

	// IPListRefresh is the cadence of the background feed refresh.
	IPListRefresh time.Duration

	// IPListFireHOLURL / IPListTorURL override the public feed URLs.
	// Empty uses the provider defaults (FireHOL level1 netset on GitHub
	// raw; check.torproject.org bulk exit list).
	IPListFireHOLURL string
	IPListTorURL     string
}

func Load() (*Config, error) {
	cfg := &Config{
		Mode:         Mode(envOr("BASTIO_MODE", "all-in-one")),
		Port:         envIntOr("BASTIO_PORT", 4000),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		RedisURL:     envOr("REDIS_URL", "redis://localhost:6379"),
		ClickHouseURL: envOr("CLICKHOUSE_URL", "http://localhost:8123"),
		SecurityMode: SecurityMode(envOr("BASTIO_SECURITY_MODE", "fail-open")),
		LogLevel:     envOr("BASTIO_LOG_LEVEL", "info"),
		MasterEncryptionKey: os.Getenv("BASTIO_MASTER_ENCRYPTION_KEY"),
		CORSOrigins:         envListOr("BASTIO_CORS_ORIGINS", nil),
		MaxRequestBodyBytes: int64(envIntOr("BASTIO_MAX_BODY_BYTES", 1<<20)),
		DataDir:             envOr("BASTIO_DATA_DIR", "./data"),
		PlatformHosts: envListOr("BASTIO_PLATFORM_HOSTS", []string{
			"localhost", "127.0.0.1", "0.0.0.0",
		}),
		IPListEnabled:    envBoolOr("BASTIO_IPLIST_ENABLED", false),
		IPListBlock:      envBoolOr("BASTIO_IPLIST_BLOCK", false),
		IPListRefresh:    envDurationOr("BASTIO_IPLIST_REFRESH", 6*time.Hour),
		IPListFireHOLURL: os.Getenv("BASTIO_IPLIST_FIREHOL_URL"),
		IPListTorURL:     os.Getenv("BASTIO_IPLIST_TOR_URL"),
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	switch c.Mode {
	case ModeAllInOne, ModeServer, ModeWorker, ModeIngest:
	default:
		return fmt.Errorf("invalid BASTIO_MODE: %q", c.Mode)
	}

	switch c.SecurityMode {
	case SecurityModeFailOpen, SecurityModeFailClosed:
	default:
		return fmt.Errorf("invalid BASTIO_SECURITY_MODE: %q", c.SecurityMode)
	}

	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envBoolOr(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envDurationOr(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}

func envListOr(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
