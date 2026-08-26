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
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

// TestIntegration_NewExamRollbackDoesNotTakeNodeOffline pins BLOCKER (v12):
//
//  1. Load and publish exam-a.
//  2. Attempt new exam-b; force its transaction to roll back.
//  3. exam-b absent from DB/cache.
//  4. exam-a content remains available.
//  5. /readyz remains 200 for exam-a.
//  6. Retry valid exam-b -> /readyz 200 with both exams.
func TestIntegration_NewExamRollbackDoesNotTakeNodeOffline(t *testing.T) {
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

	// 1. Load and publish exam-a.
	a := sameExamBundle("exam-keep", "dep-new", "keep prompt", starts)
	a.Checksum = service.ComputeBundleChecksum(a)
	if err := loadViaRepo(t, repo, txManager, contentSvc, a); err != nil {
		t.Fatalf("load a: %v", err)
	}

	// Seed an unrelated exam whose access code will collide with exam-b's.
	other := sameExamBundle("exam-other", "dep-new", "other prompt", starts)
	other.Participants[0].AccessCode = "OOOOOO-777777"
	other.Checksum = service.ComputeBundleChecksum(other)
	if err := loadViaRepo(t, repo, txManager, contentSvc, other); err != nil {
		t.Fatalf("load other: %v", err)
	}

	// 2. New exam-b whose roster collides with exam-other's code -> rollback.
	b := sameExamBundle("exam-new-b", "dep-new", "b prompt", starts)
	b.Participants[0].AccessCode = "OOOOOO-777777"
	b.Checksum = service.ComputeBundleChecksum(b)
	if err := loadViaRepo(t, repo, txManager, contentSvc, b); err == nil {
		t.Fatal("expected new exam-b load to fail (rollback)")
	}

	// 3. exam-b absent from DB and cache.
	exams, _ := repo.ListExams(context.Background())
	for _, e := range exams {
		if e.ID == "exam-new-b" {
			t.Fatal("rolled-back new exam must not be persisted")
		}
	}
	if _, ok := contentSvc.UnreadyExams()["exam-new-b"]; ok {
		t.Fatal("rolled-back NEW exam left unready marker — node taken offline")
	}

	// 4. exam-a content still available.
	content, _, _, _, cerr := contentSvc.GetExamContent(context.Background(), "exam-keep")
	if cerr != nil || content == nil {
		t.Fatalf("exam-a content unavailable after b rollback: %v", cerr)
	}

	// Build readiness router AFTER the failed push to assert the marker is gone.
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

	// 5. /readyz remains 200 for the healthy exams.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/readyz = %d after new-exam rollback, want 200; body=%s", w.Code, w.Body.String())
	}

	// 6. Retry a VALID exam-b load -> both exams ready.
	b2 := sameExamBundle("exam-new-b", "dep-new", "b prompt", starts)
	b2.Participants[0].AccessCode = "NNNNNN-222222"
	b2.Checksum = service.ComputeBundleChecksum(b2)
	if err := loadViaRepo(t, repo, txManager, contentSvc, b2); err != nil {
		t.Fatalf("retry exam-b: %v", err)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/readyz after valid retry = %d, want 200", w.Code)
	}
}
