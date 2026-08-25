//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testDBForUpsert(t *testing.T) *gorm.DB {
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

func resetUpsertDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	_ = db.Exec("SET FOREIGN_KEY_CHECKS = 0").Error
	for _, tbl := range []string{"answers", "attempts"} {
		_ = db.Migrator().DropTable(tbl)
	}
	_ = db.Exec("SET FOREIGN_KEY_CHECKS = 1").Error
	if err := db.Exec(`CREATE TABLE attempts (
		id VARCHAR(36) NOT NULL PRIMARY KEY,
		participant_id VARCHAR(36) NOT NULL,
		student_id VARCHAR(36) NOT NULL,
		attempt_no INT NOT NULL,
		status VARCHAR(30) NOT NULL,
		started_at DATETIME NOT NULL,
		due_at DATETIME NOT NULL,
		submitted_at DATETIME NULL,
		auto_submitted_at DATETIME NULL,
		score DECIMAL(8,2) NULL,
		max_score DECIMAL(8,2) NOT NULL,
		grading_status VARCHAR(30) NOT NULL,
		harvested_at DATETIME NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`).Error; err != nil {
		t.Fatalf("create attempts: %v", err)
	}
	if err := db.Exec(`CREATE TABLE answers (
		id VARCHAR(36) NOT NULL PRIMARY KEY,
		attempt_id VARCHAR(36) NOT NULL,
		item_id VARCHAR(36) NOT NULL,
		answer_json JSON NULL,
		answer_text TEXT NULL,
		score DECIMAL(8,2) NULL,
		max_score DECIMAL(8,2) NOT NULL,
		grading_status VARCHAR(30) NOT NULL,
		last_saved_at DATETIME NOT NULL,
		client_seq BIGINT NOT NULL DEFAULT 0,
		UNIQUE KEY uniq_answers_attempt_item (attempt_id, item_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`).Error; err != nil {
		t.Fatalf("create answers: %v", err)
	}
}

func upsertAnswer(attemptID, itemID, answer string, seq int64) *entity.Answer {
	now := time.Now().UTC().Truncate(time.Second)
	return &entity.Answer{
		ID: uuid.NewString(), AttemptID: attemptID, ItemID: itemID,
		AnswerJSON: map[string]interface{}{"answer": answer},
		Score:      nil, MaxScore: 10, GradingStatus: entity.GradingAutoGraded,
		LastSavedAt: now, ClientSeq: seq,
	}
}

func storedAnswer(t *testing.T, db *gorm.DB, attemptID, itemID string) (string, int64) {
	t.Helper()
	var raw string
	var seq int64
	if err := db.Raw("SELECT answer_json, client_seq FROM answers WHERE attempt_id = ? AND item_id = ?", attemptID, itemID).Row().Scan(&raw, &seq); err != nil {
		t.Fatalf("read stored answer: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("decode answer_json: %v", err)
	}
	answer, _ := decoded["answer"].(string)
	return answer, seq
}

func TestIntegration_UpsertAnswerDropsStaleWrite(t *testing.T) {
	db := testDBForUpsert(t)
	resetUpsertDB(t, db)
	repo := NewNodeRepository(db).(*nodeRepository)
	ctx := context.Background()

	// 1. insert sequence 2 with answer B
	if _, err := repo.UpsertAnswer(ctx, upsertAnswer("att-up", "item-up", "B", 2)); err != nil {
		t.Fatalf("insert seq 2: %v", err)
	}
	answer, seq := storedAnswer(t, db, "att-up", "item-up")
	if answer != "B" || seq != 2 {
		t.Fatalf("after first write want B/2, got %q/%d", answer, seq)
	}

	// 2. replay stale sequence 1 with answer A — must be dropped
	_, err := repo.UpsertAnswer(ctx, upsertAnswer("att-up", "item-up", "A", 1))
	if err != node_error.ErrStaleAnswerWrite {
		t.Fatalf("stale write must return ErrStaleAnswerWrite, got %v", err)
	}

	// 3. stored content remains B / client_seq == 2
	answer, seq = storedAnswer(t, db, "att-up", "item-up")
	if answer != "B" || seq != 2 {
		t.Fatalf("stale replay clobbered content: want B/2, got %q/%d", answer, seq)
	}

	// newer seq overwrites again
	if _, err := repo.UpsertAnswer(ctx, upsertAnswer("att-up", "item-up", "C", 3)); err != nil {
		t.Fatalf("seq 3 write: %v", err)
	}
	answer, seq = storedAnswer(t, db, "att-up", "item-up")
	if answer != "C" || seq != 3 {
		t.Fatalf("newer seq must overwrite: want C/3, got %q/%d", answer, seq)
	}
}

func TestIntegration_UpsertAnswerConcurrentSeqsFinalContentIsHighestSeq(t *testing.T) {
	db := testDBForUpsert(t)
	resetUpsertDB(t, db)
	repo := NewNodeRepository(db).(*nodeRepository)

	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	seqs := []int64{1, 2}
	answers := []string{"A", "B"}
	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			// small jitter so the two writes interleave differently per run
			time.Sleep(time.Duration(idx) * 20 * time.Millisecond)
			_, errs[idx] = repo.UpsertAnswer(context.Background(), upsertAnswer("att-race", "item-race", answers[idx], seqs[idx]))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil && err != node_error.ErrStaleAnswerWrite {
			t.Fatalf("goroutine %d unexpected error: %v", i, err)
		}
	}
	answer, seq := storedAnswer(t, db, "att-race", "item-race")
	if seq != 2 || answer != "B" {
		t.Fatalf("final row must hold highest-seq content: want B/2, got %q/%d", answer, seq)
	}
}
