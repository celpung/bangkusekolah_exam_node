package central

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

func TestBundleClientListsDeploymentsAndPullsBundleFromCentralEnvelope(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		var data interface{}
		switch r.URL.Path {
		case "/api/v1/exam-nodes/deployments":
			data = []Deployment{
				{ID: "dep-b", ExamID: "exam-b", ExamNodeID: "node-1", Status: "deployed"},
				{ID: "dep-a", ExamID: "exam-a", ExamNodeID: "node-1", Status: "harvesting"},
			}
		case "/api/v1/exam-nodes/deployments/dep-a/bundle":
			data = inbound.ExamNodeBundle{DeploymentID: "dep-a", Exam: inbound.ExamNodeBundleExam{ID: "exam-a"}}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "ok",
			"data":    data,
			"meta":    map[string]interface{}{},
		})
	}))
	defer server.Close()

	client := newBundleClient(server.URL, "node-token", server.Client())
	deployments, err := client.ListDeployments(t.Context())
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(deployments) != 2 || deployments[0].ExamID != "exam-a" || deployments[1].ExamID != "exam-b" {
		t.Fatalf("deployments = %+v, want deterministic exam-a/exam-b order", deployments)
	}
	bundle, err := client.PullBundle(t.Context(), "dep-a")
	if err != nil {
		t.Fatalf("PullBundle: %v", err)
	}
	if bundle.DeploymentID != "dep-a" || bundle.Exam.ID != "exam-a" {
		t.Fatalf("bundle = %+v, want dep-a/exam-a", bundle)
	}
	if gotAuth != "Bearer node-token" {
		t.Fatalf("Authorization = %q, want bearer node token", gotAuth)
	}
}

func TestBundleClientRejectsUnsuccessfulCentralEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "forbidden",
			"data":    nil,
		})
	}))
	defer server.Close()

	client := newBundleClient(server.URL, "node-token", server.Client())
	if _, err := client.ListDeployments(t.Context()); err == nil {
		t.Fatal("ListDeployments should reject an unsuccessful central envelope")
	}
}
