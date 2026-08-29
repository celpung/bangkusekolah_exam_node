package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/central"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

type bundleSource interface {
	ListDeployments(ctx context.Context) ([]central.Deployment, error)
	PullBundle(ctx context.Context, deploymentID string) (inbound.ExamNodeBundle, error)
}

type bundleLoader interface {
	LoadBundle(ctx context.Context, bundle inbound.ExamNodeBundle) error
}

// fetchBundles discovers and validates every live deployment before any bundle
// is written to the node database. A single invalid or failed bundle aborts the
// pull, while BundleService keeps each individual replacement transactional.
func fetchBundles(ctx context.Context, source bundleSource) ([]inbound.ExamNodeBundle, error) {
	deployments, err := source.ListDeployments(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(deployments, func(i, j int) bool {
		if deployments[i].ExamID == deployments[j].ExamID {
			return deployments[i].ID < deployments[j].ID
		}
		return deployments[i].ExamID < deployments[j].ExamID
	})

	bundles := make([]inbound.ExamNodeBundle, 0, len(deployments))
	for _, deployment := range deployments {
		if deployment.ID == "" || deployment.ExamID == "" {
			return nil, fmt.Errorf("central discovery returned an incomplete deployment")
		}
		if deployment.Status != "deployed" && deployment.Status != "harvesting" {
			return nil, fmt.Errorf("deployment %s has non-live status %q", deployment.ID, deployment.Status)
		}
		bundle, err := source.PullBundle(ctx, deployment.ID)
		if err != nil {
			return nil, err
		}
		if bundle.DeploymentID != deployment.ID {
			return nil, fmt.Errorf("deployment %s returned bundle for %s", deployment.ID, bundle.DeploymentID)
		}
		if bundle.Exam.ID != deployment.ExamID {
			return nil, fmt.Errorf("deployment %s returned bundle for exam %s, want %s", deployment.ID, bundle.Exam.ID, deployment.ExamID)
		}
		if bundle.Checksum == "" || service.ComputeBundleChecksum(bundle) != bundle.Checksum {
			return nil, fmt.Errorf("deployment %s returned an invalid bundle checksum", deployment.ID)
		}
		if deployment.BundleVersion != 0 && bundle.BundleVersion != deployment.BundleVersion {
			return nil, fmt.Errorf("deployment %s returned bundle version %d, want %d", deployment.ID, bundle.BundleVersion, deployment.BundleVersion)
		}
		if strings.TrimSpace(deployment.BundleChecksum) != "" && bundle.Checksum != deployment.BundleChecksum {
			return nil, fmt.Errorf("deployment %s returned checksum different from discovery metadata", deployment.ID)
		}
		bundles = append(bundles, bundle)
	}
	return bundles, nil
}

func pullAndLoad(ctx context.Context, source bundleSource, loader bundleLoader) (int, error) {
	bundles, err := fetchBundles(ctx, source)
	if err != nil {
		return 0, err
	}
	for _, bundle := range bundles {
		if err := loader.LoadBundle(ctx, bundle); err != nil {
			return 0, fmt.Errorf("load deployment %s: %w", bundle.DeploymentID, err)
		}
	}
	return len(bundles), nil
}
