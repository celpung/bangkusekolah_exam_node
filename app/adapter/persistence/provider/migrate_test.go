package provider

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func dsnFromEnv() string {
	return os.Getenv("TEST_DB_DSN")
}

func TestMigrateIsIdempotent(t *testing.T) {
	dsn := dsnFromEnv()
	if dsn == "" || os.Getenv("TEST_DB_DSN") == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	if _, err := os.Stat("../../migrations"); err != nil {
		t.Skip("migrations dir not reachable from test working directory (run from repo root)")
	}
	// RunFrom with an absolute path so the test is cwd-independent.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := filepath.Join(wd, "..", "..", "migrations")
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("no db: %v", err)
	}
	defer db.Close()
	for i := 0; i < 2; i++ {
		if err := RunFrom(db, dir); err != nil {
			t.Fatalf("migrate run %d: %v", i+1, err)
		}
	}
}
