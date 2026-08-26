package provider

import (
	"database/sql"
	"os"
	"sync"
	"testing"
)

func dsnFromEnv() string {
	return os.Getenv("TEST_DB_DSN")
}

// TestMigrateIsIdempotent uses the embedded migration set — no working
// directory dependency. Runs twice sequentially.
func TestMigrateIsIdempotent(t *testing.T) {
	dsn := dsnFromEnv()
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("no db: %v", err)
	}
	defer db.Close()
	for i := 0; i < 2; i++ {
		if err := Run(db); err != nil {
			t.Fatalf("migrate run %d: %v", i+1, err)
		}
	}
}

// TestMigrateConcurrentRunners pins HIGH-3: two simultaneous runners on the
// same database must both succeed (one applies, the other sees applied state).
func TestMigrateConcurrentRunners(t *testing.T) {
	dsn := dsnFromEnv()
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set")
	}
	db1, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("no db: %v", err)
	}
	defer db1.Close()
	db2, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("no db: %v", err)
	}
	defer db2.Close()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs[0] = Run(db1) }()
	go func() { defer wg.Done(); errs[1] = Run(db2) }()
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent runner %d failed: %v", i+1, err)
		}
	}
}
