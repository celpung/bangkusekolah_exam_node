package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type fakeSubmitRepo struct {
	outbound_repository.NodeRepository
	attempts map[string]*entity.Attempt
	answers  map[string][]entity.Answer
	exam     *entity.Exam
}

func (f *fakeSubmitRepo) FindAttemptByID(_ context.Context, id string) (*entity.Attempt, error) {
	if a, ok := f.attempts[id]; ok {
		return a, nil
	}
	return nil, node_error.ErrAttemptNotFound
}
func (f *fakeSubmitRepo) FindAttemptByIDForUpdate(_ context.Context, id string) (*entity.Attempt, error) {
	if a, ok := f.attempts[id]; ok {
		copied := *a // emulate a fresh DB read, not an aliased pointer
		return &copied, nil
	}
	return nil, node_error.ErrAttemptNotFound
}
func (f *fakeSubmitRepo) ListAnswersByAttempt(_ context.Context, id string) ([]entity.Answer, error) {
	return f.answers[id], nil
}
func (f *fakeSubmitRepo) UpdateAttempt(_ context.Context, a *entity.Attempt) error {
	// emulate the real conditional update WHERE status='in_progress': a
	// finalization write (submitted/auto_submitted/graded) on a row that is
	// already finalized is a no-op and reports locked, like RowsAffected==0.
	if a.Status != entity.AttemptInProgress {
		stored, ok := f.attempts[a.ID]
		if ok && stored.Status != entity.AttemptInProgress {
			return node_error.ErrAttemptLocked
		}
	}
	f.attempts[a.ID] = a
	return nil
}
func (f *fakeSubmitRepo) FindExam(_ context.Context) (*entity.Exam, error) { return f.exam, nil }
func (f *fakeSubmitRepo) FindExamByID(_ context.Context, id string) (*entity.Exam, error) {
	if f.exam != nil && f.exam.ID == id { return f.exam, nil }
	return nil, node_error.ErrExamNotLoaded
}

// ListExpiredAttempts mirrors the sweeper's query so the race test can hand
// the sweeper a stale expired list without hitting the nil embedded interface.
func (f *fakeSubmitRepo) ListExpiredAttempts(_ context.Context, now time.Time) ([]entity.Attempt, error) {
	var out []entity.Attempt
	for _, a := range f.attempts {
		if a.Status == entity.AttemptInProgress && a.DueAt.Before(now) {
			out = append(out, *a)
		}
	}
	return out, nil
}

func submitFixture() (*AttemptService, *fakeSubmitRepo) {
	score10 := 10.0
	exam := &entity.Exam{ID: "exam-1", MaxScore: 30, HasManualItems: false}
	repo := &fakeSubmitRepo{
		exam: exam,
		attempts: map[string]*entity.Attempt{
			"att-1":         {ID: "att-1", ParticipantID: "part-1", ExamID: "exam-1", StudentID: "stu-1", Status: entity.AttemptInProgress, AttemptNo: 1, StartedAt: time.Now().Add(-30 * time.Minute), DueAt: time.Now().Add(60 * time.Minute), MaxScore: 30, GradingStatus: entity.GradingPending},
			"att-submitted": {ID: "att-submitted", ParticipantID: "part-1", ExamID: "exam-1", StudentID: "stu-1", Status: entity.AttemptSubmitted, SubmittedAt: ptrTime(time.Now()), MaxScore: 30},
		},
		answers: map[string][]entity.Answer{
			"att-1": {
				{ID: "ans-1", AttemptID: "att-1", ItemID: "item-1", Score: &score10, MaxScore: 10, GradingStatus: entity.GradingAutoGraded},
				{ID: "ans-2", AttemptID: "att-1", ItemID: "item-2", Score: nil, MaxScore: 20, GradingStatus: entity.GradingManualRequired},
			},
		},
	}
	svc := &AttemptService{repo: repo, txManager: stubNodeTx{}, idGen: stubNodeID{id: "x"}}
	return svc, repo
}

