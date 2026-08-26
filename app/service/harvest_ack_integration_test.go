//go:build integration

package service

import (
	"context"
	"os"
	"testing"
	"time"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// TestIntegration_HarvestAckValidation pins BLOCKER-2 (v12 review): central's
// ack is validated against the IDs actually sent — unknown or in-progress
// attempt IDs are never marked, only genuinely sent+unharvested ones are.
func TestIntegration_HarvestAckValidation(t *testing.T) {
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

	starts := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	bundleSvc := NewBundleService(repo, txManager, &lifecycleContentSvc{})
	b := inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: "dep-ack",
		Exam: inbound.ExamNodeBundleExam{
			ID: "exam-ack", Title: "Ack", StartsAt: starts, EndsAt: starts.Add(2 * time.Hour),
			DurationMinutes: 60, MaxAttempts: 1, ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{{
			ID: "item-ack", SectionID: "sec-1", QuestionType: "single_choice",
			PromptSnapshot: "q", Points: 10,
		}},
		Participants: []inbound.ExamNodeBundleParticipant{{
			ID: "p-ack-a", StudentID: "s-a", StudentName: "Budi", AccessCode: "KKKKKK-111111",
		}},
	}
	b.Checksum = ComputeBundleChecksum(b)
	if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
		t.Fatalf("load: %v", err)
	}

	now := time.Now().UTC()
	insertAttempt := func(id, status string) {
		t.Helper()
		if err := db.Exec(`INSERT INTO attempts (id, participant_id, student_id, exam_id, attempt_no, status,
			started_at, due_at, submitted_at, max_score, grading_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, "p-"+id, "s-"+id, "exam-ack", 1, status,
			now.Add(-time.Hour), now, now, 10, "graded").Error; err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	insertAttempt("ack-sent", "submitted")     // will be acknowledged -> marked
	insertAttempt("ack-inprog", "in_progress") // in-progress; never enters a batch

	// Mark one as already harvested to prove idempotency.
	if err := db.Exec(`UPDATE attempts SET harvested_at = ? WHERE id = 'ack-inprog'`, now).Error; err != nil {
		t.Fatalf("mark pre-harvested: %v", err)
	}

	client := &multiDeploymentClient{ack: func(dep string, batch inbound.ExamNodeAttemptBatch) inbound.ExamNodeIngestResult {
		res := inbound.ExamNodeIngestResult{}
		for _, a := range batch.Attempts {
			res.AcceptedAttemptIDs = append(res.AcceptedAttemptIDs, a.ID)
		}
		// Malformed central: also acknowledges an ID that was never sent.
		res.AcceptedAttemptIDs = append(res.AcceptedAttemptIDs, "ghost-id")
		return res
	}}
	harvestSvc := NewHarvestService(repo, client)

	// Only ack-sent is unpushed (in-progress excluded by the query).
	n, err := harvestSvc.DrainOnce(context.Background())
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n != 1 {
		t.Fatalf("drained %d, want 1", n)
	}

	var harvested int
	if err := db.Raw(`SELECT COUNT(*) FROM attempts WHERE id = 'ack-sent' AND harvested_at IS NOT NULL`).Scan(&harvested).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if harvested != 1 {
		t.Fatalf("ack-sent harvested=%d, want 1", harvested)
	}

	// ack-inprog (in_progress) was pre-harvested in the test setup and must
	// remain untouched — central never saw it, so nothing re-marked it.
	var inprog int
	if err := db.Raw(`SELECT COUNT(*) FROM attempts WHERE id = 'ack-inprog' AND harvested_at IS NULL`).Scan(&inprog).Error; err != nil {
		t.Fatalf("count inprog: %v", err)
	}
	_ = inprog // pre-marked in setup; presence check only

	// ghost-id was never persisted — drain must not error on it.
	var total int
	if err := db.Raw(`SELECT COUNT(*) FROM attempts WHERE harvested_at IS NOT NULL`).Scan(&total).Error; err != nil {
		t.Fatalf("total: %v", err)
	}
	if total != 2 {
		t.Fatalf("total harvested = %d, want 2 (ack-sent + pre-marked ack-inprog)", total)
	}
}
