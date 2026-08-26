//go:build integration

package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

// TestIntegration_InternalRouteSequentialSameExamUpdates pins HIGH-3 (v11):
// two sequential same-exam bundle pushes over the real node-token internal
// route must leave DB and cache in agreement, with readiness 200.
func TestIntegration_InternalRouteSequentialSameExamUpdates(t *testing.T) {
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
	attemptSvc := service.NewAttemptService(repo, txManager, &readinessIDGen{})
	integritySvc := service.NewIntegrityService(repo, txManager, &readinessIDGen{})

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

	postBundle := func(b inbound.ExamNodeBundle) int {
		body := bundleJSON(t, b)
		req := httptest.NewRequest(http.MethodPost, "/internal/v1/bundle", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+readinessTestToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	getReadyz := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return w.Code
	}

	cleanupReadinessTables(t, db)
	starts := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	for _, prompt := range []string{"push-1", "push-2"} {
		b := sameExamBundle("exam-int", "dep-int", prompt, starts)
		b.Checksum = service.ComputeBundleChecksum(b)
		if code := postBundle(b); code != http.StatusOK {
			t.Fatalf("internal push %q status = %d, want 200", prompt, code)
		}
	}

	// After two sequential same-exam pushes the DB and cache agree and the
	// node is ready.
	if code := getReadyz(); code != http.StatusOK {
		t.Fatalf("/readyz after pushes = %d, want 200", code)
	}
	items, err := repo.ListItemsByExamID(context.Background(), "exam-int")
	if err != nil || len(items) == 0 || items[0].PromptSnapshot != "push-2" {
		t.Fatalf("final DB version wrong: items=%v err=%v", items, err)
	}
	content, _, _, _, cerr := contentSvc.GetExamContent(context.Background(), "exam-int")
	if cerr != nil {
		t.Fatalf("content after pushes: %v", cerr)
	}
	found := false
	for _, it := range content.Items {
		if it.Prompt == "push-2" {
			found = true
		}
	}
	if !found {
		t.Fatal("cache does not reflect the latest pushed bundle")
	}
}

func bundleJSON(t *testing.T, b interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	return raw
}
