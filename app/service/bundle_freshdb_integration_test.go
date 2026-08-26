//go:build integration

package service

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/provider"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// TestIntegration_BundleFreshDBMigrateLoad pins BLOCKER-1 (v2 review): a
// completely empty database becomes usable through the documented bundleload
// path — migrations run first, then the bundle loads and preflight passes.
func TestIntegration_BundleFreshDBMigrateLoad(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN_FRESH")
	if dsn == "" {
		t.Skip("TEST_DB_DSN_FRESH not set for integration test")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect fresh db: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	// Documented path step 1: run migrations on the empty database.
	if err := provider.RunFrom(sqlDB, "../../migrations"); err != nil {
		t.Fatalf("migrate on fresh db: %v", err)
	}
	// Running twice must be safe.
	if err := provider.RunFrom(sqlDB, "../../migrations"); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	bundleSvc := NewBundleService(repo, txManager, &lifecycleContentSvc{})

	now := time.Now()
	b := inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: "dep-fresh",
		Exam: inbound.ExamNodeBundleExam{
			ID: "exam-fresh", Title: "UTS", StartsAt: now.Add(time.Hour), EndsAt: now.Add(3 * time.Hour),
			DurationMinutes: 90, MaxAttempts: 1, ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{{
			ID: "item-fresh", SectionID: "sec-1", QuestionType: "single_choice",
			PromptSnapshot: "1+1?", Points: 10,
			AnswerKeySnapshotJSON: map[string]interface{}{"answer": "B"},
		}},
		Participants: []inbound.ExamNodeBundleParticipant{{
			ID: "p-fresh-1", StudentID: "s-1", StudentName: "Budi", AccessCode: "BBBBBB-111111",
		}},
	}
	b.Checksum = ComputeBundleChecksum(b)

	// Documented path step 2: load the bundle.
	if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
		t.Fatalf("load on fresh db: %v", err)
	}

	// Step 3: preflight passes.
	if err := bundleSvc.Preflight(context.Background(), "exam-fresh", 1, 1); err != nil {
		t.Fatalf("preflight on fresh db: %v", err)
	}
}

// TestIntegration_PreflightDetectsTampering pins BLOCKER-2: hand-editing a
// prompt, an answer key, or the manual-grading flag must fail preflight.
func TestIntegration_PreflightDetectsTampering(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set for integration test")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	contentSvc := &lifecycleContentSvc{}
	bundleSvc := NewBundleService(repo, txManager, contentSvc)
	ctx := context.Background()

	cleanupIntegrityTables(t, db)
	mustExec(t, db, "DELETE FROM items")
	mustExec(t, db, "DELETE FROM exams")
	mustExec(t, db, "DELETE FROM participants")

	now := time.Now()
	b := inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: "dep-tamper",
		Exam: inbound.ExamNodeBundleExam{
			ID: "exam-tamper", Title: "UTS", StartsAt: now.Add(time.Hour), EndsAt: now.Add(3 * time.Hour),
			DurationMinutes: 90, MaxAttempts: 1, ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{{
			ID: "item-tamper", SectionID: "sec-1", QuestionType: "single_choice",
			PromptSnapshot: "2+2?", Points: 10,
			AnswerKeySnapshotJSON: map[string]interface{}{"answer": "A"},
		}},
		Participants: []inbound.ExamNodeBundleParticipant{{
			ID: "p-tamper", StudentID: "s-9", StudentName: "Budi", AccessCode: "CCCCCC-111111",
		}},
	}
	b.Checksum = ComputeBundleChecksum(b)
	if err := bundleSvc.LoadBundle(ctx, b); err != nil {
		t.Fatalf("load: %v", err)
	}

	tamper := func(field, value string) {
		t.Helper()
		if err := db.Exec("UPDATE items SET "+field+" = ? WHERE id = ?", value, "item-tamper").Error; err != nil {
			t.Fatalf("tamper %s: %v", field, err)
		}
		if err := bundleSvc.Preflight(ctx, "exam-tamper", 1, 1); err == nil {
			t.Fatalf("preflight passed after editing %s — tampering undetected", field)
		}
		// restore so the next tamper case starts from a clean state
		loadAgain(t, bundleSvc, b)
	}

	tamper("prompt_snapshot", "TAMPERED PROMPT")
	tamper("requires_manual_grading", "1")

	// answer key tamper
	if err := db.Exec(`UPDATE items SET answer_key_snapshot_json = JSON_SET(answer_key_snapshot_json, '$.answer', 'C') WHERE id = ?`, "item-tamper").Error; err != nil {
		t.Fatalf("tamper answer key: %v", err)
	}
	if err := bundleSvc.Preflight(ctx, "exam-tamper", 1, 1); err == nil {
		t.Fatal("preflight passed after editing answer key")
	}
}

func loadAgain(t *testing.T, bundleSvc *BundleService, b inbound.ExamNodeBundle) {
	t.Helper()
	if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
		t.Fatalf("reload after tamper: %v", err)
	}
}
