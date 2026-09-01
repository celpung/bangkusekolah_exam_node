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

type fakeNodeRepo struct {
	outbound_repository.NodeRepository
	exam         *entity.Exam
	participants map[string]*entity.Participant
	attempts     map[string]*entity.Attempt
	answers      map[string][]entity.Answer
	createCalls  int
	updateCalls  int
	activeErr    error
}

func (f *fakeNodeRepo) FindExamByID(_ context.Context, id string) (*entity.Exam, error) {
	if f.exam == nil || f.exam.ID != id {
		return nil, node_error.ErrExamNotLoaded
	}
	return f.exam, nil
}
func (f *fakeNodeRepo) FindParticipantByID(_ context.Context, id string) (*entity.Participant, error) {
	if p, ok := f.participants[id]; ok {
		return p, nil
	}
	return nil, node_error.ErrParticipantNotFound
}
func (f *fakeNodeRepo) FindParticipantByIDForUpdate(_ context.Context, id string) (*entity.Participant, error) {
	if p, ok := f.participants[id]; ok {
		return p, nil
	}
	return nil, node_error.ErrParticipantNotFound
}
func (f *fakeNodeRepo) FindActiveAttemptByParticipantAndExam(_ context.Context, pid, examID string) (*entity.Attempt, error) {
	if f.activeErr != nil {
		return nil, f.activeErr
	}
	for _, a := range f.attempts {
		if a.ParticipantID == pid && a.ExamID == examID && a.Status == entity.AttemptInProgress {
			return a, nil
		}
	}
	return nil, node_error.ErrAttemptNotFound
}
func (f *fakeNodeRepo) FindActiveAttemptByParticipant(_ context.Context, pid string) (*entity.Attempt, error) {
	if f.activeErr != nil {
		return nil, f.activeErr
	}
	for _, a := range f.attempts {
		if a.ParticipantID == pid && a.Status == entity.AttemptInProgress {
			return a, nil
		}
	}
	return nil, node_error.ErrAttemptNotFound
}
func (f *fakeNodeRepo) FindAttemptByID(_ context.Context, id string) (*entity.Attempt, error) {
	if a, ok := f.attempts[id]; ok {
		return a, nil
	}
	return nil, node_error.ErrAttemptNotFound
}
func (f *fakeNodeRepo) FindAttemptByIDForUpdate(_ context.Context, id string) (*entity.Attempt, error) {
	if a, ok := f.attempts[id]; ok {
		copied := *a
		return &copied, nil
	}
	return nil, node_error.ErrAttemptNotFound
}
func (f *fakeNodeRepo) ListAnswersByAttempt(_ context.Context, id string) ([]entity.Answer, error) {
	return f.answers[id], nil
}
func (f *fakeNodeRepo) CreateAttempt(_ context.Context, a *entity.Attempt) error {
	f.createCalls++
	f.attempts[a.ID] = a
	return nil
}
func (f *fakeNodeRepo) UpdateParticipant(_ context.Context, p *entity.Participant) error {
	f.updateCalls++
	f.participants[p.ID] = p
	return nil
}
func (f *fakeNodeRepo) CountAttemptsByParticipant(_ context.Context, pid string) (int, error) {
	n := 0
	for _, a := range f.attempts {
		if a.ParticipantID == pid {
			n++
		}
	}
	return n, nil
}

func (f *fakeNodeRepo) UpdateAttempt(_ context.Context, a *entity.Attempt) error {
	f.updateCalls++
	f.attempts[a.ID] = a
	return nil
}

type stubNodeTx struct{ fail bool }

func (s stubNodeTx) Atomic(_ context.Context, fn func(context.Context) error) error {
	if s.fail {
		return errors.New("tx failed")
	}
	return fn(context.Background())
}

type stubNodeID struct{ id string }

func (s stubNodeID) NewID() string { return s.id }

func nodeExam() *entity.Exam {
	now := time.Now()
	return &entity.Exam{
		ID: "exam-1", DeploymentID: "dep-1", Title: "UTS",
		StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour),
		DurationMinutes: 90, MaxAttempts: 1, MaxScore: 40, HasManualItems: false,
		AccessCodePrefix: "ABCDEF",
	}
}
func nodeParticipant() *entity.Participant {
	return &entity.Participant{ID: "part-1", ExamID: "exam-1", StudentID: "stu-1", StudentName: "Budi", AccessCode: "ABCDEF-GHIJKL", AttemptCount: 0}
}

