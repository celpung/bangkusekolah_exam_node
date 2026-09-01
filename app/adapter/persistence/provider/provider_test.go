package provider

import (
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestNormalizeDBDSNForcesUTCAndParseTime(t *testing.T) {
	dsn, err := normalizeDBDSN("user:pass@tcp(127.0.0.1:3306)/examnode?parseTime=false&loc=Local")
	if err != nil {
		t.Fatalf("normalizeDBDSN: %v", err)
	}
	parsed, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN(normalized): %v", err)
	}
	if !parsed.ParseTime {
		t.Fatal("normalized DSN must enable parseTime")
	}
	if parsed.Loc != time.UTC {
		t.Fatalf("normalized DSN location = %v, want UTC", parsed.Loc)
	}
}

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
