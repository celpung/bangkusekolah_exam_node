//go:build integration

package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	outbound "github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

func harvestSeedExam(t *testing.T, db *gorm.DB, repo outbound_repository.NodeRepository, txManager outbound.TxManager, examID, depID, code string) {
	t.Helper()
	starts := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	bundleSvc := NewBundleService(repo, txManager, &lifecycleContentSvc{})
	b := inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: depID,
		Exam: inbound.ExamNodeBundleExam{
			ID: examID, Title: examID, StartsAt: starts, EndsAt: starts.Add(2 * time.Hour),
			DurationMinutes: 60, MaxAttempts: 1, ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{{
			ID: "item-" + examID, SectionID: "sec-1", QuestionType: "single_choice",
			PromptSnapshot: "q", Points: 10,
		}},
		Participants: []inbound.ExamNodeBundleParticipant{{
			ID: "p-" + examID, StudentID: "s-" + examID, StudentName: "Budi", AccessCode: code,
		}},
	}
	b.Checksum = ComputeBundleChecksum(b)
	if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
		t.Fatalf("load %s: %v", examID, err)
	}
}

func harvestInsertAttempt(t *testing.T, db *gorm.DB, id, examID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO attempts (id, participant_id, student_id, exam_id, attempt_no, status,
		started_at, due_at, submitted_at, max_score, grading_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, "p-"+id, "s-"+id, examID, 1, "submitted",
		now.Add(-time.Hour), now, now, 10, "graded").Error; err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

// TestIntegration_HarvestOrphanDoesNotBlockValidDeployment pins BLOCKER-1
// (review v2): one orphaned attempt must not stop valid deployments from
// draining; it is logged and left unharvested with an aggregated error.
func TestIntegration_HarvestOrphanDoesNotBlockValidDeployment(t *testing.T) {
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

	// Valid exam B; attempt A1 points at a nonexistent exam A.
	harvestSeedExam(t, db, repo, txManager, "exam-orphan-b", "dep-B", "QQQQQQ-222222")
	harvestInsertAttempt(t, db, "att-orphan", "exam-deleted-a")
	harvestInsertAttempt(t, db, "att-valid-b", "exam-orphan-b")

	client := &multiDeploymentClient{ack: func(dep string, batch inbound.ExamNodeAttemptBatch) inbound.ExamNodeIngestResult {
		res := inbound.ExamNodeIngestResult{}
		for _, a := range batch.Attempts {
			res.AcceptedAttemptIDs = append(res.AcceptedAttemptIDs, a.ID)
		}
		return res
	}}
	harvestSvc := NewHarvestService(repo, client)

	n, drainErr := harvestSvc.DrainOnce(context.Background())
	if n != 1 || strings.Contains("", "") == false {
		t.Fatalf("drained %d, want 1", n)
	}
	// Aggregated error mentions the orphan but the valid push already happened.
	if drainErr == nil || !strings.Contains(drainErr.Error(), "orphaned") {
		t.Fatalf("expected aggregated orphan error, got %v", drainErr)
	}

	var marked int
	if err := db.Raw(`SELECT COUNT(*) FROM attempts WHERE id = 'att-valid-b' AND harvested_at IS NOT NULL`).Scan(&marked).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if marked != 1 {
		t.Fatalf("valid attempt harvested=%d, want 1", marked)
	}

	// Orphan remains unharvested and is audited in harvest_log.
	var unharvested int
	if err := db.Raw(`SELECT COUNT(*) FROM attempts WHERE id = 'att-orphan' AND harvested_at IS NULL`).Scan(&unharvested).Error; err != nil {
		t.Fatalf("count orphan: %v", err)
	}
	if unharvested != 1 {
		t.Fatalf("orphan should remain unharvested, got %d", unharvested)
	}
	var logRows int
	if err := db.Raw(`SELECT COUNT(*) FROM harvest_log WHERE attempt_id = 'att-orphan' AND deployment_id = ''`).Scan(&logRows).Error; err != nil {
		t.Fatalf("count log: %v", err)
	}
	if logRows < 1 {
		t.Fatalf("orphan resolution failure missing from harvest_log")
	}
}
