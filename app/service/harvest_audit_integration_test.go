//go:build integration

package service

import (
	"context"
	"os"
	"testing"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// TestIntegration_HarvestAuditLog pins HIGH-3 (review v2): transport failures
// and per-attempt rejections land in harvest_log with deployment/count
// metadata; rejected attempts stay unharvested while acked ones are marked.
func TestIntegration_HarvestAuditLog(t *testing.T) {
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
	mustExec(t, db, "DELETE FROM items")
	mustExec(t, db, "DELETE FROM exams")
	mustExec(t, db, "DELETE FROM participants")

	harvestSeedExam(t, db, repo, txManager, "exam-audit", "dep-audit", "WWWWWW-111111")
	harvestInsertAttempt(t, db, "audit-reject", "exam-audit")
	harvestInsertAttempt(t, db, "audit-accept", "exam-audit")

	// Central accepts audit-accept, rejects audit-reject.
	client := &multiDeploymentClient{ack: func(dep string, batch inbound.ExamNodeAttemptBatch) inbound.ExamNodeIngestResult {
		res := inbound.ExamNodeIngestResult{Failures: map[string]string{}}
		for _, a := range batch.Attempts {
			if a.ID == "audit-accept" {
				res.AcceptedAttemptIDs = append(res.AcceptedAttemptIDs, a.ID)
			} else {
				res.Failures[a.ID] = "validation failed at central"
			}
		}
		return res
	}}
	harvestSvc := NewHarvestService(repo, client)
	n, err := harvestSvc.DrainOnce(context.Background())
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n != 1 {
		t.Fatalf("accepted %d, want 1", n)
	}

	var acceptMarked int
	if err := db.Raw(`SELECT COUNT(*) FROM attempts WHERE id = 'audit-accept' AND harvested_at IS NOT NULL`).Scan(&acceptMarked).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if acceptMarked != 1 {
		t.Fatalf("acked attempt harvested=%d, want 1", acceptMarked)
	}

	// Rejected attempt remains unharvested and audited with metadata.
	var rejectUnharvested int
	if err := db.Raw(`SELECT COUNT(*) FROM attempts WHERE id = 'audit-reject' AND harvested_at IS NULL`).Scan(&rejectUnharvested).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if rejectUnharvested != 1 {
		t.Fatalf("rejected attempt must stay unharvested")
	}
	row := db.Raw(`SELECT deployment_id, attempts_count FROM harvest_log WHERE attempt_id = 'audit-reject' LIMIT 1`).Row()
	var depID string
	var attemptsCount int
	if err := row.Scan(&depID, &attemptsCount); err != nil {
		t.Fatalf("scan log row: %v", err)
	}
	if depID != "dep-audit" || attemptsCount < 1 {
		t.Fatalf("log row: deployment_id=%q attempts_count=%d", depID, attemptsCount)
	}
}
