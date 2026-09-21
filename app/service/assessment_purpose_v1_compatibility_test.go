package service

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

func TestAssessmentPurposeV1FixtureHasStableChecksumAndNoPurposeField(t *testing.T) {
	data, err := os.ReadFile("../../testdata/assessment-purpose-v1-bundle.json")
	if err != nil {
		t.Fatalf("read v1 fixture: %v", err)
	}
	var bundle inbound.ExamNodeBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("decode v1 fixture: %v", err)
	}
	if bundle.BundleVersion != 1 {
		t.Fatalf("bundle version = %d, want 1", bundle.BundleVersion)
	}
	if bundle.Checksum == "" {
		t.Fatal("fixture must contain the checksum captured from Central")
	}
	if strings.Contains(string(data), "assessment_purpose") {
		t.Fatal("v1 fixture must not add assessment_purpose to the node payload")
	}
	if got := ComputeBundleChecksum(bundle); got != bundle.Checksum {
		t.Fatalf("fixture checksum = %q, recomputed = %q", bundle.Checksum, got)
	}
}

func TestAssessmentPurposeV1FixtureHarvestsAttemptDataForDiagnosticAndFormative(t *testing.T) {
	data, err := os.ReadFile("../../testdata/assessment-purpose-v1-bundle.json")
	if err != nil {
		t.Fatalf("read v1 fixture: %v", err)
	}
	var bundle inbound.ExamNodeBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("decode v1 fixture: %v", err)
	}

	for _, purpose := range []string{"DIAGNOSTIC", "FORMATIVE"} {
		t.Run(purpose, func(t *testing.T) {
			submitted := time.Now()
			repo := &fakeHarvestRepo{
				exams: map[string]*entity.Exam{
					bundle.Exam.ID: {ID: bundle.Exam.ID, DeploymentID: bundle.DeploymentID},
				},
				attempts: map[string]*entity.Attempt{
					"attempt-compat-1": {ID: "attempt-compat-1", ExamID: bundle.Exam.ID, ParticipantID: "part-compat-1", StudentID: "student-compat-1", Status: entity.AttemptSubmitted, SubmittedAt: &submitted},
				},
			}
			server := centralMock(t, "node-token", func(w http.ResponseWriter, r *http.Request) {
				var batch inbound.ExamNodeAttemptBatch
				if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
					t.Errorf("decode harvest batch: %v", err)
				}
				if len(batch.Attempts) != 1 || batch.Attempts[0].ID != "attempt-compat-1" || batch.Attempts[0].StudentID != "student-compat-1" {
					t.Errorf("harvest batch for %s = %+v", purpose, batch)
				}
				_ = json.NewEncoder(w).Encode(inbound.ExamNodeIngestResult{AcceptedAttemptIDs: []string{"attempt-compat-1"}})
			})
			defer server.Close()

			svc := NewHarvestService(repo, newTestHarvestClient(server.URL, "node-token", bundle.DeploymentID))
			if n, err := svc.DrainOnce(context.Background()); err != nil || n != 1 {
				t.Fatalf("harvest %s: n=%d err=%v", purpose, n, err)
			}
			if repo.attempts["attempt-compat-1"].HarvestedAt == nil {
				t.Fatalf("attempt for %s was not marked harvested", purpose)
			}
		})
	}
}
