//go:build integration

package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// TestIntegration_BundleLoadReplaceLifecycle pins BLOCKER-2 end to end on
// real MySQL: load once, load the same bundle again (idempotent), then load a
// changed roster and assert stale participants are reconciled away.
func TestIntegration_BundleLoadReplaceLifecycle(t *testing.T) {
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

	load := func(participants []inbound.ExamNodeBundleParticipant) {
		now := time.Now()
		b := inbound.ExamNodeBundle{
			BundleVersion: 1, DeploymentID: "dep-life",
			Exam: inbound.ExamNodeBundleExam{
				ID: "exam-life", Title: "UTS", StartsAt: now.Add(time.Hour), EndsAt: now.Add(3 * time.Hour),
				DurationMinutes: 90, MaxAttempts: 1, ResultSelectionPolicy: "best",
			},
			Items:        []inbound.ExamNodeBundleItem{{ID: "item-life", SectionID: "sec-1", QuestionType: "single_choice", PromptSnapshot: "1+1?", Points: 10}},
			Participants: participants,
		}
		b.Checksum = ComputeBundleChecksum(b)
		if err := bundleSvc.LoadBundle(ctx, b); err != nil {
			t.Fatalf("load: %v", err)
		}
	}

	rosterA := []inbound.ExamNodeBundleParticipant{
		{ID: "p-life-a1", StudentID: "s-1", StudentName: "Budi", AccessCode: "AAAAAA-111111"},
		{ID: "p-life-a2", StudentID: "s-2", StudentName: "Ani", AccessCode: "AAAAAA-222222"},
	}

	// 1. load once; 2. load the same bundle again — must not duplicate
	load(rosterA)
	load(rosterA)
	parts, err := repo.ListParticipantsByExam(ctx, "exam-life")
	if err != nil {
		t.Fatalf("list after reload: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("reload duplicated participants: %d rows, want 2", len(parts))
	}

	// 3. changed roster: drop a2, add a3
	rosterB := []inbound.ExamNodeBundleParticipant{
		{ID: "p-life-a1", StudentID: "s-1", StudentName: "Budi", AccessCode: "AAAAAA-111111"},
		{ID: "p-life-a3", StudentID: "s-3", StudentName: "Cici", AccessCode: "AAAAAA-333333"},
	}
	load(rosterB)
	parts, err = repo.ListParticipantsByExam(ctx, "exam-life")
	if err != nil {
		t.Fatalf("list after change: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("changed roster: %d rows, want 2", len(parts))
	}
	found := map[string]bool{}
	for _, p := range parts {
		found[p.ID] = true
	}
	if found["p-life-a2"] || !found["p-life-a3"] || !found["p-life-a1"] {
		t.Fatalf("roster reconcile wrong: %+v", parts)
	}

	// 4. preflight passes with matching counts
	if err := bundleSvc.Preflight(ctx, "exam-life", 1, 2); err != nil {
		dbItems, _ := repo.ListItemsByExamID(ctx, "exam-life")
		dbParts, _ := repo.ListParticipantsByExam(ctx, "exam-life")
		dbExam, _ := repo.FindExamByID(ctx, "exam-life")
		lj, _ := json.Marshal(contentHashView(dbItems, dbParts, dbExam))
		t.Logf("DEBUG stored view: %s", lj)
		t.Fatalf("preflight after lifecycle: %v", err)
	}
	if !contentSvc.rebuiltExams["exam-life"] {
		t.Fatal("content cache was not rebuilt for exam-life")
	}
}

func mustExec(t *testing.T, db *gorm.DB, q string) {
	t.Helper()
	if err := db.Exec(q).Error; err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

type lifecycleContentSvc struct{ rebuiltExams map[string]bool }

func (f *lifecycleContentSvc) RebuildExam(_ context.Context, examID string, _ ...uint64) error {
	if f.rebuiltExams == nil {
		f.rebuiltExams = map[string]bool{}
	}
	f.rebuiltExams[examID] = true
	return nil
}
func (f *lifecycleContentSvc) BeginRebuild(string) uint64        { return 0 }
func (f *lifecycleContentSvc) CancelRebuild(string, uint64) bool { return false }
func (f *lifecycleContentSvc) LockExam(string) func()        { return func() {} }