func newAttemptService(repo *fakeNodeRepo) *AttemptService {
	return &AttemptService{repo: repo, txManager: stubNodeTx{}, idGen: stubNodeID{id: "att-new"}}
}

func TestStartAttemptCreatesDueAtAsMinDurationAndEndsAt(t *testing.T) {
	repo := &fakeNodeRepo{exam: nodeExam(), participants: map[string]*entity.Participant{"part-1": nodeParticipant()}, attempts: map[string]*entity.Attempt{}, answers: map[string][]entity.Answer{}}
	svc := newAttemptService(repo)
	before := time.Now()
	att, err := svc.StartAttempt(context.Background(), "part-1", "exam-1")
	if err != nil {
		t.Fatalf("StartAttempt: %v", err)
	}
	if !att.DueAt.Equal(repo.exam.EndsAt) {
		t.Fatalf("DueAt = %v, want ends_at %v (min of duration and window)", att.DueAt, repo.exam.EndsAt)
	}
	if att.Status != entity.AttemptInProgress || att.AttemptNo != 1 || att.MaxScore != 40 {
		t.Fatalf("attempt = %+v", att)
	}
	if before.After(att.StartedAt) || att.StartedAt.After(time.Now()) {
		t.Fatalf("StartedAt not in window: %v", att.StartedAt)
	}
	if repo.participants["part-1"].AttemptCount != 1 || repo.participants["part-1"].LatestAttemptID == nil {
		t.Fatalf("participant counters not bumped: %+v", repo.participants["part-1"])
	}
}

func TestStartAttemptResumesActiveAttempt(t *testing.T) {
	existing := &entity.Attempt{ID: "att-old", ParticipantID: "part-1", ExamID: "exam-1", StudentID: "stu-1", AttemptNo: 1, Status: entity.AttemptInProgress, StartedAt: time.Now().Add(-10 * time.Minute), DueAt: time.Now().Add(80 * time.Minute), MaxScore: 40}
	repo := &fakeNodeRepo{exam: nodeExam(), participants: map[string]*entity.Participant{"part-1": {ID: "part-1", StudentID: "stu-1", ExamID: "exam-1", AttemptCount: 1, LatestAttemptID: &existing.ID}}, attempts: map[string]*entity.Attempt{"att-old": existing}, answers: map[string][]entity.Answer{}}
	svc := newAttemptService(repo)
	att, err := svc.StartAttempt(context.Background(), "part-1", "exam-1")
	if err != nil {
		t.Fatalf("StartAttempt: %v", err)
	}
	if att.ID != "att-old" || repo.createCalls != 0 {
		t.Fatalf("resume must return existing attempt without insert: got %q calls %d", att.ID, repo.createCalls)
	}
}

func TestStartAttemptAutoSubmitsExpiredActiveAttemptBeforeCreatingNext(t *testing.T) {
	existing := &entity.Attempt{
		ID: "att-old", ParticipantID: "part-1", ExamID: "exam-1", StudentID: "stu-1",
		AttemptNo: 1, Status: entity.AttemptInProgress, StartedAt: time.Now().Add(-2 * time.Hour),
		DueAt: time.Now().Add(-time.Minute), MaxScore: 40,
	}
	score := 12.0
	repo := &fakeNodeRepo{
		exam: nodeExam(),
		participants: map[string]*entity.Participant{
			"part-1": {ID: "part-1", StudentID: "stu-1", ExamID: "exam-1", AttemptCount: 1, LatestAttemptID: &existing.ID},
		},
		attempts: map[string]*entity.Attempt{"att-old": existing},
		answers:  map[string][]entity.Answer{"att-old": {{ID: "ans-1", AttemptID: "att-old", Score: &score, GradingStatus: entity.GradingAutoGraded}}},
	}
	repo.exam.MaxAttempts = 2
	svc := newAttemptService(repo)

	started, err := svc.StartAttempt(context.Background(), "part-1", "exam-1")
	if err != nil {
		t.Fatalf("StartAttempt: %v", err)
	}
	if started.ID != "att-new" || started.AttemptNo != 2 || started.Status != entity.AttemptInProgress {
		t.Fatalf("new attempt = %+v", started)
	}
	old := repo.attempts["att-old"]
	if old.Status != entity.AttemptAutoSubmitted || old.AutoSubmittedAt == nil {
		t.Fatalf("expired attempt was not auto-submitted: %+v", old)
	}
	if old.Score == nil || *old.Score != score {
		t.Fatalf("expired attempt score = %v, want %v", old.Score, score)
	}
	if repo.participants["part-1"].AttemptCount != 2 {
		t.Fatalf("attempt count = %d, want 2", repo.participants["part-1"].AttemptCount)
	}
}

