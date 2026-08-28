package central

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type PendingFence struct {
	ID         string `json:"id"`
	ExamID     string `json:"exam_id"`
	ExamNodeID string `json:"exam_node_id"`
	Status     string `json:"status"`
}

type FenceClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewFenceClient(baseURL, token string) *FenceClient {
	return &FenceClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{},
	}
}

func (c *FenceClient) ListPendingFences(ctx context.Context) ([]PendingFence, error) {
	var out []PendingFence
	if err := c.request(ctx, http.MethodGet, "/api/v1/exam-nodes/fences", "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *FenceClient) AcknowledgeFence(ctx context.Context, deploymentID string) error {
	return c.request(ctx, http.MethodPost, "/api/v1/exam-nodes/deployments/"+deploymentID+"/fence-ack", "", nil)
}

func (c *FenceClient) request(ctx context.Context, method, path, body string, target interface{}) error {
	if c.baseURL == "" || c.token == "" {
		return fmt.Errorf("central base URL and node token are required")
	}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("central fence request returned status %d", resp.StatusCode)
	}
	if target == nil {
		return nil
	}
	var envelope struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("decode central fence response: %w", err)
	}
	if !envelope.Success {
		return fmt.Errorf("central fence response: %s", envelope.Message)
	}
	if len(envelope.Data) == 0 {
		return fmt.Errorf("central fence response has no data")
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		return fmt.Errorf("decode central fence data: %w", err)
	}
	return nil
}
