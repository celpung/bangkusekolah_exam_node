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

func nowPtr() func() time.Time { return time.Now }

func timeHour() time.Duration { return time.Hour }

// TestIntegration_RestartRehydratesCache pins BLOCKER-1 (v4 review): a fresh
// ContentService over an already-loaded database (the "restarted process")
// must be able to rebuild every persisted exam's cache without another
// bundle push — the exact sequence cmd/examnode runs at startup.
func TestIntegration_RestartRehydratesCache(t *testing.T) {
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

	// "First process": load the bundle.
	firstContentSvc := NewContentService(repo)
	bundleSvc := NewBundleService(repo, txManager, firstContentSvc)
	b := validBundle("exam-restart", nowPtr())
	b.Checksum = ComputeBundleChecksum(b)
	if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
		t.Fatalf("load: %v", err)
	}

	// "Restart": brand-new empty cache over the same DB — no LoadBundle call.
	restartedContentSvc := NewContentService(repo)
	if _, _, _, _, err := restartedContentSvc.GetExamContent(context.Background(), "exam-restart"); err == nil {
		t.Fatal("expected cold cache after restart, got content")
	}

	// Startup rehydrate path — the REAL function cmd/examnode calls.
	RehydrateAllCaches(context.Background(), repo, restartedContentSvc)

	content, etag, gzipped, raw, err := restartedContentSvc.GetExamContent(context.Background(), "exam-restart")
	if err != nil {
		t.Fatalf("content after rehydrate: %v", err)
	}
	if content == nil || content.Exam.ID != "exam-restart" || etag == "" || len(gzipped) == 0 || len(raw) == 0 {
		t.Fatalf("rehydrated cache incomplete: hasContent=%v etag=%q gzip=%d raw=%d", content != nil, etag, len(gzipped), len(raw))
	}
}

// TestIntegration_RebuildFailureDetectable pins the fail-start contract:
// when rows are broken so RebuildExam errors, startup must surface it (the
// CLI does log.Fatalf) rather than serving an unusable node.
func TestIntegration_RebuildFailureDetectable(t *testing.T) {
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

	b := validBundle("exam-broken", nowPtr())
	b.Checksum = ComputeBundleChecksum(b)
	bundleSvc := NewBundleService(repo, txManager, &lifecycleContentSvc{})
	if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Corrupt the bundle row: with the exam gone, rebuild fails and the
	// startup loop would log.Fatalf.
	mustExec(t, db, "DELETE FROM exams WHERE id = 'exam-broken'")

	contentSvc := NewContentService(repo)
	if err := contentSvc.RebuildExam(context.Background(), "exam-broken"); err == nil {
		t.Fatal("rebuild of exam with missing exam row must fail (startup aborts)")
	}
}

func validBundle(examID string, now func() time.Time) inbound.ExamNodeBundle {
	starts := now()
	return inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: "dep-restart",
		Exam: inbound.ExamNodeBundleExam{
			ID: examID, Title: "UTS", StartsAt: starts.Add(time.Hour), EndsAt: starts.Add(3 * time.Hour),
			DurationMinutes: 90, MaxAttempts: 1, ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{{
			ID: "item-" + examID, SectionID: "sec-1", QuestionType: "single_choice",
			PromptSnapshot: "2+2?", Points: 10,
			AnswerKeySnapshotJSON: map[string]interface{}{"answer": "A"},
		}},
		Participants: []inbound.ExamNodeBundleParticipant{{
			ID: "p-" + examID, StudentID: "s-7", StudentName: "Budi", AccessCode: "FFFFFF-111111",
		}},
	}
}
