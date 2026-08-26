package service

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	nodecentral "github.com/celpung/bangkusekolah_exam_node/app/adapter/central"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"

	"net/http"
	"net/http/httptest"
)

type fakeHarvestRepo struct {
	outbound_repository.NodeRepository
	attempts map[string]*entity.Attempt
	exams    map[string]*entity.Exam
	answers  map[string][]entity.Answer
	events   map[string][]entity.IntegrityEvent
	pushLog  []string // failure log entries "attempt|dep|count|err"
}

func (f *fakeHarvestRepo) ListUnpushedAttempts(_ context.Context) ([]entity.Attempt, error) {
	var out []entity.Attempt
	for _, a := range f.attempts {
		if a.HarvestedAt == nil && (a.Status == entity.AttemptSubmitted || a.Status == entity.AttemptAutoSubmitted) {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (f *fakeHarvestRepo) FindExamByID(_ context.Context, id string) (*entity.Exam, error) {
	if e, ok := f.exams[id]; ok {
		return e, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeHarvestRepo) ListAnswersByAttempt(_ context.Context, id string) ([]entity.Answer, error) {
	return f.answers[id], nil
}

func (f *fakeHarvestRepo) ListIntegrityEventsByAttempt(_ context.Context, id string) ([]entity.IntegrityEvent, error) {
	return f.events[id], nil
}

func (f *fakeHarvestRepo) MarkAttemptsHarvested(_ context.Context, ids []string, at time.Time) (int, error) {
	marked := 0
	for _, id := range ids {
		if a, ok := f.attempts[id]; ok && a.HarvestedAt == nil &&
			(a.Status == entity.AttemptSubmitted || a.Status == entity.AttemptAutoSubmitted) {
			a.HarvestedAt = &at
			marked++
		}
	}
	return marked, nil
}

func (f *fakeHarvestRepo) LogHarvestFailure(_ context.Context, attemptID, deploymentID string, attemptsCount int, errMsg string) error {
	f.pushLog = append(f.pushLog, attemptID+"|"+deploymentID+"|"+itoa(attemptsCount)+"|"+errMsg)
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

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

// newTestHarvestClient constructs the real central.HarvestClient pointed at
// the httptest server URL — loopback only, no external network.
func newTestHarvestClient(baseURL, token, deploymentID string) *nodecentral.HarvestClient {
	return nodecentral.NewHarvestClient(&config.Config{
		CentralBaseURL:   baseURL,
		CentralNodeToken: token,
		DeploymentID:     deploymentID,
	})
}

func centralMock(t *testing.T, wantToken string, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+wantToken {
			t.Errorf("Authorization = %q, want Bearer %q", got, wantToken)
		}
		handler(w, r)
	}))
}
