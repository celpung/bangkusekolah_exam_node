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

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/handler"
	node_router "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/router"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	node_security "github.com/celpung/bangkusekolah_exam_node/app/adapter/security"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

// TestIntegration_ReadyzEmptyNodeFailsClosed pins BLOCKER (v8 review): a
// migrated node with zero exams must report /readyz 503; after loading and
// rebuilding a valid bundle it flips to 200 — through the actual readiness
// router implementation.
func TestIntegration_ReadyzEmptyNodeFailsClosed(t *testing.T) {
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

	cfg := &config.Config{JWTSecret: "integration-jwt-secret-32-characters!", JWTTTL: 90 * time.Minute}
	internalH := handler.NewInternalHandler(bundleSvc)
	harvestH := handler.NewHarvestHandler(service.NewHarvestService(repo, testPusherOK{}))
	attemptSvc := service.NewAttemptService(repo, txManager, &readinessIDGen{})
	integritySvc := service.NewIntegrityService(repo, txManager, &readinessIDGen{})

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
	r.Mount("/", node_router.NewRouter(node_security.NewJWTIssuer(cfg), readinessTestToken, nil, nil, contentSvc, attemptSvc, integritySvc, internalH, harvestH, readiness))

	getReadyz := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return w.Code
	}

	cleanupReadinessTables(t, db)

	// Fresh migrated DB with zero exams -> fail closed.
	if code := getReadyz(); code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz on empty node = %d, want 503", code)
	}

	// Load + rebuild a valid bundle -> ready.
	b := readinessBundle("ready prompt")
	b.Checksum = service.ComputeBundleChecksum(b)
	if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
		t.Fatalf("load: %v", err)
	}
	if code := getReadyz(); code != http.StatusOK {
		t.Fatalf("/readyz after load = %d, want 200", code)
	}
}
