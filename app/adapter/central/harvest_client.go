package central

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

// HarvestClient pushes finished attempt batches to central's ingest API.
// Timeout 10s: a batch is at most a few hundred attempts (~40 answers each),
// so failing fast and retrying on the next tick beats blocking the ticker.
// The client is not shared with student traffic, so slow central never
// backpressures login.
type HarvestClient struct {
	baseURL      string
	deploymentID string
	token        string
	httpClient   *http.Client
}

func NewHarvestClient(cfg *config.Config) *HarvestClient {
	return &HarvestClient{
		baseURL:      cfg.CentralBaseURL,
		deploymentID: cfg.DeploymentID,
		token:        cfg.CentralNodeToken,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Push posts one batch and returns central's per-attempt acknowledgement.
func (c *HarvestClient) Push(ctx context.Context, batch inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	raw, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("marshal batch: %w", err)
	}
	url := fmt.Sprintf("%s/api/v1/exam-nodes/deployments/%s/attempts", c.baseURL, c.deploymentID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("push to central: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("harvest push: status %d", resp.StatusCode)
	}
	var result inbound.ExamNodeIngestResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode ack: %w", err)
	}
	return &result, nil
}
