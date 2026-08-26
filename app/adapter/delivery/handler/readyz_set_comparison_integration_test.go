//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	node_router "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/router"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

func bundleForID(id string, starts time.Time) inbound.ExamNodeBundle {
	return inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: "dep-set",
		Exam: inbound.ExamNodeBundleExam{
			ID: id, Title: id, StartsAt: starts, EndsAt: starts.Add(2 * time.Hour),
			DurationMinutes: 60, MaxAttempts: 1, ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{{
			ID: "item-" + id, SectionID: "sec-1", QuestionType: "single_choice",
			PromptSnapshot: "prompt " + id, Points: 10,
			AnswerKeySnapshotJSON: map[string]interface{}{"answer": "A"},
		}},
		Participants: []inbound.ExamNodeBundleParticipant{{
			ID: "p-" + id, StudentID: "s-" + id, StudentName: "Budi", AccessCode: codeFor(id),
		}},
	}
}

var setCodes = map[string]string{
	"set-exam-a": "SSSSSS-111111",
	"set-exam-b": "SSSSSS-222222",
	"set-exam-c": "SSSSSS-333333",
}

func codeFor(id string) string {
	if c, ok := setCodes[id]; ok {
		return c
	}
	return "SSSSSS-999999"
}

// TestIntegration_ReadyzExactSetComparison pins BLOCKER (v9 review): /readyz
// must compare the exact persisted exam ID set with the cache-ready set.
//
//	exam-a cached, exam-b not cached -> 503
//	exam-b unready                   -> 503
//	exam-a + exam-b cached           -> 200
//	cache holds stale deleted ID     -> 503
func TestIntegration_ReadyzExactSetComparison(t *testing.T) {
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
	bundleSvc := service.NewBundleService(repo, txManager, contentSvc)

	r := chi.NewRouter()
	readiness := node_router.NewReadinessRouter(contentSvc,
		func() ([]string, error) {
			exams, err := repo.ListExams(context.Background())
			if err != nil {
				return nil, err
			}
			ids := make([]string, len(exams))
			for i, e := range exams {
				ids[i] = e.ID
			}
			return ids, nil
		},
		func() error { return db.Error })
	r.Mount("/", readiness)

	getReadyz := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return w.Code
	}
	loadExam := func(id string) {
		t.Helper()
		starts := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
		b := bundleForID(id, starts)
		b.Checksum = service.ComputeBundleChecksum(b)
		if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
	}

	cleanupReadinessTables(t, db)

	// Persist both exams via the repository directly for exam-b (no cache
	// entry): simulates a DB-committed bundle whose rebuild never happened.
	loadExam("set-exam-a")
	starts := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	bB := bundleForID("set-exam-b", starts)
	bB.Checksum = service.ComputeBundleChecksum(bB)
	if err := contentSvc.RebuildExam(context.Background(), "set-exam-a"); err != nil {
		t.Fatalf("rebuild a: %v", err)
	}
	if err := bundleSvc.LoadBundle(context.Background(), bB); err != nil {
		t.Fatalf("load b: %v", err)
	}
	// Drop exam-b's cache entry so its persisted rows exist with no cache:
	contentSvc.DropFromCache("set-exam-b")

	// Case 1: exam-a cached, exam-b persisted but never cached -> 503.
	if code := getReadyz(); code != http.StatusServiceUnavailable {
		t.Fatalf("partial cache: /readyz = %d, want 503", code)
	}

	// Case 2: force exam-b's rebuild to fail -> unready -> still 503.
	mustExec(t, db, "DELETE FROM exams WHERE id = 'set-exam-b'")
	if err := contentSvc.RebuildExam(context.Background(), "set-exam-b"); err == nil {
		t.Fatal("expected exam-b rebuild failure after row deletion")
	}
	if _, ok := contentSvc.UnreadyExams()["set-exam-b"]; !ok {
		t.Fatal("exam-b should be in the unready map")
	}
	if code := getReadyz(); code != http.StatusServiceUnavailable {
		t.Fatalf("unready present: /readyz = %d, want 503", code)
	}

	// Case 3: restore exam-b and cache BOTH exams -> 200.
	loadExam("set-exam-b")
	if err := contentSvc.RebuildExam(context.Background(), "set-exam-a"); err != nil {
		t.Fatalf("re-rebuild a: %v", err)
	}
	if err := contentSvc.RebuildExam(context.Background(), "set-exam-b"); err != nil {
		t.Fatalf("re-rebuild b: %v", err)
	}
	if code := getReadyz(); code != http.StatusOK {
		t.Fatalf("both cached: /readyz = %d, want 200", code)
	}

	// Case 4: stale deleted ID — delete exam-b's rows; cached set now holds
	// an ID that is no longer persisted => sets differ => 503.
	mustExec(t, db, "DELETE FROM exams WHERE id = 'set-exam-b'")
	mustExec(t, db, "DELETE FROM items WHERE exam_id = 'set-exam-b'")
	mustExec(t, db, "DELETE FROM participants WHERE exam_id = 'set-exam-b'")
	if code := getReadyz(); code != http.StatusServiceUnavailable {
		t.Fatalf("stale cache entry: /readyz = %d, want 503", code)
	}
}
