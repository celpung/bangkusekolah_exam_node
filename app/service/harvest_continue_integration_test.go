//go:build integration

package service

import (
	"context"
	"fmt"
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
)

// TestIntegration_HarvestContinuesAfterDeploymentFailure pins BLOCKER-1
// (review v3): a transport error for dep-A must not prevent dep-B from
// draining; DrainOnce returns an aggregated error with the accepted count.
func TestIntegration_HarvestContinuesAfterDeploymentFailure(t *testing.T) {
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

	starts := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	bundleSvc := NewBundleService(repo, txManager, &lifecycleContentSvc{})
	for id, dep := range map[string]string{"exam-fail-a": "dep-A", "exam-ok-b": "dep-B"} {
		b := inbound.ExamNodeBundle{
			BundleVersion: 1, DeploymentID: dep,
			Exam: inbound.ExamNodeBundleExam{
				ID: id, Title: id, StartsAt: starts, EndsAt: starts.Add(2 * time.Hour),
				DurationMinutes: 60, MaxAttempts: 1, ResultSelectionPolicy: "best",
			},
			Items: []inbound.ExamNodeBundleItem{{
				ID: "item-" + id, SectionID: "sec-1", QuestionType: "single_choice",
				PromptSnapshot: "q", Points: 10,
			}},
			Participants: []inbound.ExamNodeBundleParticipant{{
				ID: "p-" + id, StudentID: "s-" + id, StudentName: "Budi", AccessCode: codeFor(id),
			}},
		}
		b.Checksum = ComputeBundleChecksum(b)
		if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
	}

	now := time.Now().UTC()
	for _, att := range []struct{ id, exam string }{
		{"att-fail-a", "exam-fail-a"},
		{"att-ok-b", "exam-ok-b"},
	} {
		if err := db.Exec(`INSERT INTO attempts (id, participant_id, student_id, exam_id, attempt_no, status,
			started_at, due_at, submitted_at, max_score, grading_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			att.id, "p-"+att.id, "s-"+att.id, att.exam, 1, "submitted",
			now.Add(-time.Hour), now, now, 10, "graded").Error; err != nil {
			t.Fatalf("insert %s: %v", att.id, err)
		}
	}

	// dep-A pusher fails; dep-B succeeds.
	client := &splitPusher{
		failDep: "dep-A",
		failErr: fmt.Errorf("connection refused"),
		ack: func(dep string, batch inbound.ExamNodeAttemptBatch) inbound.ExamNodeIngestResult {
			res := inbound.ExamNodeIngestResult{}
			for _, a := range batch.Attempts {
				res.AcceptedAttemptIDs = append(res.AcceptedAttemptIDs, a.ID)
			}
			return res
		},
	}
	harvestSvc := NewHarvestService(repo, client)

	n, drainErr := harvestSvc.DrainOnce(context.Background())
	if n != 1 {
		t.Fatalf("accepted %d, want 1 (only dep-B)", n)
	}
	if drainErr == nil || !strings.Contains(drainErr.Error(), "dep-A") {
		t.Fatalf("expected aggregated error mentioning dep-A, got %v", drainErr)
	}

	// dep-B attempt is harvested; dep-A attempt remains unharvested.
	var bMarked, aUnharvested int
	if err := db.Raw(`SELECT COUNT(*) FROM attempts WHERE id = 'att-ok-b' AND harvested_at IS NOT NULL`).Scan(&bMarked).Error; err != nil {
		t.Fatalf("scan bMarked: %v", err)
	}
	if err := db.Raw(`SELECT COUNT(*) FROM attempts WHERE id = 'att-fail-a' AND harvested_at IS NULL`).Scan(&aUnharvested).Error; err != nil {
		t.Fatalf("scan aUnharvested: %v", err)
	}
	if bMarked != 1 {
		t.Fatalf("dep-B harvested=%d, want 1", bMarked)
	}
	if aUnharvested != 1 {
		t.Fatalf("dep-A unharvested=%d, want 1", aUnharvested)
	}
}

// splitPusher fails for one specific deployment and delegates the rest.
type splitPusher struct {
	failDep string
	failErr error
	ack     func(string, inbound.ExamNodeAttemptBatch) inbound.ExamNodeIngestResult
}

func (s *splitPusher) Push(_ context.Context, deploymentID string, batch inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	if deploymentID == s.failDep {
		return nil, s.failErr
	}
	res := s.ack(deploymentID, batch)
	return &res, nil
}
