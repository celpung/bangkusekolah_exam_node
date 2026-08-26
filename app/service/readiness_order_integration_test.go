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

// TestIntegration_CanonicalOrderPreflightPasses pins BLOCKER-2 (v5 review):
// a valid bundle whose participants arrive in NON-alphabetical order and
// whose items share sort values must still pass preflight — canonical
// sorting by stable keys makes load-time and read-back hashes identical.
func TestIntegration_CanonicalOrderPreflightPasses(t *testing.T) {
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
	bundleSvc := NewBundleService(repo, txManager, &lifecycleContentSvc{})
	ctx := context.Background()

	cleanupIntegrityTables(t, db)
	mustExec(t, db, "DELETE FROM items")
	mustExec(t, db, "DELETE FROM exams")
	mustExec(t, db, "DELETE FROM participants")

	starts := time.Now().Add(time.Hour)
	b := inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: "dep-order",
		Exam: inbound.ExamNodeBundleExam{
			ID: "exam-order", Title: "UTS", StartsAt: starts, EndsAt: starts.Add(3 * time.Hour),
			DurationMinutes: 90, MaxAttempts: 1, ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{
			{ID: "item-zeta", SectionID: "sec-1", QuestionType: "single_choice", PromptSnapshot: "q-z", Points: 5},
			{ID: "item-alpha", SectionID: "sec-2", QuestionType: "single_choice", PromptSnapshot: "q-a", Points: 5},
			{ID: "item-mike", SectionID: "sec-1", QuestionType: "essay", PromptSnapshot: "q-m", Points: 10, RequiresManualGrading: true},
		},
		// Deliberately NOT alphabetical by name or ID.
		Participants: []inbound.ExamNodeBundleParticipant{
			{ID: "p-ccc", StudentID: "s-3", StudentName: "Zaki", AccessCode: "GGGGGG-333333"},
			{ID: "p-aaa", StudentID: "s-1", StudentName: "Budi", AccessCode: "GGGGGG-111111"},
			{ID: "p-bbb", StudentID: "s-2", StudentName: "Ani", AccessCode: "GGGGGG-222222"},
		},
	}
	b.Checksum = ComputeBundleChecksum(b)
	if err := bundleSvc.LoadBundle(ctx, b); err != nil {
		t.Fatalf("load with reversed order: %v", err)
	}
	if err := bundleSvc.Preflight(ctx, "exam-order", 3, 3); err != nil {
		t.Fatalf("preflight failed on untampered reversed-order bundle: %v", err)
	}
}

// TestIntegration_UnreadyExamBlocksReadiness pins BLOCKER-1 (v5 review):
// a post-commit rebuild failure marks the exam unready; a successful retry
// clears it. /readyz turns UnreadyExams into a 503 until then.
func TestIntegration_UnreadyExamBlocksAndRecovers(t *testing.T) {
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
	contentSvc := NewContentService(repo)
	bundleSvc := NewBundleService(repo, txManager, contentSvc)
	ctx := context.Background()

	cleanupIntegrityTables(t, db)
	mustExec(t, db, "DELETE FROM items")
	mustExec(t, db, "DELETE FROM exams")
	mustExec(t, db, "DELETE FROM participants")

	b := validBundle("exam-unready", nowPtr())
	b.Checksum = ComputeBundleChecksum(b)
	if err := bundleSvc.LoadBundle(ctx, b); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Simulate a live-push rebuild failure: drop the exam rows out from under
	// the cache (DB committed new state, cache publication would fail).
	mustExec(t, db, "DELETE FROM exams WHERE id = 'exam-unready'")
	if err := contentSvc.RebuildExam(ctx, "exam-unready"); err == nil {
		t.Fatal("expected rebuild failure after exam row deletion")
	}
	unready := contentSvc.UnreadyExams()
	if _, ok := unready["exam-unready"]; !ok {
		t.Fatalf("exam should be marked unready after failed rebuild, got %v", unready)
	}

	// Recovery: restore the rows (retry path) and rebuild — unready clears.
	load(t, bundleSvc, b)
	if err := contentSvc.RebuildExam(ctx, "exam-unready"); err != nil {
		t.Fatalf("recovery rebuild: %v", err)
	}
	if remaining := contentSvc.UnreadyExams(); len(remaining) != 0 {
		t.Fatalf("unready should be empty after successful retry, got %v", remaining)
	}
}
