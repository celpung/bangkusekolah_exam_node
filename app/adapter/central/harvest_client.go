package central

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

// Push posts one batch scoped to a single deployment and returns central's
// per-attempt acknowledgement. Trailing slashes in CentralBaseURL are
// normalized so the path never becomes "//api/...".
func (c *HarvestClient) Push(ctx context.Context, deploymentID string, batch inbound.ExamNodeAttemptBatch) (*inbound.ExamNodeIngestResult, error) {
	if deploymentID == "" {
		return nil, fmt.Errorf("harvest push: empty deployment ID")
	}
	raw, err := json.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("marshal batch: %w", err)
	}
	base := strings.TrimRight(c.baseURL, "/")
	url := fmt.Sprintf("%s/api/v1/exam-nodes/deployments/%s/attempts", base, deploymentID)
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ack: %w", err)
	}
	var envelope struct {
		Success *bool           `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode ack: %w", err)
	}
	if envelope.Success != nil || envelope.Data != nil {
		if envelope.Success != nil && !*envelope.Success {
			if envelope.Message == "" {
				return nil, fmt.Errorf("central rejected harvest")
			}
			return nil, fmt.Errorf("central rejected harvest: %s", envelope.Message)
		}
		if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			return nil, fmt.Errorf("central harvest acknowledgement has no data")
		}
		var result inbound.ExamNodeIngestResult
		if err := json.Unmarshal(envelope.Data, &result); err != nil {
			return nil, fmt.Errorf("decode envelope ack data: %w", err)
		}
		return &result, nil
	}

	// Keep compatibility with older test/proxy responses that returned the ack
	// object without the central response envelope.
	var result inbound.ExamNodeIngestResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode ack: %w", err)
	}
	return &result, nil
}
