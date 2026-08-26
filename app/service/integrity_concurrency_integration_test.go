//go:build integration

package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/model"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testDBForIntegrity(t *testing.T) *gorm.DB {
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
	var actual string
	if err := db.Raw("SELECT DATABASE()").Scan(&actual).Error; err != nil {
		t.Fatalf("get db name: %v", err)
	}
	if !strings.HasSuffix(actual, "_test") {
		t.Fatalf("refusing destructive test on non-test db %q", actual)
	}
	return db
}

func resetIntegrityDB(t *testing.T, db *gorm.DB, examsTable bool) *gorm.DB {
	t.Helper()
	if examsTable {
		_ = db.Exec(`CREATE TABLE IF NOT EXISTS exams (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			deployment_id VARCHAR(36) NOT NULL DEFAULT '',
			title VARCHAR(255) NOT NULL DEFAULT '',
			starts_at DATETIME NOT NULL,
			ends_at DATETIME NOT NULL,
			duration_minutes INT NOT NULL DEFAULT 90,
			max_attempts INT NOT NULL DEFAULT 1,
			result_selection_policy VARCHAR(30) NOT NULL DEFAULT 'latest',
			max_score DECIMAL(8,2) NOT NULL DEFAULT 100,
			has_manual_items TINYINT(1) NOT NULL DEFAULT 0,
			access_code_prefix VARCHAR(10) NOT NULL DEFAULT '',
			bundle_checksum VARCHAR(80) NOT NULL DEFAULT '',
			loaded_at DATETIME NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`).Error
		return db
	}
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS attempts (
		id VARCHAR(36) NOT NULL PRIMARY KEY,
		participant_id VARCHAR(36) NOT NULL,
		student_id VARCHAR(36) NOT NULL,
		exam_id VARCHAR(36) NOT NULL DEFAULT '',
		attempt_no INT NOT NULL DEFAULT 1,
		status VARCHAR(30) NOT NULL,
		started_at DATETIME NOT NULL,
		due_at DATETIME NOT NULL,
		submitted_at DATETIME NULL,
		auto_submitted_at DATETIME NULL,
		score DECIMAL(8,2) NULL,
		max_score DECIMAL(8,2) NOT NULL DEFAULT 100,
		grading_status VARCHAR(30) NOT NULL DEFAULT 'pending',
		harvested_at DATETIME NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`).Error
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS integrity_events (
		id VARCHAR(36) NOT NULL PRIMARY KEY,
		attempt_id VARCHAR(36) NOT NULL,
		student_id VARCHAR(36) NOT NULL,
		event_type VARCHAR(40) NOT NULL,
		description TEXT NULL,
		metadata_json JSON NULL,
		created_at DATETIME NOT NULL,
		KEY idx_integrity_attempt (attempt_id, event_type, created_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`).Error
	return db
}

// TestIntegration_IntegrityRateLimitAtomic pins BLOCKER-2: 15 simultaneous
// events on one attempt must store exactly 10 — no more.
func TestIntegration_IntegrityRateLimitAtomic(t *testing.T) {
	db := testDBForIntegrity(t)
	resetIntegrityDB(t, db, false)
	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	svc := NewIntegrityService(repo, txManager, &concurrentIDGen{})

	now := gormNow()
	if err := db.Exec(`INSERT INTO attempts (id, participant_id, student_id, exam_id, attempt_no, status, started_at, due_at, max_score, grading_status)
		VALUES (?, ?, ?, ?, 1, 'in_progress', NOW(), DATE_ADD(NOW(), INTERVAL 1 HOUR), 100, 'pending')`,
		"att-int", "part-1", "stu-1", "exam-a").Error; err != nil {
		t.Fatalf("seed attempt: %v", err)
	}
	_ = now

	const workers = 15
	var wg sync.WaitGroup
	wg.Add(workers)
	results := make([]error, workers)
	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			evType := fmt.Sprintf("focus_lost_%d", idx%2) // two types to dodge dedup
			_, results[idx] = svc.RecordEvent(context.Background(), "att-int", "part-1", evType, nil, nil)
		}(i)
	}
	wg.Wait()

	floods := 0
	for _, err := range results {
		if err == nil {
			continue
		}
		if err == node_error.ErrIntegrityFlood || strings.Contains(err.Error(), "too many") {
			floods++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	var stored int64
	if err := db.Raw("SELECT COUNT(*) FROM integrity_events WHERE attempt_id = ?", "att-int").Scan(&stored).Error; err != nil {
		t.Fatalf("count stored: %v", err)
	}
	if stored > 10 {
		t.Fatalf("rate limit breached atomically: %d events stored, want <=10 (floods=%d)", stored, floods)
	}
	if floods == 0 && stored != 10 {
		t.Fatalf("expected some flood rejections or exactly 10, got %d stored / %d floods", stored, floods)
	}
}

// TestIntegration_IntegrityDedupAtomic pins the dedup window: concurrent
// same-type events collapse to a single row within 5s.
func TestIntegration_IntegrityDedupAtomic(t *testing.T) {
	db := testDBForIntegrity(t)
	resetIntegrityDB(t, db, true)
	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	svc := NewIntegrityService(repo, txManager, &concurrentIDGen{})

	if err := db.Exec(`INSERT INTO attempts (id, participant_id, student_id, exam_id, attempt_no, status, started_at, due_at, max_score, grading_status)
		VALUES (?, ?, ?, ?, 1, 'in_progress', NOW(), DATE_ADD(NOW(), INTERVAL 1 HOUR), 100, 'pending')`,
		"att-dedup", "part-d", "stu-d", "exam-a").Error; err != nil {
		t.Fatalf("seed attempt: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, _ = svc.RecordEvent(context.Background(), "att-dedup", "part-d", "focus_lost", nil, nil)
		}()
	}
	wg.Wait()

	var stored int64
	if err := db.Raw("SELECT COUNT(*) FROM integrity_events WHERE attempt_id = ? AND event_type = ?", "att-dedup", "focus_lost").Scan(&stored).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if stored != 1 {
		t.Fatalf("dedup collapsed to %d rows, want exactly 1", stored)
	}
}

func gormNow() model.Attempt { return model.Attempt{} }

var _ = uuid.NewString
