//go:build integration

package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/model"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testDBForAttemptConcurrency(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		host := os.Getenv("DB_HOST")
		if host == "" {
			host = "127.0.0.1"
		}
		port := os.Getenv("DB_PORT")
		if port == "" {
			port = "3306"
		}
		user := os.Getenv("DB_USERNAME")
		pass := os.Getenv("DB_PASSWORD")
		name := os.Getenv("DB_NAME")
		if name != "" {
			name = name + "_test"
		}
		if user != "" && name != "" {
			dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", user, pass, host, port, name)
		}
	}
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set for integration test")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	var actualDBName string
	if err := db.Raw("SELECT DATABASE()").Scan(&actualDBName).Error; err != nil {
		t.Fatalf("get db name: %v", err)
	}
	if !strings.HasSuffix(actualDBName, "_test") {
		t.Fatalf("refusing destructive test on non-test db %q", actualDBName)
	}
	return db
}

func resetAttemptDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	_ = db.Exec("SET FOREIGN_KEY_CHECKS = 0").Error
	for _, tbl := range []string{"answers", "attempts", "participants", "items", "exams"} {
		_ = db.Migrator().DropTable(tbl)
	}
	_ = db.Exec("SET FOREIGN_KEY_CHECKS = 1").Error
	if err := db.AutoMigrate(&model.Exam{}, &model.Participant{}, &model.Attempt{}, &model.Answer{}, &model.Item{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

func TestIntegration_ConcurrentStartAttemptExactlyOne(t *testing.T) {
	db := testDBForAttemptConcurrency(t)
	resetAttemptDB(t, db)
	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	idGen := &concurrentIDGen{}
	svc := NewAttemptService(repo, txManager, idGen)

	now := time.Now()
	exam := &model.Exam{
		ID: "exam-conc", DeploymentID: "dep-conc", Title: "UTS",
		StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour),
		DurationMinutes: 90, MaxAttempts: 1, MaxScore: 40,
		AccessCodePrefix: "ABCDEF", BundleChecksum: "sha256:abc", LoadedAt: now,
		ResultSelectionPolicy: "latest",
	}
	if err := db.Create(exam).Error; err != nil {
		t.Fatalf("seed exam: %v", err)
	}
	part := &model.Participant{ID: "part-conc", ExamID: "exam-conc", StudentID: "stu-conc", StudentName: "Budi", AccessCode: "ABCDEF-GHIJKL", AttemptCount: 0}
	if err := db.Create(part).Error; err != nil {
		t.Fatalf("seed participant: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	results := make([]*entity.Attempt, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			att, err := svc.StartAttempt(context.Background(), "part-conc", "exam-conc")
			results[idx] = att
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d StartAttempt error: %v", i, err)
		}
		if results[i] == nil {
			t.Fatalf("goroutine %d returned nil attempt", i)
		}
	}
	if results[0].ID != results[1].ID {
		// If both created distinct attempts, that's a bug: should be same active
		var count int64
		if err := db.Model(&model.Attempt{}).Where("participant_id = ?", "part-conc").Count(&count).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected exactly 1 attempt, got %d (ids %q %q)", count, results[0].ID, results[1].ID)
		}
		t.Fatalf("concurrent starts returned different IDs %q vs %q but count is 1 - one should have resumed", results[0].ID, results[1].ID)
	}
	var count int64
	if err := db.Model(&model.Attempt{}).Where("participant_id = ?", "part-conc").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 attempt in DB, got %d", count)
	}
	var p model.Participant
	if err := db.Where("id = ?", "part-conc").First(&p).Error; err != nil {
		t.Fatalf("load participant: %v", err)
	}
	if p.AttemptCount != 1 {
		t.Fatalf("expected AttemptCount 1, got %d", p.AttemptCount)
	}
	if p.LatestAttemptID == nil || *p.LatestAttemptID != results[0].ID {
		t.Fatalf("latest_attempt_id mismatch: got %v want %q", p.LatestAttemptID, results[0].ID)
	}
}

type concurrentIDGen struct{}

func (g *concurrentIDGen) NewID() string { return uuid.NewString() }
