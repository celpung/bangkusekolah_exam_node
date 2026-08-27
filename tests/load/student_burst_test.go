//go:build load
// +build load

package load

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/provider"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/security"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
	"gorm.io/gorm"
)

// TestBurst simulates the D-0 minute-zero herd: 1000 students start an
// attempt, autosave 40 items each, then submit — all concurrently.
// Run with: go test -tags=load -run TestBurst -count=1 -v
func TestBurst(t *testing.T) {
	if testing.Short() {
		t.Skip("skip burst load test in -short")
	}
	cfg := loadTestConfig(t)
	db, err := provider.Connect(cfg)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	verifyLoadTestDatabase(t, db)
	if err := provider.Run(sqlDB); err != nil {
		t.Fatalf("run node migrations: %v", err)
	}
	t.Cleanup(func() {
		cleanupBurstData(t, db)
		_ = sqlDB.Close()
	})
	cleanupBurstData(t, db)

	bundle := syntheticBundle(1000, 40)
	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	contentSvc := service.NewContentService(repo)
	bundleSvc := service.NewBundleService(repo, txManager, contentSvc)
	if err := bundleSvc.LoadBundle(context.Background(), bundle); err != nil {
		t.Fatalf("seed bundle: %v", err)
	}

	// Services under test — same wiring as cmd/examnode.
	attemptSvc := service.NewAttemptService(repo, txManager, security.NewIDGenerator())

	const students = 1000
	var (
		errors atomic.Int64
		done   atomic.Int64
	)

	participantIDs := participantIDsFromBundle(bundle)

	// Phase 1: 1000 StartAttempt within 60s (the D-0 herd).
	t.Log("Phase 1: StartAttempt burst")
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(students)
	for i := 0; i < students; i++ {
		go func(pid string) {
			defer wg.Done()
			_, err := attemptSvc.StartAttempt(context.Background(), pid)
			if err != nil {
				errors.Add(1)
				t.Logf("StartAttempt %s: %v", pid, err)
			} else {
				done.Add(1)
			}
		}(participantIDs[i])
	}
	wg.Wait()
	elapsed := time.Since(start)
	if got := done.Load(); got != students {
		t.Fatalf("StartAttempt: %d/%d succeeded, %d errors, elapsed %v", got, students, errors.Load(), elapsed)
	}
	t.Logf("StartAttempt burst: %d students in %v (%.0f rps)", students, elapsed, float64(students)/elapsed.Seconds())

	// Phase 2: each student autosaves 40 items concurrently.
	t.Log("Phase 2: Autosave burst")
	errors.Store(0)
	done.Store(0)
	autosaveStart := time.Now()
	wg.Add(students)
	for i := 0; i < students; i++ {
		go func(pid string) {
			defer wg.Done()
			att, err := repo.FindActiveAttemptByParticipant(context.Background(), pid)
			if err != nil {
				errors.Add(1)
				t.Logf("FindActiveAttemptByParticipant %s: %v", pid, err)
				return
			}
			if att == nil {
				errors.Add(1)
				t.Logf("FindActiveAttemptByParticipant %s: nil attempt", pid)
				return
			}
			for j, item := range bundle.Items {
				seq := int64(j + 1)
				answerJSON := map[string]interface{}{"answer": "A"}
				if _, err := attemptSvc.AutosaveAnswer(context.Background(), att.ID, item.ID, answerJSON, nil, seq, pid); err != nil {
					errors.Add(1)
					return
				}
			}
			done.Add(1)
		}(participantIDs[i])
	}
	wg.Wait()
	autosaveElapsed := time.Since(autosaveStart)
	t.Logf("Autosave burst: %d/%d students × %d items in %v (%.0f rps), errors %d",
		done.Load(), students, len(bundle.Items), autosaveElapsed,
		float64(students*len(bundle.Items))/autosaveElapsed.Seconds(), errors.Load())
	if errors.Load() > 0 {
		t.Fatalf("autosave errors: %d", errors.Load())
	}

	// Phase 3: 1000 Submit — the submit herd.
	t.Log("Phase 3: Submit burst")
	errors.Store(0)
	done.Store(0)
	submitStart := time.Now()
	wg.Add(students)
	for i := 0; i < students; i++ {
		go func(pid string) {
			defer wg.Done()
			att, err := repo.FindActiveAttemptByParticipant(context.Background(), pid)
			if err != nil {
				errors.Add(1)
				t.Logf("FindActiveAttemptByParticipant %s: %v", pid, err)
				return
			}
			if att == nil {
				errors.Add(1)
				t.Logf("FindActiveAttemptByParticipant %s: nil attempt", pid)
				return
			}
			if _, err := attemptSvc.SubmitAttempt(context.Background(), att.ID, pid); err != nil {
				errors.Add(1)
				return
			}
			done.Add(1)
		}(participantIDs[i])
	}
	wg.Wait()
	submitElapsed := time.Since(submitStart)
	t.Logf("Submit burst: %d/%d in %v (%.0f rps), errors %d",
		done.Load(), students, submitElapsed,
		float64(students)/submitElapsed.Seconds(), errors.Load())
	if done.Load() != students {
		t.Fatalf("submit: %d/%d, errors %d", done.Load(), students, errors.Load())
	}
	assertBurstCounts(t, db, students, len(bundle.Items))

	// Gate: entire burst must complete within 90s on 4 vCPU.
	totalElapsed := time.Since(start)
	if totalElapsed > 90*time.Second {
		t.Fatalf("burst took %v, want <90s", totalElapsed)
	}
	t.Logf("Burst complete: %d students, 3 phases in %v", students, totalElapsed)
}

