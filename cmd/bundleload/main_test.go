package main

import (
	"context"
	"testing"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/central"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

type fakeBundleSource struct {
	deployments []central.Deployment
	bundles     map[string]inbound.ExamNodeBundle
	pulled      []string
}

func (f *fakeBundleSource) ListDeployments(context.Context) ([]central.Deployment, error) {
	return f.deployments, nil
}

func (f *fakeBundleSource) PullBundle(_ context.Context, deploymentID string) (inbound.ExamNodeBundle, error) {
	f.pulled = append(f.pulled, deploymentID)
	return f.bundles[deploymentID], nil
}

type fakeBundleLoader struct {
	loaded []inbound.ExamNodeBundle
}

func (f *fakeBundleLoader) LoadBundle(_ context.Context, bundle inbound.ExamNodeBundle) error {
	f.loaded = append(f.loaded, bundle)
	return nil
}

func validBundle(deploymentID, examID string) inbound.ExamNodeBundle {
	bundle := inbound.ExamNodeBundle{
		BundleVersion: 1,
		DeploymentID:  deploymentID,
		Exam:          inbound.ExamNodeBundleExam{ID: examID},
	}
	bundle.Checksum = service.ComputeBundleChecksum(bundle)
	return bundle
}

func TestPullAndLoadLoadsEveryDeploymentInDeterministicOrder(t *testing.T) {
	source := &fakeBundleSource{
		deployments: []central.Deployment{
			{ID: "dep-b", ExamID: "exam-b", Status: "deployed"},
			{ID: "dep-a", ExamID: "exam-a", Status: "harvesting"},
		},
		bundles: map[string]inbound.ExamNodeBundle{
			"dep-a": validBundle("dep-a", "exam-a"),
			"dep-b": validBundle("dep-b", "exam-b"),
		},
	}
	loader := &fakeBundleLoader{}

	loaded, err := pullAndLoad(t.Context(), source, loader)
	if err != nil {
		t.Fatalf("pullAndLoad: %v", err)
	}
	if loaded != 2 {
		t.Fatalf("loaded = %d, want 2", loaded)
	}
	if len(source.pulled) != 2 || source.pulled[0] != "dep-a" || source.pulled[1] != "dep-b" {
		t.Fatalf("pulled = %v, want [dep-a dep-b]", source.pulled)
	}
	if len(loader.loaded) != 2 || loader.loaded[0].DeploymentID != "dep-a" || loader.loaded[1].DeploymentID != "dep-b" {
		t.Fatalf("loaded bundles = %+v, want dep-a/dep-b", loader.loaded)
	}
}

func TestPullAndLoadRejectsBundleDeploymentMismatchBeforeLoading(t *testing.T) {
	source := &fakeBundleSource{
		deployments: []central.Deployment{{ID: "dep-a", ExamID: "exam-a", Status: "deployed"}},
		bundles: map[string]inbound.ExamNodeBundle{
			"dep-a": {DeploymentID: "dep-other", Exam: inbound.ExamNodeBundleExam{ID: "exam-a"}},
		},
	}
	loader := &fakeBundleLoader{}

	if _, err := pullAndLoad(t.Context(), source, loader); err == nil {
		t.Fatal("pullAndLoad should reject a bundle with a mismatched deployment ID")
	}
	if len(loader.loaded) != 0 {
		t.Fatalf("loaded = %d, want zero after validation failure", len(loader.loaded))
	}
}
