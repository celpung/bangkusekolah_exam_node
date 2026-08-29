package central

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// Deployment is the central metadata returned by node-token discovery.
type Deployment struct {
	ID                   string    `json:"id"`
	ExamID               string    `json:"exam_id"`
	ExamNodeID           string    `json:"exam_node_id"`
	Status               string    `json:"status"`
	BundleVersion        int       `json:"bundle_version"`
	BundleChecksum       string    `json:"bundle_checksum"`
	ParticipantCount     int       `json:"participant_count"`
	ItemCount            int       `json:"item_count"`
	ReportedAttemptCount int       `json:"reported_attempt_count"`
	DeployedAt           time.Time `json:"deployed_at"`
}

// BundleClient reads deployment metadata and bundles from central using the
// node's bearer token. It is used by the offline bundleload command only; the
// student runtime uses a separate node-local API surface.
type BundleClient struct {
	baseURL    string
	nodeToken  string
	httpClient *http.Client
}

func NewBundleClient(cfg *config.Config) *BundleClient {
	return newBundleClient(cfg.CentralBaseURL, cfg.CentralNodeToken, &http.Client{Timeout: 30 * time.Second})
}

func newBundleClient(baseURL, nodeToken string, httpClient *http.Client) *BundleClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &BundleClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		nodeToken:  nodeToken,
		httpClient: httpClient,
	}
}

func (c *BundleClient) ListDeployments(ctx context.Context) ([]Deployment, error) {
	var deployments []Deployment
	if err := c.request(ctx, http.MethodGet, "/api/v1/exam-nodes/deployments", &deployments); err != nil {
		return nil, fmt.Errorf("list central deployments: %w", err)
	}
	if deployments == nil {
		deployments = make([]Deployment, 0)
	}
	sort.Slice(deployments, func(i, j int) bool {
		if deployments[i].ExamID == deployments[j].ExamID {
			return deployments[i].ID < deployments[j].ID
		}
		return deployments[i].ExamID < deployments[j].ExamID
	})
	return deployments, nil
}

func (c *BundleClient) PullBundle(ctx context.Context, deploymentID string) (inbound.ExamNodeBundle, error) {
	if strings.TrimSpace(deploymentID) == "" {
		return inbound.ExamNodeBundle{}, fmt.Errorf("pull central bundle: deployment ID is required")
	}
	var bundle inbound.ExamNodeBundle
	path := "/api/v1/exam-nodes/deployments/" + url.PathEscape(deploymentID) + "/bundle"
	if err := c.request(ctx, http.MethodGet, path, &bundle); err != nil {
		return inbound.ExamNodeBundle{}, fmt.Errorf("pull central bundle %s: %w", deploymentID, err)
	}
	if bundle.DeploymentID != deploymentID {
		return inbound.ExamNodeBundle{}, fmt.Errorf("pull central bundle %s: response deployment is %q", deploymentID, bundle.DeploymentID)
	}
	return bundle, nil
}

func (c *BundleClient) request(ctx context.Context, method, path string, target interface{}) error {
	if c == nil || c.httpClient == nil {
		return fmt.Errorf("bundle client is not configured")
	}
	if c.baseURL == "" || c.nodeToken == "" {
		return fmt.Errorf("central base URL and node token are required")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(nil))
	if err != nil {
		return fmt.Errorf("build central request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("central request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("central request returned status %d", resp.StatusCode)
	}

	var envelope struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("decode central envelope: %w", err)
	}
	if !envelope.Success {
		if envelope.Message == "" {
			return fmt.Errorf("central response was unsuccessful")
		}
		return fmt.Errorf("central response: %s", envelope.Message)
	}
	if len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return fmt.Errorf("central response has no data")
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("decode central response data: %w", err)
	}
	return nil
}