func TestStartAttemptAutoSubmitsExpiredActiveBeforeMaxAttemptsError(t *testing.T) {
	existing := &entity.Attempt{
		ID: "att-old", ParticipantID: "part-1", ExamID: "exam-1", StudentID: "stu-1",
		AttemptNo: 1, Status: entity.AttemptInProgress, StartedAt: time.Now().Add(-2 * time.Hour),
		DueAt: time.Now().Add(-time.Minute), MaxScore: 40,
	}
	repo := &fakeNodeRepo{
		exam: nodeExam(),
		participants: map[string]*entity.Participant{
			"part-1": {ID: "part-1", StudentID: "stu-1", ExamID: "exam-1", AttemptCount: 1, LatestAttemptID: &existing.ID},
		},
		attempts: map[string]*entity.Attempt{"att-old": existing},
		answers:  map[string][]entity.Answer{},
	}
	svc := newAttemptService(repo)

	_, err := svc.StartAttempt(context.Background(), "part-1", "exam-1")
	if !errors.Is(err, node_error.ErrMaxAttemptsReached) {
		t.Fatalf("error = %v, want max attempts", err)
	}
	if repo.attempts["att-old"].Status != entity.AttemptAutoSubmitted {
		t.Fatalf("expired attempt status = %q, want auto_submitted", repo.attempts["att-old"].Status)
	}
}

func TestGetAttemptStateAutoSubmitsExpiredAttempt(t *testing.T) {
	existing := &entity.Attempt{
		ID: "att-old", ParticipantID: "part-1", ExamID: "exam-1", StudentID: "stu-1",
		AttemptNo: 1, Status: entity.AttemptInProgress, StartedAt: time.Now().Add(-2 * time.Hour),
		DueAt: time.Now().Add(-time.Minute), MaxScore: 40,
	}
	repo := &fakeNodeRepo{
		exam:         nodeExam(),
		participants: map[string]*entity.Participant{"part-1": nodeParticipant()},
		attempts:     map[string]*entity.Attempt{"att-old": existing},
		answers:      map[string][]entity.Answer{},
	}
	svc := newAttemptService(repo)

	state, err := svc.GetAttemptState(context.Background(), "part-1", "att-old")
	if err != nil {
		t.Fatalf("GetAttemptState: %v", err)
	}
	if state.Attempt.Status != entity.AttemptAutoSubmitted || state.Attempt.AutoSubmittedAt == nil {
		t.Fatalf("state attempt = %+v, want auto_submitted", state.Attempt)
	}
}

func TestStartAttemptWithDeviceStoresAndResumesOnlyOnTheSameDevice(t *testing.T) {
	repo := &fakeNodeRepo{
		exam:         nodeExam(),
		participants: map[string]*entity.Participant{"part-1": nodeParticipant()},
		attempts:     map[string]*entity.Attempt{},
		answers:      map[string][]entity.Answer{},
	}
	svc := newAttemptService(repo)

	created, err := svc.StartAttemptWithDevice(context.Background(), "part-1", "exam-1", "install-1")
	if err != nil {
		t.Fatalf("device-bound start: %v", err)
	}
	if created.DeviceID != "install-1" {
		t.Fatalf("created device binding = %q, want install-1", created.DeviceID)
	}

	resumed, err := svc.StartAttemptWithDevice(context.Background(), "part-1", "exam-1", "install-1")
	if err != nil || resumed.ID != created.ID {
		t.Fatalf("same-device resume = (%v, %v), want %q", resumed, err, created.ID)
	}
	if _, err := svc.StartAttemptWithDevice(context.Background(), "part-1", "exam-1", "install-2"); !errors.Is(err, node_error.ErrAttemptDeviceMismatch) {
		t.Fatalf("different-device resume error = %v, want device mismatch", err)
	}
}

