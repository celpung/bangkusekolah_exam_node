//go:build integration

package service

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// TestIntegration_PreflightDetectsFieldTampering pins BLOCKER-2 (v3 review):
// editing ANY student-visible or behavioral exam/item field in the DB must
// fail preflight. One bundle, one field mutated per case, restored after each.
func TestIntegration_PreflightDetectsFieldTampering(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set for integration test")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	bundleSvc := NewBundleService(repo, txManager, &lifecycleContentSvc{})
	ctx := context.Background()

	cleanupIntegrityTables(t, db)
	mustExec(t, db, "DELETE FROM items")
	mustExec(t, db, "DELETE FROM exams")
	mustExec(t, db, "DELETE FROM participants")

	now := time.Now()
	b := inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: "dep-fields",
		Exam: inbound.ExamNodeBundleExam{
			ID:                    "exam-fields",
			Title:                 "UTS",
			Instruction:           strPtr("Kerjakan semua soal"),
			StartsAt:              now.Add(time.Hour),
			EndsAt:                now.Add(3 * time.Hour),
			DurationMinutes:       90,
			MaxAttempts:           1,
			ShuffleQuestions:      true,
			ShuffleOptions:        true,
			ShowResultImmediately: true,
			PassingScore:          floatPtr(70),
			ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{{
			ID: "item-fields", SectionID: "sec-1", QuestionType: "single_choice",
			PromptSnapshot: "1+1?", Points: 10,
			AnswerKeySnapshotJSON: map[string]interface{}{"answer": "B"},
		}},
		Participants: []inbound.ExamNodeBundleParticipant{{
			ID: "p-fields", StudentID: "s-1", StudentName: "Budi", AccessCode: "EEEEEE-111111",
		}},
	}
	b.Checksum = ComputeBundleChecksum(b)
	if err := bundleSvc.LoadBundle(ctx, b); err != nil {
		t.Fatalf("load: %v", err)
	}

	cases := []struct {
		name  string
		query string
		args  []interface{}
	}{
		{"exams.instruction", `UPDATE exams SET instruction = 'TAMPERED' WHERE id = ?`, []interface{}{"exam-fields"}},
		{"exams.shuffle_questions", `UPDATE exams SET shuffle_questions = 0 WHERE id = ?`, []interface{}{"exam-fields"}},
		{"exams.shuffle_options", `UPDATE exams SET shuffle_options = 0 WHERE id = ?`, []interface{}{"exam-fields"}},
		{"exams.show_result_immediately", `UPDATE exams SET show_result_immediately = 0 WHERE id = ?`, []interface{}{"exam-fields"}},
		{"exams.passing_score", `UPDATE exams SET passing_score = 50 WHERE id = ?`, []interface{}{"exam-fields"}},
		{"items.section_title", `UPDATE items SET section_title = 'TAMPERED' WHERE id = ?`, []interface{}{"item-fields"}},
		{"items.section_sort_order", `UPDATE items SET section_sort_order = 99 WHERE id = ?`, []interface{}{"item-fields"}},
	}

	for _, tc := range cases {
		if err := db.Exec(tc.query, tc.args...).Error; err != nil {
			t.Fatalf("%s: exec: %v", tc.name, err)
		}
		if err := bundleSvc.Preflight(ctx, "exam-fields", 1, 1); err == nil {
			t.Errorf("preflight PASSED after editing %s — field not covered by content hash", tc.name)
		} else {
			t.Logf("ok: %s tamper detected: %v", tc.name, short(err.Error()))
		}
		// restore
		load(t, bundleSvc, b)
	}
}

func strPtr(s string) *string     { return &s }
func floatPtr(f float64) *float64 { return &f }

func load(t *testing.T, bundleSvc *BundleService, b inbound.ExamNodeBundle) {
	t.Helper()
	if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
		t.Fatalf("reload: %v", err)
	}
}

func short(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}
