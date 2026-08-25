package provider

import (
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
)

func TestConnectAppliesThePoolBounds(t *testing.T) {
	cfg := &config.Config{
		DBDSN:             "user:pass@tcp(127.0.0.1:3306)/examnode?parseTime=true",
		DBMaxOpenConns:    7,
		DBMaxIdleConns:    3,
		DBConnMaxLifetime: time.Minute,
	}

	db, err := Connect(cfg)
	if err != nil {
		t.Skipf("no database available: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql handle: %v", err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 7 {
		t.Fatalf("MaxOpenConnections = %d, want 7", got)
	}
}
