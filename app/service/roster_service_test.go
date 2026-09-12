package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
)

type fakeRosterRepository struct {
	exam         *entity.Exam
	participants map[string]*entity.Participant
	receipts     map[string]*entity.RosterEventReceipt
	insertCalls  int
	updateError  error
	receiptError error
}

func (r *fakeRosterRepository) ListItemsByExamID(_ context.Context, _ string) ([]entity.Item, error) {
	return nil, nil
}

func (r *fakeRosterRepository) ListParticipantsByExam(_ context.Context, examID string) ([]entity.Participant, error) {
	participants := make([]entity.Participant, 0)
	for _, participant := range r.participants {
		if participant.ExamID == examID {
			participants = append(participants, *participant)
		}
	}
	return participants, nil
}

func (r *fakeRosterRepository) FindReceipt(_ context.Context, eventID string) (*entity.RosterEventReceipt, error) {
	if receipt, ok := r.receipts[eventID]; ok {
		copy := *receipt
		return &copy, nil
	}
	return nil, node_error.ErrRosterReceiptNotFound
}

func (r *fakeRosterRepository) InsertReceipt(_ context.Context, receipt *entity.RosterEventReceipt) error {
	if r.receiptError != nil {
		return r.receiptError
	}
	if r.receipts == nil {
		r.receipts = map[string]*entity.RosterEventReceipt{}
	}
	copy := *receipt
	r.receipts[receipt.EventID] = &copy
	return nil
}

func (r *fakeRosterRepository) FindExamForUpdate(_ context.Context, examID string) (*entity.Exam, error) {
	if r.exam == nil || r.exam.ID != examID {
		return nil, node_error.ErrExamNotLoaded
	}
	return r.exam, nil
}

func (r *fakeRosterRepository) InsertParticipantIfAbsent(_ context.Context, participant *entity.Participant) (*entity.Participant, bool, error) {
	r.insertCalls++
	for _, existing := range r.participants {
		if existing.ExamID == participant.ExamID && existing.StudentID == participant.StudentID {
			copy := *existing
			return &copy, false, nil
		}
		if existing.AccessCode == participant.AccessCode {
			return nil, false, node_error.ErrRosterCodeConflict
		}
	}
	copy := *participant
	r.participants[participant.ID] = &copy
	return &copy, true, nil
}

func (r *fakeRosterRepository) UpdateExamRosterState(_ context.Context, examID string, revision int64, contentHash string) error {
	if r.updateError != nil {
		return r.updateError
	}
	if r.exam == nil || r.exam.ID != examID {
		return node_error.ErrExamNotLoaded
	}
	r.exam.RosterRevision = revision
	r.exam.ContentHash = contentHash
	return nil
}

type fakeRosterTx struct{}