func TestStartAttemptWithDeviceRequiresDeviceID(t *testing.T) {
	repo := &fakeNodeRepo{}
	_, err := newAttemptService(repo).StartAttemptWithDevice(
		context.Background(), "part-1", "exam-1", "  ",
	)
	if !errors.Is(err, node_error.ErrAttemptDeviceIDInvalid) {
		t.Fatalf("empty device id error = %v, want ErrAttemptDeviceIDInvalid", err)
	}
}

func TestStartAttemptRejectedWhenWindowClosed(t *testing.T) {
	exam := nodeExam()
	exam.StartsAt = time.Now().Add(time.Hour)
	repo := &fakeNodeRepo{exam: exam, participants: map[string]*entity.Participant{"part-1": nodeParticipant()}, attempts: map[string]*entity.Attempt{}}
	svc := newAttemptService(repo)
	if _, err := svc.StartAttempt(context.Background(), "part-1", "exam-1"); !errors.Is(err, node_error.ErrExamNotOpen) {
		t.Fatalf("want ErrExamNotOpen, got %v", err)
	}
}

func TestStartAttemptRejectedWhenMaxAttemptsReached(t *testing.T) {
	p := nodeParticipant()
	p.AttemptCount = 1
	repo := &fakeNodeRepo{exam: nodeExam(), participants: map[string]*entity.Participant{"part-1": p}, attempts: map[string]*entity.Attempt{}}
	svc := newAttemptService(repo)
	if _, err := svc.StartAttempt(context.Background(), "part-1", "exam-1"); !errors.Is(err, node_error.ErrMaxAttemptsReached) {
		t.Fatalf("want ErrMaxAttemptsReached, got %v", err)
	}
}

func TestGetAttemptStateReturnsAnswersAndServerTime(t *testing.T) {
	att := &entity.Attempt{ID: "att-1", ParticipantID: "part-1", ExamID: "exam-1", StudentID: "stu-1", Status: entity.AttemptInProgress, DueAt: time.Now().Add(time.Hour)}
	repo := &fakeNodeRepo{exam: nodeExam(), participants: map[string]*entity.Participant{"part-1": nodeParticipant()}, attempts: map[string]*entity.Attempt{"att-1": att}, answers: map[string][]entity.Answer{"att-1": {{ID: "ans-1", AttemptID: "att-1", ItemID: "item-1"}}}}
	svc := newAttemptService(repo)
	before := time.Now()
	state, err := svc.GetAttemptState(context.Background(), "part-1", "att-1")
	if err != nil {
		t.Fatalf("GetAttemptState: %v", err)
	}
	if len(state.Answers) != 1 || state.Attempt.ID != "att-1" {
		t.Fatalf("state = %+v", state)
	}
	if state.ServerTime.Before(before) || state.ServerTime.After(time.Now().Add(time.Second)) {
		t.Fatalf("server_time not now: %v", state.ServerTime)
	}
}

func TestGetAttemptStateRejectsWrongOwner(t *testing.T) {
	att := &entity.Attempt{ID: "att-1", ParticipantID: "part-1", ExamID: "exam-1", StudentID: "stu-1", Status: entity.AttemptInProgress}
	repo := &fakeNodeRepo{exam: nodeExam(), participants: map[string]*entity.Participant{"part-1": nodeParticipant()}, attempts: map[string]*entity.Attempt{"att-1": att}}
	svc := newAttemptService(repo)
	if _, err := svc.GetAttemptState(context.Background(), "part-2", "att-1"); !errors.Is(err, node_error.ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

func TestStartAttemptPropagatesDBErrorOnActiveLookup(t *testing.T) {
	repo := &fakeNodeRepo{exam: nodeExam(), participants: map[string]*entity.Participant{"part-1": nodeParticipant()}, attempts: map[string]*entity.Attempt{}, activeErr: errors.New("db unavailable")}
	svc := newAttemptService(repo)
	_, err := svc.StartAttempt(context.Background(), "part-1", "exam-1")
	if err == nil || err.Error() != "db unavailable" {
		t.Fatalf("want db unavailable, got %v", err)
	}
	if repo.createCalls != 0 || repo.updateCalls != 0 {
		t.Fatalf("must not write on db read failure: create %d update %d", repo.createCalls, repo.updateCalls)
	}
}