func assertBurstCounts(t *testing.T, db *gorm.DB, wantStudents, itemsPerStudent int) {
	t.Helper()
	var submitted, attempts, answers, duplicateIdentity int64
	checks := []struct {
		name  string
		query string
		want  int64
	}{
		{"submitted attempts", "SELECT COUNT(*) FROM attempts WHERE exam_id = ? AND status = 'submitted'", int64(wantStudents)},
		{"attempts", "SELECT COUNT(*) FROM attempts WHERE exam_id = ?", int64(wantStudents)},
		{"answers", "SELECT COUNT(*) FROM answers a JOIN attempts t ON t.id = a.attempt_id WHERE t.exam_id = ?", int64(wantStudents * itemsPerStudent)},
	}
	values := []*int64{&submitted, &attempts, &answers}
	for i, check := range checks {
		if err := db.Raw(check.query, "exam-burst").Scan(values[i]).Error; err != nil {
			t.Fatalf("count %s: %v", check.name, err)
		}
		if *values[i] != check.want {
			t.Fatalf("count %s = %d, want %d", check.name, *values[i], check.want)
		}
	}
	if err := db.Raw("SELECT COUNT(*) FROM (SELECT participant_id, attempt_no FROM attempts WHERE exam_id = ? GROUP BY participant_id, attempt_no HAVING COUNT(*) > 1) duplicates", "exam-burst").Scan(&duplicateIdentity).Error; err != nil {
		t.Fatalf("count duplicate attempt identities: %v", err)
	}
	if duplicateIdentity != 0 {
		t.Fatalf("found %d duplicate (participant_id, attempt_no) identities", duplicateIdentity)
	}
}

func loadTestConfig(t *testing.T) *config.Config {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Fatal("TEST_DB_DSN is required for the load test")
	}
	t.Setenv("DB_DSN", dsn)
	if os.Getenv("NODE_JWT_SECRET") == "" {
		t.Setenv("NODE_JWT_SECRET", "load-test-jwt-secret-0123456789")
	}
	if os.Getenv("CENTRAL_BASE_URL") == "" {
		t.Setenv("CENTRAL_BASE_URL", "https://load-test.invalid")
	}
	if os.Getenv("CENTRAL_NODE_TOKEN") == "" {
		t.Setenv("CENTRAL_NODE_TOKEN", "load-test-node-token")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load test config: %v", err)
	}
	return cfg
}

func verifyLoadTestDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	var actualName string
	if err := db.Raw("SELECT DATABASE()").Scan(&actualName).Error; err != nil {
		t.Fatalf("read connected database name: %v", err)
	}
	if actualName == "" || !strings.HasSuffix(actualName, "_test") {
		t.Fatalf("refusing load test on non-test database %q", actualName)
	}
}

func cleanupBurstData(t *testing.T, db *gorm.DB) {
	t.Helper()
	const examID = "exam-burst"
	queries := []string{
		"DELETE FROM harvest_log WHERE attempt_id IN (SELECT id FROM attempts WHERE exam_id = ?)",
		"DELETE FROM integrity_events WHERE attempt_id IN (SELECT id FROM attempts WHERE exam_id = ?)",
		"DELETE FROM answers WHERE attempt_id IN (SELECT id FROM attempts WHERE exam_id = ?)",
		"DELETE FROM attempts WHERE exam_id = ?",
		"DELETE FROM participants WHERE exam_id = ?",
		"DELETE FROM items WHERE exam_id = ?",
		"DELETE FROM exams WHERE id = ?",
	}
	for _, query := range queries {
		if err := db.Exec(query, examID).Error; err != nil {
			t.Errorf("cleanup burst data: %v", err)
		}
	}
}

// syntheticBundle builds a bundle with `itemCount` single_choice items and
// `participantCount` participants with Crockford access codes.
func syntheticBundle(participantCount, itemCount int) inbound.ExamNodeBundle {
	now := time.Now()
	end := now.Add(2 * time.Hour)

	items := make([]inbound.ExamNodeBundleItem, itemCount)
	for i := 0; i < itemCount; i++ {
		items[i] = inbound.ExamNodeBundleItem{
			ID:             fmt.Sprintf("item-%03d", i+1),
			SectionID:      "sec-1",
			QuestionType:   entity.QuestionSingleChoice,
			PromptSnapshot: fmt.Sprintf(`{"question":"Q%d","options":["A","B","C","D"]}`, i+1),
			OptionsSnapshotJSON: []map[string]interface{}{
				{"label": "A", "value": "A"},
				{"label": "B", "value": "B"},
				{"label": "C", "value": "C"},
				{"label": "D", "value": "D"},
			},
			AnswerKeySnapshotJSON: map[string]interface{}{"answer": "A"},
			Points:                10,
			SortOrder:             i + 1,
		}
	}

	participants := make([]inbound.ExamNodeBundleParticipant, participantCount)
	for i := 0; i < participantCount; i++ {
		participants[i] = inbound.ExamNodeBundleParticipant{
			ID:          fmt.Sprintf("part-%04d", i+1),
			StudentID:   fmt.Sprintf("stu-%04d", i+1),
			StudentName: fmt.Sprintf("Student %d", i+1),
			AccessCode:  fmt.Sprintf("AAAAAA-%06d", i+1),
		}
	}

	return inbound.ExamNodeBundle{
		BundleVersion: 1,
		DeploymentID:  "dep-burst",
		Exam: inbound.ExamNodeBundleExam{
			ID:                    "exam-burst",
			Title:                 "Burst Load Test",
			StartsAt:              now,
			EndsAt:                end,
			DurationMinutes:       120,
			MaxAttempts:           1,
			ShuffleQuestions:      false,
			ShuffleOptions:        false,
			ShowResultImmediately: false,
			ResultSelectionPolicy: entity.ResultSelectionBest,
		},
		Sections: []inbound.ExamNodeBundleSection{
			{ID: "sec-1", Title: "Section 1", SortOrder: 1},
		},
		Items:        items,
		Participants: participants,
		Checksum:     "burst-test-checksum",
	}
}

func participantIDsFromBundle(b inbound.ExamNodeBundle) []string {
	ids := make([]string, len(b.Participants))
	for i, p := range b.Participants {
		ids[i] = p.ID
	}
	return ids
}
