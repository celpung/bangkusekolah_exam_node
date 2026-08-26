package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the node's whole configuration. It is read once at startup; the
// node has no runtime settings UI and no database-backed configuration table.
type Config struct {
	HTTPPort            string
	DBDSN               string
	DBMaxOpenConns      int
	DBMaxIdleConns      int
	DBConnMaxLifetime   time.Duration
	MaxInflightRequests int
	JWTSecret           string
	JWTTTL              time.Duration
	CentralBaseURL      string
	CentralNodeToken    string
	DeploymentID        string
	HarvestInterval     time.Duration
	SweepInterval       time.Duration
	HeartbeatInterval   time.Duration
	LoginRateLimit      int
	LoginRateWindow     time.Duration
}

// Defaults. The pool numbers are risk 1 in the design: an unconfigured pool is
// the most likely way this box falls over, so nothing here is left to GORM.
const (
	defaultHTTPPort            = "8080"
	defaultDBMaxOpenConns      = 50
	defaultDBMaxIdleConns      = 25
	defaultDBConnMaxLifetime   = 30 * time.Minute
	defaultMaxInflightRequests = 400
	// Student JWTs live 90 minutes per the Task 16 contract; an unconfigured
	// deployment must not silently issue six-hour tokens.
	defaultJWTTTL            = 90 * time.Minute
	defaultHarvestInterval   = 5 * time.Minute
	defaultSweepInterval     = time.Minute
	defaultHeartbeatInterval = time.Minute
	defaultLoginRateLimit    = 10
	defaultLoginRateWindow   = time.Minute
	minJWTSecretLength       = 32
)

func Load() (*Config, error) {
	cfg := &Config{
		HTTPPort:            stringFromEnv("HTTP_PORT", defaultHTTPPort),
		DBDSN:               os.Getenv("DB_DSN"),
		DBMaxOpenConns:      intFromEnv("DB_MAX_OPEN_CONNS", defaultDBMaxOpenConns),
		DBMaxIdleConns:      intFromEnv("DB_MAX_IDLE_CONNS", defaultDBMaxIdleConns),
		DBConnMaxLifetime:   durationFromEnv("DB_CONN_MAX_LIFETIME", defaultDBConnMaxLifetime),
		MaxInflightRequests: intFromEnv("MAX_INFLIGHT_REQUESTS", defaultMaxInflightRequests),
		JWTSecret:           os.Getenv("NODE_JWT_SECRET"),
		JWTTTL:              durationFromEnv("NODE_JWT_TTL", defaultJWTTTL),
		CentralBaseURL:      os.Getenv("CENTRAL_BASE_URL"),
		CentralNodeToken:    os.Getenv("CENTRAL_NODE_TOKEN"),
		DeploymentID:        os.Getenv("DEPLOYMENT_ID"),
		HarvestInterval:     durationFromEnv("HARVEST_INTERVAL", defaultHarvestInterval),
		SweepInterval:       durationFromEnv("SWEEP_INTERVAL", defaultSweepInterval),
		HeartbeatInterval:   durationFromEnv("HEARTBEAT_INTERVAL", defaultHeartbeatInterval),
		LoginRateLimit:      intFromEnv("LOGIN_RATE_LIMIT", defaultLoginRateLimit),
		LoginRateWindow:     durationFromEnv("LOGIN_RATE_WINDOW", defaultLoginRateWindow),
	}
	required := map[string]string{
		"DB_DSN":             cfg.DBDSN,
		"CENTRAL_BASE_URL":   cfg.CentralBaseURL,
		"CENTRAL_NODE_TOKEN": cfg.CentralNodeToken,
	}
	for key, value := range required {
		if value == "" {
			return nil, fmt.Errorf("%s is required", key)
		}
	}
	if len(cfg.JWTSecret) < minJWTSecretLength {
		return nil, fmt.Errorf("NODE_JWT_SECRET must be at least %d characters long", minJWTSecretLength)
	}
	return cfg, nil
}

func stringFromEnv(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func intFromEnv(key string, fallback int) int {
	parsed, err := strconv.Atoi(os.Getenv(key))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(os.Getenv(key))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