func TestSubmitSumsObjectiveScoresAndMarksManualRequired(t *testing.T) {
	svc, repo := submitFixture()
	att, err := svc.SubmitAttempt(context.Background(), "att-1", "part-1")
	if err != nil {
		t.Fatalf("SubmitAttempt: %v", err)
	}
	if att.Status != entity.AttemptSubmitted || att.SubmittedAt == nil {
		t.Fatalf("attempt not marked submitted: %+v", att)
	}
	if att.Score == nil || *att.Score != 10 {
		t.Fatalf("score = %v, want 10 (essay ungraded)", att.Score)
	}
	if att.GradingStatus != entity.GradingManualRequired {
		t.Fatalf("grading_status = %q, want manual_required (essay present)", att.GradingStatus)
	}
	if repo.attempts["att-1"].Score == nil || *repo.attempts["att-1"].Score != 10 {
		t.Fatalf("not persisted: %+v", repo.attempts["att-1"])
	}
}

func TestSubmitObjectiveOnlyIsGraded(t *testing.T) {
	score10 := 10.0
	svc, repo := submitFixture()
	// overwrite answers with objective-only
	repo.answers["att-1"] = []entity.Answer{{ID: "ans-1", AttemptID: "att-1", ItemID: "item-1", Score: &score10, MaxScore: 10, GradingStatus: entity.GradingAutoGraded}}
	repo.exam.HasManualItems = false
	att, err := svc.SubmitAttempt(context.Background(), "att-1", "part-1")
	if err != nil {
		t.Fatalf("SubmitAttempt: %v", err)
	}
	if att.GradingStatus != entity.GradingAutoGraded || att.Score == nil || *att.Score != 10 {
		t.Fatalf("objective-only should be graded: %+v", att)
	}
}

func TestSubmitIsIdempotent(t *testing.T) {
	svc, _ := submitFixture()
	att, err := svc.SubmitAttempt(context.Background(), "att-submitted", "part-1")
	if err != nil {
		t.Fatalf("SubmitAttempt on already submitted: %v", err)
	}
	if att.Status != entity.AttemptSubmitted {
		t.Fatalf("status changed: %+v", att)
	}
}

func TestSubmitRejectsWrongOwner(t *testing.T) {
	svc, _ := submitFixture()
	_, err := svc.SubmitAttempt(context.Background(), "att-1", "part-2")
	if !errors.Is(err, node_error.ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

func TestSubmitRejectsExpiredAttempt(t *testing.T) {
	svc, repo := submitFixture()
	repo.attempts["att-1"].DueAt = time.Now().Add(-time.Minute)
	_, err := svc.SubmitAttempt(context.Background(), "att-1", "part-1")
	if !errors.Is(err, node_error.ErrAttemptExpired) && !errors.Is(err, node_error.ErrAttemptLocked) {
		t.Fatalf("want expiry error, got %v", err)
	}
}

// TestSubmitManualExamWithoutEssayAnswerRow pins BLOCKER-3: an exam flagged
// has_manual_items stays manual_required even when the student never saved an
// answer for the manual item (no answer row exists).
func TestSubmitManualExamWithoutEssayAnswerRow(t *testing.T) {
	score10 := 10.0
	svc, repo := submitFixture()
	repo.exam.HasManualItems = true
	// only objective answers persisted — no essay row at all
	repo.answers["att-1"] = []entity.Answer{{ID: "ans-1", AttemptID: "att-1", ItemID: "item-1", Score: &score10, MaxScore: 10, GradingStatus: entity.GradingAutoGraded}}
	att, err := svc.SubmitAttempt(context.Background(), "att-1", "part-1")
	if err != nil {
		t.Fatalf("SubmitAttempt: %v", err)
	}
	if att.GradingStatus != entity.GradingManualRequired {
		t.Fatalf("exam with manual items must stay manual_required without essay row, got %q", att.GradingStatus)
	}
	if att.Score == nil || *att.Score != 10 {
		t.Fatalf("score = %v, want 10 (objective subtotal)", att.Score)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
