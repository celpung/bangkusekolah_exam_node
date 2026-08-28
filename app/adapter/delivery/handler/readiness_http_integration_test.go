//go:build integration

package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/handler"
	node_router "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/router"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	node_security "github.com/celpung/bangkusekolah_exam_node/app/adapter/security"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

// readinessIDGen satisfies the ID generator port for the attempt/integrity
// services; this test never creates attempts.
type readinessIDGen struct{}

func (*readinessIDGen) NewID() string { return "00000000-0000-0000-0000-000000000000" }

const readinessTestToken = "integration-internal-token-32ch!!"

// TestIntegration_ReadyzAndContentRecoverAfterUnready pins the HTTP contract:
// while an exam's rebuild has failed, /readyz is 503 and the content endpoint
// is 503 WITHOUT leaking the stale snapshot; after a successful retry both
// return 200 and the content reflects the reloaded rows. This drives the real
// router (auth middleware + handlers), not internal service state.
func TestIntegration_ReadyzAndContentRecoverAfterUnready(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set for integration test")
	}
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })

	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	contentSvc := service.NewContentService(repo)
	attemptSvc := service.NewAttemptService(repo, txManager, &readinessIDGen{})
	integritySvc := service.NewIntegrityService(repo, txManager, &readinessIDGen{})
	bundleSvc := service.NewBundleService(repo, txManager, contentSvc)

	cfg := &config.Config{JWTSecret: "integration-jwt-secret-32-characters!", JWTTTL: 90 * time.Minute}
	internalH := handler.NewInternalHandler(bundleSvc)
	harvestH := handler.NewHarvestHandler(service.NewHarvestService(repo, testPusherOK{}))

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
	r.Mount("/", node_router.NewRouter(node_security.NewJWTIssuer(cfg), readinessTestToken, contentSvc, attemptSvc, integritySvc, internalH, harvestH, readiness))

	get := func(path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	cleanupReadinessTables(t, db)

	// 1. Load bundle v1 through the real service.
	v1 := readinessBundle("v1 prompt")
	v1.Checksum = service.ComputeBundleChecksum(v1)
	if err := bundleSvc.LoadBundle(context.Background(), v1); err != nil {
		t.Fatalf("load v1: %v", err)
	}

	// Mint a student token via the real issuer.
	issuer := node_security.NewJWTIssuer(cfg)
	token, err := issuer.Issue(context.Background(), "p-ready", "s-ready", "exam-ready", "dep-ready")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	// Content v1 served with 200.
	if w := get("/api/v1/student/exams/exam-ready/content", token); w.Code != http.StatusOK {
		t.Fatalf("v1 content status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// 2-4. Simulate a DB-committed v2 whose cache publication fails.
	mustExec(t, db, "DELETE FROM exams WHERE id = 'exam-ready'")
	if err := contentSvc.RebuildExam(context.Background(), "exam-ready"); err == nil {
		t.Fatal("expected rebuild failure after exam deletion")
	}

	// 5. Unready recorded.
	if _, ok := contentSvc.UnreadyExams()["exam-ready"]; !ok {
		t.Fatal("exam not marked unready after failed rebuild")
	}

	// 6. /readyz must be 503.
	if w := get("/readyz", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz during unready = %d, want 503", w.Code)
	}

	// 7. Content must be 503 and never contain the stale v1 snapshot.
	w := get("/api/v1/student/exams/exam-ready/content", token)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("content during unready = %d, want 503; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "v1 prompt") {
		t.Fatal("stale v1 content leaked in unready response")
	}

	// 8. Retry: restore rows and rebuild through the service.
	loadReadiness(t, bundleSvc, v1)

	// 9. /readyz back to 200.
	if w := get("/readyz", ""); w.Code != http.StatusOK {
		t.Fatalf("/readyz after recovery = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// 10. Content back to 200.
	if w := get("/api/v1/student/exams/exam-ready/content", token); w.Code != http.StatusOK {
		t.Fatalf("content after recovery = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func readinessBundle(prompt string) inbound.ExamNodeBundle {
	starts := time.Now().Add(time.Hour)
	return inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: "dep-readiness",
		Exam: inbound.ExamNodeBundleExam{
			ID: "exam-ready", Title: "UTS", StartsAt: starts, EndsAt: starts.Add(3 * time.Hour),
			DurationMinutes: 90, MaxAttempts: 1, ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{{
			ID: "item-ready", SectionID: "sec-1", QuestionType: "single_choice",
			PromptSnapshot: prompt, Points: 10,
			AnswerKeySnapshotJSON: map[string]interface{}{"answer": "A"},
		}},
		Participants: []inbound.ExamNodeBundleParticipant{{
			ID: "p-ready", StudentID: "s-ready", StudentName: "Budi", AccessCode: "RRRRRR-111111",
		}},
	}
}

func loadReadiness(t *testing.T, bundleSvc *service.BundleService, b inbound.ExamNodeBundle) {
	t.Helper()
	if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
		t.Fatalf("reload: %v", err)
	}
}

func cleanupReadinessTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, q := range []string{
		"DELETE FROM integrity_events", "DELETE FROM attempts",
		"DELETE FROM items", "DELETE FROM exams", "DELETE FROM participants",
	} {
		if _, err := db.DB(); err != nil {
			t.Fatalf("db handle: %v", err)
		}
		if err := db.Exec(q).Error; err != nil {
			t.Fatalf("cleanup %q: %v", q, err)
		}
	}
}

func mustExec(t *testing.T, db *gorm.DB, q string) {
	t.Helper()
	if err := db.Exec(q).Error; err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// testPusherOK is a harvest pusher that never fails; readiness tests never
// exercise the push path.
type testPusherOK struct{}

func (testPusherOK) Push(_ context.Context, _ string, _ inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	return &inbound.ExamNodeIngestResult{}, nil
}
