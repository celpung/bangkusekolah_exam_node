//go:build integration

package service

import (
	"context"
	"fmt"
	"os"
	"testing"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// TestIntegration_HarvestTransportFailureAudit pins HIGH-3 (review v3):
// a transport-level push failure (not a per-attempt rejection) writes a
// harvest_log row with deployment_id and attempts_count, and the attempt
// remains unharvested.
func TestIntegration_HarvestTransportFailureAudit(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set for integration test")
	}
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)

	cleanupIntegrityTables(t, db)
	mustExec(t, db, "DELETE FROM attempts")
	mustExec(t, db, "DELETE FROM items")
	mustExec(t, db, "DELETE FROM exams")
	mustExec(t, db, "DELETE FROM participants")

	harvestSeedExam(t, db, repo, txManager, "exam-xfail", "dep-xfail", "VVVVVV-111111")
	harvestInsertAttempt(t, db, "att-xfail", "exam-xfail")

	// Pusher returns a transport error — simulates network timeout / 500.
	errClient := &errPusher{err: fmt.Errorf("connection refused")}
	harvestSvc := NewHarvestService(repo, errClient)

	n, drainErr := harvestSvc.DrainOnce(context.Background())
	if drainErr == nil {
		t.Fatal("expected transport error to surface")
	}
	if n != 0 {
		t.Fatalf("accepted %d on transport failure, want 0", n)
	}

	// Attempt remains unharvested.
	var unharvested int
	if err := db.Raw(`SELECT COUNT(*) FROM attempts WHERE id = 'att-xfail' AND harvested_at IS NULL`).Scan(&unharvested).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if unharvested != 1 {
		t.Fatalf("transport-failed attempt should remain unharvested, got %d", unharvested)
	}

	// harvest_log row exists with deployment_id and attempts_count.
	var depID string
	var attemptsCount int
	row := db.Raw(`SELECT deployment_id, attempts_count FROM harvest_log WHERE attempt_id = 'att-xfail' LIMIT 1`).Row()
	if err := row.Scan(&depID, &attemptsCount); err != nil {
		t.Fatalf("scan log row: %v", err)
	}
	if depID != "dep-xfail" || attemptsCount < 1 {
		t.Fatalf("log row: deployment_id=%q attempts_count=%d", depID, attemptsCount)
	}
}

type errPusher struct{ err error }

func (e *errPusher) Push(context.Context, string, inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	return nil, e.err
}