func (fakeRosterTx) Atomic(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type fakeRosterContent struct{}

func (fakeRosterContent) LockExam(string) func() { return func() {} }

type fakeRosterIDGen struct{ next int }

func (g *fakeRosterIDGen) NewID() string {
	g.next++
	return "generated-" + string(rune('0'+g.next))
}

func rosterEvent() inbound.RosterEvent {
	return inbound.RosterEvent{
		EventID: "event-1", DeploymentID: "deployment-1", ExamID: "exam-1", ParticipantID: "participant-2",
		StudentID: "student-2", StudentName: "Student Two", AccessCode: "ABCDEF-GHIJKM", Revision: 1,
		Deadline: time.Now().Add(time.Hour), CreatedAt: time.Now().UTC(), Status: string(entity.RosterEventPending),
	}
}

func newRosterServiceForTest(repo *fakeRosterRepository) *RosterService {
	return NewRosterService(repo, fakeRosterTx{}, &fakeRosterIDGen{}, fakeRosterContent{})
}

func newRosterRepositoryFixture() *fakeRosterRepository {
	return &fakeRosterRepository{
		exam: &entity.Exam{ID: "exam-1", DeploymentID: "deployment-1", EndsAt: time.Now().Add(time.Hour)},
		participants: map[string]*entity.Participant{
			"participant-1": {ID: "participant-1", ExamID: "exam-1", StudentID: "student-1", AccessCode: "ABCDEF-OLD123", AttemptCount: 2, LatestAttemptID: stringPtr("attempt-2")},
		},
		receipts: map[string]*entity.RosterEventReceipt{},
	}
}

func TestRosterServiceApplyIsIdempotentAndPreservesExistingAttemptState(t *testing.T) {
	repo := newRosterRepositoryFixture()
	svc := newRosterServiceForTest(repo)
	event := rosterEvent()

	first, err := svc.Apply(context.Background(), event)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	second, err := svc.Apply(context.Background(), event)
	if err != nil {
		t.Fatalf("duplicate apply: %v", err)
	}
	if first.Status != string(entity.RosterEventApplied) || second.Status != first.Status {
		t.Fatalf("outcomes = %+v / %+v, want applied", first, second)
	}
	if repo.insertCalls != 1 || len(repo.participants) != 2 || len(repo.receipts) != 1 {
		t.Fatalf("insert calls/participants/receipts = %d/%d/%d, want 1/2/1", repo.insertCalls, len(repo.participants), len(repo.receipts))
	}
	existing := repo.participants["participant-1"]
	if existing.AttemptCount != 2 || existing.LatestAttemptID == nil || *existing.LatestAttemptID != "attempt-2" {
		t.Fatalf("existing attempt state changed: %+v", existing)
	}
}

func TestRosterServiceApplyRejectsPayloadMutation(t *testing.T) {
	repo := newRosterRepositoryFixture()
	svc := newRosterServiceForTest(repo)
	event := rosterEvent()
	if _, err := svc.Apply(context.Background(), event); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	event.AccessCode = "ABCDEF-MUTATED"
	if _, err := svc.Apply(context.Background(), event); !errors.Is(err, node_error.ErrRosterPayloadConflict) {
		t.Fatalf("mutated payload error = %v, want payload conflict", err)
	}
}

func TestRosterServiceApplyRejectsInvalidStateAndRevisionGap(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*fakeRosterRepository, *inbound.RosterEvent)
		wantStatus string
		wantErr    error
	}{
		{name: "wrong deployment", mutate: func(_ *fakeRosterRepository, event *inbound.RosterEvent) { event.DeploymentID = "other" }, wantStatus: string(entity.RosterEventRejected)},
		{name: "fenced", mutate: func(repo *fakeRosterRepository, _ *inbound.RosterEvent) { now := time.Now(); repo.exam.FencedAt = &now }, wantStatus: string(entity.RosterEventRejected)},
		{name: "expired", mutate: func(_ *fakeRosterRepository, event *inbound.RosterEvent) {
			event.Deadline = time.Now().Add(-time.Second)
		}, wantStatus: string(entity.RosterEventRejected)},
		{name: "revision gap", mutate: func(_ *fakeRosterRepository, event *inbound.RosterEvent) { event.Revision = 2 }, wantErr: node_error.ErrRosterRevisionGap},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRosterRepositoryFixture()
			event := rosterEvent()
			tc.mutate(repo, &event)
			outcome, err := newRosterServiceForTest(repo).Apply(context.Background(), event)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || outcome == nil || outcome.Status != tc.wantStatus {
				t.Fatalf("outcome/error = %+v/%v, want status %q", outcome, err, tc.wantStatus)
			}
		})
	}
}

func TestRosterServiceApplyRejectsAccessCodeConflictAndRollsBackReceipt(t *testing.T) {
	repo := newRosterRepositoryFixture()
	event := rosterEvent()
	event.AccessCode = "ABCDEF-OLD123"
	outcome, err := newRosterServiceForTest(repo).Apply(context.Background(), event)
	if err != nil || outcome == nil || outcome.Status != string(entity.RosterEventRejected) {
		t.Fatalf("outcome/error = %+v/%v, want rejected", outcome, err)
	}
	if len(repo.receipts) != 1 || len(repo.participants) != 1 {
		t.Fatalf("conflicting apply mutated participants/receipts: %d/%d", len(repo.participants), len(repo.receipts))
	}
}

func stringPtr(value string) *string { return &value }

var _ outbound.RosterRepository = (*fakeRosterRepository)(nil)
