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

// TestIntegration_LoadBundleRollsBackOnFailure pins BLOCKER-1 (v7 review):
// a bundle v2 whose write fails mid-way must roll back completely — exam,
// items, participants all remain exactly v1 and no v2 rows leak through.
func TestIntegration_LoadBundleRollsBackOnFailure(t *testing.T) {
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
	v1 := inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: "dep-rollback",
		Exam: inbound.ExamNodeBundleExam{
			ID: "exam-roll", Title: "v1", StartsAt: starts, EndsAt: starts.Add(2 * time.Hour),
			DurationMinutes: 60, MaxAttempts: 1, ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{{
			ID: "item-roll-v1", SectionID: "sec-1", QuestionType: "single_choice",
			PromptSnapshot: "v1 prompt", Points: 10,
			AnswerKeySnapshotJSON: map[string]interface{}{"answer": "A"},
		}},
		Participants: []inbound.ExamNodeBundleParticipant{
			{ID: "p-r1", StudentID: "s-1", StudentName: "Budi", AccessCode: "BBBBBB-111111"},
		},
	}
	v1.Checksum = ComputeBundleChecksum(v1)
	if err := bundleSvc.LoadBundle(ctx, v1); err != nil {
		t.Fatalf("load v1: %v", err)
	}

	// v2 collides with an access code owned by a DIFFERENT exam's roster —
	// the reconcile insert blows up mid-transaction, after the old
	// exam/items of exam-roll were already deleted.
	other := v1
	other.Exam.ID = "exam-other"
	other.Items = []inbound.ExamNodeBundleItem{{
		ID: "item-other", SectionID: "sec-1", QuestionType: "single_choice",
		PromptSnapshot: "other prompt", Points: 5,
	}}
	other.Participants = []inbound.ExamNodeBundleParticipant{
		{ID: "p-oth", StudentID: "s-8", StudentName: "Other", AccessCode: "CCCCCCC-999999"},
	}
	other.Checksum = ComputeBundleChecksum(other)
	if err := bundleSvc.LoadBundle(ctx, other); err != nil {
		t.Fatalf("load other exam: %v", err)
	}

	v2 := v1
	v2.Exam.Title = "v2"
	v2.Items = []inbound.ExamNodeBundleItem{{
		ID: "item-roll-v2", SectionID: "sec-1", QuestionType: "single_choice",
		PromptSnapshot: "v2 prompt", Points: 20,
	}}
	v2.Participants = []inbound.ExamNodeBundleParticipant{
		{ID: "p-r1", StudentID: "s-1", StudentName: "Budi", AccessCode: "BBBBBB-111111"},
		{ID: "p-new2", StudentID: "s-9", StudentName: "New", AccessCode: "CCCCCCC-999999"}, // owned by exam-other
	}
	v2.Checksum = ComputeBundleChecksum(v2)
	if err := bundleSvc.LoadBundle(ctx, v2); err == nil {
		t.Fatal("expected v2 load to fail on duplicate access code")
	}

	// v1 remains fully intact.
	exam, err := repo.FindExamByID(ctx, "exam-roll")
	if err != nil || exam == nil {
		t.Fatalf("exam v1 missing after failed v2 load: err=%v exam=%v", err, exam)
	}
	if exam.Title != "v1" {
		t.Errorf("exam title = %q, want v1 (rollback failed)", exam.Title)
	}
	items, err := repo.ListItemsByExamID(ctx, "exam-roll")
	if err != nil || len(items) != 1 {
		t.Fatalf("items after rollback: count=%d err=%v, want 1 v1 item", len(items), err)
	}
	if items[0].PromptSnapshot != "v1 prompt" {
		t.Errorf("item prompt = %q, want v1 prompt", items[0].PromptSnapshot)
	}
	parts, err := repo.ListParticipantsByExam(ctx, "exam-roll")
	if err != nil || len(parts) != 1 {
		t.Fatalf("participants after rollback: count=%d err=%v, want 1", len(parts), err)
	}
	if parts[0].StudentName != "Budi" {
		t.Errorf("participant name = %q, want Budi", parts[0].StudentName)
	}

	// And a retry of a valid v2 succeeds cleanly.
	v2.Participants = []inbound.ExamNodeBundleParticipant{
		{ID: "p-r1", StudentID: "s-1", StudentName: "Budi", AccessCode: "BBBBBB-111111"},
	}
	v2.Checksum = ComputeBundleChecksum(v2)
	if err := bundleSvc.LoadBundle(ctx, v2); err != nil {
		t.Fatalf("retry valid v2: %v", err)
	}
	exam, _ = repo.FindExamByID(ctx, "exam-roll")
	if exam.Title != "v2" {
		t.Errorf("after retry exam title = %q, want v2", exam.Title)
	}
}
