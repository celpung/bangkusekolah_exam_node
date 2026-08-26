//go:build integration

package handler_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

// TestIntegration_RollbackKeepsPreExistingUnready pins acceptance 1-4
// (v11 review): when an exam is ALREADY unready (v2 rebuild failed) and a
// v3 load rolls back, the exam must REMAIN unready for v2 — v1 cache must
// not be resurrected as ready.
func TestIntegration_RollbackKeepsPreExistingUnready(t *testing.T) {
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
	contentSvc := service.NewContentService(repo)

	cleanupReadinessTables(t, db)
	starts := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	v1 := sameExamBundle("exam-gen", "dep-gen", "v1 prompt", starts)
	v1.Checksum = service.ComputeBundleChecksum(v1)
	if err := loadViaRepo(t, repo, txManager, contentSvc, v1); err != nil {
		t.Fatalf("load v1: %v", err)
	}

	// 1-2. v2 committed but its rebuild fails -> exam unready for v2.
	v2 := sameExamBundle("exam-gen", "dep-gen", "v2 prompt", starts)
	v2.Checksum = service.ComputeBundleChecksum(v2)
	contentSvc.BeginRebuild("exam-gen")
	if err := replaceViaRepo(t, repo, txManager, v2); err != nil {
		t.Fatalf("commit v2: %v", err)
	}
	mustExec(t, db, "DELETE FROM exams WHERE id = 'exam-gen'")
	if err := contentSvc.RebuildExam(context.Background(), "exam-gen"); err == nil {
		t.Fatal("expected v2 rebuild failure")
	}
	if _, ok := contentSvc.UnreadyExams()["exam-gen"]; !ok {
		t.Fatal("pre-condition failed: exam should be unready after failed v2 rebuild")
	}

	// 3. v3 load attempt whose transaction ROLLS BACK (duplicate access code
	// against exam-other). Its CancelRebuild must NOT clear v2's unready
	// state just because it rolled back.
	other := sameExamBundle("exam-rollb", "dep-gen", "other prompt", starts)
	other.Participants[0].AccessCode = "TTTTTT-888888"
	other.Checksum = service.ComputeBundleChecksum(other)
	if err := loadViaRepo(t, repo, txManager, contentSvc, other); err != nil {
		t.Fatalf("load other: %v", err)
	}
	v3 := sameExamBundle("exam-gen", "dep-gen", "v3 prompt", starts)
	// force rollback: new roster member collides with exam-rollb's code
	v3.Participants = append(v3.Participants, inbound.ExamNodeBundleParticipant{
		ID: "p-v3-extra", StudentID: "s-x", StudentName: "Extra", AccessCode: "TTTTTT-888888",
	})
	v3.Checksum = service.ComputeBundleChecksum(v3)
	if err := loadViaRepo(t, repo, txManager, contentSvc, v3); err == nil {
		t.Fatal("expected v3 load to fail on duplicate access code")
	}

	// 4. Exam must STILL be unready; stale v1 content must not be served.
	if _, ok := contentSvc.UnreadyExams()["exam-gen"]; !ok {
		t.Fatal("unready state lost after rolled-back v3 — stale v1 could be served")
	}
	if _, _, _, _, err := contentSvc.GetExamContent(context.Background(), "exam-gen"); err == nil {
		t.Fatal("content served although DB holds v2 and cache is stale")
	}
}

// TestIntegration_ConcurrentSameExamLoads pins acceptance 5-8 (v11 review):
// concurrent loads of distinct versions for one exam must serialize so the
// final cache matches the final DB version and readiness stays closed until
// the winner publishes.
func TestIntegration_ConcurrentSameExamLoads(t *testing.T) {
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
	contentSvc := service.NewContentService(repo)

	cleanupReadinessTables(t, db)
	starts := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	base := sameExamBundle("exam-race", "dep-race", "base prompt", starts)
	base.Checksum = service.ComputeBundleChecksum(base)
	if err := loadViaRepo(t, repo, txManager, contentSvc, base); err != nil {
		t.Fatalf("load base: %v", err)
	}

	var wg sync.WaitGroup
	prompts := []string{"race-A", "race-B", "race-C"}
	for i, p := range prompts {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			b := sameExamBundle("exam-race", "dep-race", p, starts)
			b.Participants[0].AccessCode = codeFor("set-exam-a") // shared roster slot
			_ = b.Participants[0]
			b.Participants[0].ID = "p-race-" + p[len(p)-1:]
			b.Checksum = service.ComputeBundleChecksum(b)
			svc := service.NewBundleService(repo, txManager, contentSvc)
			if err := svc.LoadBundle(context.Background(), b); err != nil {
				t.Errorf("concurrent load %d: %v", i, err)
			}
		}(i, p)
	}
	wg.Wait()

	// Final DB version and final cache version must MATCH.
	exam, err := repo.FindExamByID(context.Background(), "exam-race")
	if err != nil || exam == nil {
		t.Fatalf("find exam after race: %v", err)
	}
	content, _, _, _, cerr := contentSvc.GetExamContent(context.Background(), "exam-race")
	if cerr != nil {
		t.Fatalf("content after race: %v", cerr)
	}
	dbPrompt := ""
	items, _ := repo.ListItemsByExamID(context.Background(), "exam-race")
	if len(items) > 0 {
		dbPrompt = items[0].PromptSnapshot
	}
	cachePrompt := ""
	for _, it := range content.Items {
		if it.Prompt == dbPrompt {
			cachePrompt = it.Prompt
		}
	}
	if cachePrompt == "" || cachePrompt != dbPrompt {
		t.Fatalf("cache version (%q) does not match DB version (%q)", cachePrompt, dbPrompt)
	}
}
