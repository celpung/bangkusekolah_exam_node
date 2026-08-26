//go:build integration

package service

import (
	"context"
	"os"
	"testing"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/provider"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
)

// TestIntegration_PreflightFailsClosedOnEmptyNode pins BLOCKER-1: a migrated
// but exam-less node must not pass readiness — BundleService.Preflight on an
// unloaded exam returns ErrExamNotLoaded, and the CLI's no-exams guard turns
// that into exit 1.
func TestIntegration_PreflightFailsClosedOnEmptyNode(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN_FRESH")
	if dsn == "" {
		t.Skip("TEST_DB_DSN_FRESH not set for integration test")
	}
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect fresh db: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	// Migrated node. The fresh DB is shared with other integration tests, so
	// clear any exam rows another test may have loaded: this test pins the
	// empty-node fail-closed behavior specifically.
	if err := provider.Run(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, q := range []string{"DELETE FROM items", "DELETE FROM exams", "DELETE FROM participants"} {
		if _, err := sqlDB.Exec(q); err != nil {
			t.Fatalf("cleanup %q: %v", q, err)
		}
	}

	repo := repository.NewNodeRepository(db)
	exams, err := repo.ListExams(context.Background())
	if err != nil {
		t.Fatalf("list exams: %v", err)
	}
	if len(exams) != 0 {
		t.Fatalf("fresh node should have zero exams, got %d", len(exams))
	}

	// The CLI fails closed when ListExams is empty; assert the underlying
	// service-level behavior too: preflighting an unloaded exam errors.
	txManager := helper.NewTxManager(db)
	bundleSvc := NewBundleService(repo, txManager, &lifecycleContentSvc{})
	if err := bundleSvc.Preflight(context.Background(), "no-such-exam", 1, 1); err == nil {
		t.Fatal("preflight passed for an unloaded exam — must fail closed")
	}

	// And the raw *sql.DB path mirrors what cmd/preflight does before its
	// no-exams check: zero rows -> the CLI prints FAIL and exits 1.
	var count int
	if err := sqlDB.QueryRow("SELECT COUNT(*) FROM exams").Scan(&count); err != nil {
		t.Fatalf("count exams: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected empty exams table, got %d rows", count)
	}
}
