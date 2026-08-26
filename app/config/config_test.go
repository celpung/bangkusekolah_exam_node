package config

import (
	"testing"
	"time"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DSN", "user:pass@tcp(127.0.0.1:3306)/examnode?parseTime=true")
	t.Setenv("NODE_JWT_SECRET", "0123456789012345678901234567890123456789")
	t.Setenv("CENTRAL_BASE_URL", "https://central.example.test")
	t.Setenv("CENTRAL_NODE_TOKEN", "node-1.SECRET")
}

func TestLoadAppliesTheDocumentedPoolDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.DBMaxOpenConns != 50 || cfg.DBMaxIdleConns != 25 {
		t.Fatalf("pool = %d open / %d idle, want 50 / 25", cfg.DBMaxOpenConns, cfg.DBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime != 30*time.Minute {
		t.Fatalf("conn max lifetime = %v, want 30m", cfg.DBConnMaxLifetime)
	}
	if cfg.MaxInflightRequests != 400 {
		t.Fatalf("max in-flight = %d, want 400", cfg.MaxInflightRequests)
	}
	if cfg.HarvestInterval != 5*time.Minute || cfg.SweepInterval != time.Minute {
		t.Fatalf("tickers = %v harvest / %v sweep, want 5m / 1m", cfg.HarvestInterval, cfg.SweepInterval)
	}
	// Task 16 contract: student JWT lives 90 minutes when NODE_JWT_TTL is unset.
	if cfg.JWTTTL != 90*time.Minute {
		t.Fatalf("jwt ttl default = %v, want 90m", cfg.JWTTTL)
	}
}

func TestLoadRejectsAShortJWTSecret(t *testing.T) {
	setRequired(t)
	t.Setenv("NODE_JWT_SECRET", "too-short")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a JWT secret under 32 characters")
	}
}

func TestLoadRejectsAMissingRequiredValue(t *testing.T) {
	for _, key := range []string{"DB_DSN", "CENTRAL_BASE_URL", "CENTRAL_NODE_TOKEN"} {
		t.Run(key, func(t *testing.T) {
			setRequired(t)
			t.Setenv(key, "")
			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted an empty %s", key)
			}
		})
	}
}
