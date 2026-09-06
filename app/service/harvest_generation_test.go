package service

import (
	"context"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type generationHarvestPusher struct {
	batch inbound.ExamNodeAttemptBatch
}

func (p *generationHarvestPusher) Push(_ context.Context, _ string, batch inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	p.batch = batch
	accepted := make([]string, 0, len(batch.Attempts))
	for _, attempt := range batch.Attempts {
		accepted = append(accepted, attempt.ID)
	}
	return &inbound.ExamNodeIngestResult{AcceptedAttemptIDs: accepted, Failures: map[string]string{}}, nil
}

type generationHarvestRepo struct {
	*fakeHarvestRepo
}

func (f *generationHarvestRepo) MarkAttemptsHarvestedForGeneration(_ context.Context, generations map[string]int64, at time.Time) (int, error) {
	marked := 0
	for id, generation := range generations {
		if attempt, ok := f.attempts[id]; ok && attempt.HarvestedAt == nil && attempt.ResetGeneration == generation && (attempt.Status == entity.AttemptSubmitted || attempt.Status == entity.AttemptAutoSubmitted) {
			attempt.HarvestedAt = &at
			marked++
		}
	}
	return marked, nil
}

func TestHarvestCarriesGenerationAndMarksOnlyTheSentGeneration(t *testing.T) {
	now := time.Date(2026, time.September, 6, 10, 0, 0, 0, time.UTC)
	attempt := &entity.Attempt{ID: "attempt-1", ExamID: "exam-1", ParticipantID: "participant-1", StudentID: "student-1", Status: entity.AttemptSubmitted, StartedAt: now.Add(-time.Hour), DueAt: now.Add(time.Hour), SubmittedAt: &now, ResetGeneration: 7}
	repo := &generationHarvestRepo{fakeHarvestRepo: &fakeHarvestRepo{
		attempts: map[string]*entity.Attempt{"attempt-1": attempt},
		exams:    map[string]*entity.Exam{"exam-1": {ID: "exam-1", DeploymentID: "deployment-1"}},
		answers:  map[string][]entity.Answer{},
		events:   map[string][]entity.IntegrityEvent{},
	}}
	pusher := &generationHarvestPusher{}
	service := NewHarvestService(repo, pusher)

	if _, err := service.DrainOnce(context.Background()); err != nil {
		t.Fatalf("DrainOnce() error = %v", err)
	}
	if len(pusher.batch.Attempts) != 1 || pusher.batch.Attempts[0].ResetGeneration != 7 {
		t.Fatalf("harvest generation = %+v, want 7", pusher.batch.Attempts)
	}
	if attempt.HarvestedAt == nil {
		t.Fatal("attempt was not marked harvested")
	}
}
