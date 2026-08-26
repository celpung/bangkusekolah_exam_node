//go:build integration

package service

import (
	"context"
	"os"
	"strings"
	"testing"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

// failingListRepo wraps the real repository and fails ListItemsByExamID for
// one exam — deterministic failure injection for the rehydrate path.
type failingListRepo struct {
	outbound_repository.NodeRepository
	failExamID string
}

func (f *failingListRepo) ListItemsByExamID(ctx context.Context, examID string) ([]entity.Item, error) {
	if examID == f.failExamID {
		return nil, context.DeadlineExceeded
	}
	return f.NodeRepository.ListItemsByExamID(ctx, examID)
}

// TestIntegration_RehydrateAllCachesFailure pins the failure path of the real
// startup helper: when rebuilding any persisted exam fails, RehydrateAllCaches
// returns an error naming that exam (the executable then decides to abort).
func TestIntegration_RehydrateAllCachesFailure(t *testing.T) {
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

	realRepo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	bundleSvc := NewBundleService(realRepo, txManager, &lifecycleContentSvc{})

	cleanupIntegrityTables(t, db)
	mustExec(t, db, "DELETE FROM items")
	mustExec(t, db, "DELETE FROM exams")
	mustExec(t, db, "DELETE FROM participants")

	b := validBundle("exam-rehy", nowPtr())
	b.Checksum = ComputeBundleChecksum(b)
	if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
		t.Fatalf("load: %v", err)
	}

	contentSvc := NewContentService(&failingListRepo{NodeRepository: realRepo, failExamID: "exam-rehy"})
	err = RehydrateAllCaches(context.Background(), &failingListRepo{NodeRepository: realRepo}, contentSvc)
	if err == nil {
		t.Fatal("expected error from RehydrateAllCaches when an exam rebuild fails")
	}
	if !strings.Contains(err.Error(), "exam-rehy") {
		t.Errorf("error should identify the failing exam, got: %v", err)
	}
}
