//go:build integration

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	nodecentral "github.com/celpung/bangkusekolah_exam_node/app/adapter/central"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	outbound "github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type inboundBatchAlias = inbound.ExamNodeAttemptBatch
type inboundResultAlias = inbound.ExamNodeIngestResult

// openHarvestRepo opens a MySQL-backed repository pair for the real-client
// test; db(t) exposes the handle for seeding.
type harvestRepoBundle struct {
	repo outbound_repository.NodeRepository
	tx   outbound.TxManager
	gdb  *gorm.DB
}

func (h *harvestRepoBundle) db(*testing.T) *gorm.DB { return h.gdb }

func openHarvestRepo(t *testing.T) (*harvestRepoBundle, outbound.TxManager) {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	return &harvestRepoBundle{repo: repository.NewNodeRepository(db), gdb: db}, helper.NewTxManager(db)
}

// TestIntegration_HarvestRealClientTwoDeployments pins HIGH-4 (review v2):
// two grouped drains through the REAL central.HarvestClient hit exactly
// POST .../deployments/dep-A/attempts and POST .../deployments/dep-B/attempts.
func TestIntegration_HarvestRealClientTwoDeployments(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set for integration test")
	}

	var mu sync.Mutex
	gotPaths := map[string][]string{} // path -> attempt IDs

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch inboundBatchAlias
		_ = json.NewDecoder(r.Body).Decode(&batch)
		mu.Lock()
		for _, a := range batch.Attempts {
			gotPaths[r.URL.Path] = append(gotPaths[r.URL.Path], a.ID)
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		res := inboundResultAlias{}
		for _, a := range batch.Attempts {
			res.AcceptedAttemptIDs = append(res.AcceptedAttemptIDs, a.ID)
		}
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer server.Close()

	client := nodecentral.NewHarvestClient(&config.Config{
		CentralBaseURL:   server.URL + "/", // trailing slash must be normalized
		CentralNodeToken: "tok",
	})

	repoBundle, txManager := openHarvestRepo(t)
	cleanupIntegrityTables(t, repoBundle.gdb)
	mustExec(t, repoBundle.gdb, "DELETE FROM attempts")
	mustExec(t, repoBundle.gdb, "DELETE FROM items")
	mustExec(t, repoBundle.gdb, "DELETE FROM exams")
	mustExec(t, repoBundle.gdb, "DELETE FROM participants")
	harvestSeedExam(t, repoBundle.gdb, repoBundle.repo, txManager, "exam-real-a", "dep-A", "EEEEEE-111111")
	harvestSeedExam(t, repoBundle.gdb, repoBundle.repo, txManager, "exam-real-b", "dep-B", "EEEEEE-222222")
	harvestInsertAttempt(t, repoBundle.gdb, "real-A1", "exam-real-a")
	harvestInsertAttempt(t, repoBundle.gdb, "real-B1", "exam-real-b")

	harvestSvc := NewHarvestService(repoBundle.repo, client)
	n, err := harvestSvc.DrainOnce(context.Background())
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n != 2 {
		t.Fatalf("accepted %d, want 2", n)
	}

	wantA := "/api/v1/exam-nodes/deployments/dep-A/attempts"
	wantB := "/api/v1/exam-nodes/deployments/dep-B/attempts"
	if ids := gotPaths[wantA]; !(len(ids) == 1 && ids[0] == "real-A1") {
		t.Errorf("%s received %v, want [real-A1]", wantA, ids)
	}
	if ids := gotPaths[wantB]; !(len(ids) == 1 && ids[0] == "real-B1") {
		t.Errorf("%s received %v, want [real-B1]", wantB, ids)
	}
}
