//go:build integration

package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	node_router "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/router"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

// TestIntegration_SameExamLiveReloadFailsClosed pins BLOCKER (v10 review):
// during a live reload of the SAME exam ID, readiness and content serving
// must fail closed between DB publication and successful cache publication,
// then recover with the NEW content once published.
func TestIntegration_SameExamLiveReloadFailsClosed(t *testing.T) {
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
	v1 := sameExamBundle("exam-live", "dep-live", "v1 prompt", starts)
	v1.Checksum = service.ComputeBundleChecksum(v1)
	if err := loadViaRepo(t, repo, txManager, contentSvc, v1); err != nil {
		t.Fatalf("load v1: %v", err)
	}

	readyz := node_router.NewReadinessRouter(contentSvc,
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
		nil)
	readyzCode := func() int {
		w := httptest.NewRecorder()
		readyz.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return w.Code
	}

	// 2. Replace the SAME exam ID with v2 WITHOUT rebuilding the cache —
	// exactly the live-push window between commit and publication.
	v2 := sameExamBundle("exam-live", "dep-live", "v2 prompt", starts)
	v2.Checksum = service.ComputeBundleChecksum(v2)

	// Publication protocol: mark rebuilding BEFORE the swap commits.
	contentSvc.BeginRebuild("exam-live")
	if err := replaceViaRepo(t, repo, txManager, v2); err != nil {
		t.Fatalf("commit v2: %v", err)
	}

	// 3. Readiness must be 503 while rebuilding (same ID, stale cache).
	if code := readyzCode(); code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz during rebuild = %d, want 503", code)
	}

	// 4. Content must refuse to serve the stale v1 snapshot.
	if _, _, _, _, err := contentSvc.GetExamContent(context.Background(), "exam-live"); err == nil {
		t.Fatal("content served while rebuild in progress — stale v1 leak")
	}

	// 5. Publish v2: successful rebuild clears the rebuilding state.
	if err := contentSvc.RebuildExam(context.Background(), "exam-live"); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	// 6. /readyz -> 200 only after successful publication.
	if code := readyzCode(); code != http.StatusOK {
		t.Fatalf("/readyz after publish = %d, want 200", code)
	}

	// 7. GET content serves v2.
	content, _, _, _, err := contentSvc.GetExamContent(context.Background(), "exam-live")
	if err != nil {
		t.Fatalf("content after publish: %v", err)
	}
	foundV2 := false
	for _, it := range content.Items {
		if strings.Contains(it.Prompt, "v2 prompt") {
			foundV2 = true
		}
	}
	if !foundV2 {
		t.Fatal("published content does not contain v2 prompt")
	}

	// 8. Rebuild failure mid-protocol keeps readiness closed: begin a new
	// rebuild and fail it by deleting the exam rows before publishing.
	contentSvc.BeginRebuild("exam-live")
	mustExec(t, db, "DELETE FROM exams WHERE id = 'exam-live'")
	if err := contentSvc.RebuildExam(context.Background(), "exam-live"); err == nil {
		t.Fatal("expected rebuild failure")
	}
	if code := readyzCode(); code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz after failed publish = %d, want 503", code)
	}

	// 9. Rollback path restores readiness from the previous snapshot.
	loadReadiness(t, bundleSvcOf(repo, txManager, contentSvc), v2)
	contentSvc.CancelRebuild("exam-live")
	if code := readyzCode(); code != http.StatusOK {
		t.Fatalf("/readyz after rollback+cancel = %d, want 200", code)
	}
}

func sameExamBundle(examID, dep, prompt string, starts time.Time) inbound.ExamNodeBundle {
	return inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: dep,
		Exam: inbound.ExamNodeBundleExam{
			ID: examID, Title: examID, StartsAt: starts, EndsAt: starts.Add(2 * time.Hour),
			DurationMinutes: 60, MaxAttempts: 1, ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{{
			ID: "item-" + examID, SectionID: "sec-1", QuestionType: "single_choice",
			PromptSnapshot: prompt, Points: 10,
			AnswerKeySnapshotJSON: map[string]interface{}{"answer": "A"},
		}},
		Participants: []inbound.ExamNodeBundleParticipant{{
			ID: "p-" + examID, StudentID: "s-" + examID, StudentName: "Budi", AccessCode: "LLLLLL-111111",
		}},
	}
}

func loadViaRepo(t *testing.T, repo outbound_repository.NodeRepository, txManager outbound.TxManager, contentSvc *service.ContentService, b inbound.ExamNodeBundle) error {
	t.Helper()
	bundleSvc := service.NewBundleService(repo, txManager, contentSvc)
	return bundleSvc.LoadBundle(context.Background(), b)
}

func replaceViaRepo(t *testing.T, repo outbound_repository.NodeRepository, txManager outbound.TxManager, b inbound.ExamNodeBundle) error {
	t.Helper()
	bundleSvc := service.NewBundleService(repo, txManager, failingRebuilder{})
	return bundleSvc.LoadBundle(context.Background(), b)
}

// failingRebuilder satisfies ContentRebuilder while never touching the cache
// — simulating a live push whose cache publication has not happened yet.
type failingRebuilder struct{}

func (failingRebuilder) RebuildExam(context.Context, string) error { return nil }
func (failingRebuilder) BeginRebuild(string)                       {}
func (failingRebuilder) CancelRebuild(string)                      {}

func bundleSvcOf(repo outbound_repository.NodeRepository, txManager outbound.TxManager, contentSvc *service.ContentService) *service.BundleService {
	return service.NewBundleService(repo, txManager, contentSvc)
}

var _ = json.Marshal
