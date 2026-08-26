//go:build integration

package service

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// recordedPush captures one push call: which deployment got which attempts.
type recordedPush struct {
	deploymentID string
	attemptIDs   []string
}

// multiDeploymentClient records pushes per deployment and answers with a
// scripted ack.
type multiDeploymentClient struct {
	mu     sync.Mutex
	pushes []recordedPush
	ack    func(deploymentID string, batch inbound.ExamNodeAttemptBatch) inbound.ExamNodeIngestResult
}

func (m *multiDeploymentClient) Push(_ context.Context, deploymentID string, batch inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	m.mu.Lock()
	ids := make([]string, 0, len(batch.Attempts))
	for _, a := range batch.Attempts {
		ids = append(ids, a.ID)
	}
	m.pushes = append(m.pushes, recordedPush{deploymentID: deploymentID, attemptIDs: ids})
	res := m.ack(deploymentID, batch)
	m.mu.Unlock()
	return &res, nil
}

func (m *multiDeploymentClient) pushesFor(deploymentID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []string
	for _, p := range m.pushes {
		if p.deploymentID == deploymentID {
			ids = append(ids, p.attemptIDs...)
		}
	}
	return ids
}

func codeFor(id string) string {
	codes := map[string]string{
		"exam-dep-a": "SSSSSS-111111",
		"exam-dep-b": "SSSSSS-222222",
	}
	if c, ok := codes[id]; ok {
		return c
	}
	return "SSSSSS-999999"
}

// TestIntegration_HarvestGroupsByDeployment pins BLOCKER-1 (v12 review):
// attempts from two exams/deployments are pushed as one batch PER deployment
// — A's attempt goes to deployment A only, B's to deployment B only.
func TestIntegration_HarvestGroupsByDeployment(t *testing.T) {
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

	cleanupIntegrityTables(t, db)
	mustExec(t, db, "DELETE FROM items")
	mustExec(t, db, "DELETE FROM exams")
	mustExec(t, db, "DELETE FROM participants")

	starts := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	bundleSvc := NewBundleService(repo, txManager, &lifecycleContentSvc{})

	for id, dep := range map[string]string{
		"exam-dep-a": "dep-A",
		"exam-dep-b": "dep-B",
	} {
		b := inbound.ExamNodeBundle{
			BundleVersion: 1, DeploymentID: dep,
			Exam: inbound.ExamNodeBundleExam{
				ID: id, Title: id, StartsAt: starts, EndsAt: starts.Add(2 * time.Hour),
				DurationMinutes: 60, MaxAttempts: 1, ResultSelectionPolicy: "best",
			},
			Items: []inbound.ExamNodeBundleItem{{
				ID: "item-" + id, SectionID: "sec-1", QuestionType: "single_choice",
				PromptSnapshot: "q", Points: 10,
				AnswerKeySnapshotJSON: map[string]interface{}{"answer": "A"},
			}},
			Participants: []inbound.ExamNodeBundleParticipant{{
				ID: "p-" + id, StudentID: "s-" + id, StudentName: "Budi",
				AccessCode: codeFor(id),
			}},
		}
		b.Checksum = ComputeBundleChecksum(b)
		if err := bundleSvc.LoadBundle(context.Background(), b); err != nil {
			t.Fatalf("load %s: %v", id, err)
		}
	}

	// One finished attempt under each exam.
	now := time.Now().UTC()
	for _, att := range []entity.Attempt{
		{ID: "att-A1", ExamID: "exam-dep-a", ParticipantID: "p-exam-dep-a", StudentID: "s-a", Status: entity.AttemptSubmitted, AttemptNo: 1, StartedAt: now.Add(-time.Hour), DueAt: now},
		{ID: "att-B1", ExamID: "exam-dep-b", ParticipantID: "p-exam-dep-b", StudentID: "s-b", Status: entity.AttemptSubmitted, AttemptNo: 1, StartedAt: now.Add(-time.Hour), DueAt: now},
	} {
		if err := db.Exec(`INSERT INTO attempts (id, participant_id, student_id, exam_id, attempt_no, status,
			started_at, due_at, submitted_at, max_score, grading_status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			att.ID, att.ParticipantID, att.StudentID, att.ExamID, att.AttemptNo, string(att.Status),
			att.StartedAt, att.DueAt, att.SubmittedAt, 10, "graded").Error; err != nil {
			t.Fatalf("insert attempt %s: %v", att.ID, err)
		}
	}

	client := &multiDeploymentClient{ack: func(dep string, batch inbound.ExamNodeAttemptBatch) inbound.ExamNodeIngestResult {
		res := inbound.ExamNodeIngestResult{}
		for _, a := range batch.Attempts {
			res.AcceptedAttemptIDs = append(res.AcceptedAttemptIDs, a.ID)
		}
		return res
	}}
	harvestSvc := NewHarvestService(repo, client)
	n, err := harvestSvc.DrainOnce(context.Background())
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if n != 2 {
		t.Fatalf("drained %d, want 2", n)
	}

	if idsA := client.pushesFor("dep-A"); !(len(idsA) == 1 && idsA[0] == "att-A1") {
		t.Errorf("dep-A received %v, want [att-A1]", idsA)
	}
	if idsB := client.pushesFor("dep-B"); !(len(idsB) == 1 && idsB[0] == "att-B1") {
		t.Errorf("dep-B received %v, want [att-B1]", idsB)
	}
}
