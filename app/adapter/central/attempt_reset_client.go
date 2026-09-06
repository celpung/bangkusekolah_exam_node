package central

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type AttemptResetClient struct {
	baseURL    string
	nodeToken  string
	httpClient *http.Client
}

const attemptResetProtocolVersion = 1

type attemptResetCapabilitiesResponse struct {
	AttemptResetProtocolVersion int  `json:"attempt_reset_protocol_version"`
	AttemptResetSupported       bool `json:"attempt_reset_supported"`
}

func NewAttemptResetClient(cfg *config.Config) *AttemptResetClient {
	return newAttemptResetClient(cfg.CentralBaseURL, cfg.CentralNodeToken, &http.Client{Timeout: 10 * time.Second})
}

func newAttemptResetClient(baseURL, nodeToken string, httpClient *http.Client) *AttemptResetClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &AttemptResetClient{baseURL: strings.TrimRight(baseURL, "/"), nodeToken: nodeToken, httpClient: httpClient}
}

func (c *AttemptResetClient) PullAttemptResetCommands(ctx context.Context) ([]inbound.AttemptResetCommand, error) {
	var commands []inbound.AttemptResetCommand
	if err := c.request(ctx, http.MethodGet, "/api/v1/exam-nodes/attempt-reset-commands", nil, &commands); err != nil {
		return nil, fmt.Errorf("pull attempt reset commands: %w", err)
	}
	if commands == nil {
		commands = make([]inbound.AttemptResetCommand, 0)
	}
	return commands, nil
}

func (c *AttemptResetClient) AnnounceAttemptResetCapabilities(ctx context.Context) error {
	var response attemptResetCapabilitiesResponse
	if err := c.request(ctx, http.MethodPost, "/api/v1/exam-nodes/capabilities", map[string]interface{}{
		"attempt_reset_protocol_version": attemptResetProtocolVersion,
		"attempt_reset_supported":        true,
	}, &response); err != nil {
		return fmt.Errorf("announce attempt reset capabilities: %w", err)
	}
	if response.AttemptResetProtocolVersion != attemptResetProtocolVersion || !response.AttemptResetSupported {
		return fmt.Errorf("central did not enable attempt reset protocol version %d", attemptResetProtocolVersion)
	}
	return nil
}

func (c *AttemptResetClient) ReportAttemptResetOutcome(ctx context.Context, outcome inbound.AttemptResetOutcome) error {
	if strings.TrimSpace(outcome.RequestID) == "" {
		return fmt.Errorf("report attempt reset outcome: request ID is required")
	}
	path := "/api/v1/exam-nodes/attempt-reset-commands/" + url.PathEscape(outcome.RequestID) + "/outcome"
	if err := c.request(ctx, http.MethodPost, path, outcome, nil); err != nil {
		return fmt.Errorf("report attempt reset outcome %s: %w", outcome.RequestID, err)
	}
	return nil
}

func (c *AttemptResetClient) request(ctx context.Context, method, path string, body interface{}, target interface{}) error {
	if c == nil || c.httpClient == nil {
		return fmt.Errorf("attempt reset client is not configured")
	}
	if c.baseURL == "" || c.nodeToken == "" {
		return fmt.Errorf("central base URL and node token are required")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.nodeToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("central request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("central request returned status %d", resp.StatusCode)
	}
	if target == nil {
		return nil
	}
	var envelope struct {
		Success *bool           `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&envelope); err != nil {
		return fmt.Errorf("decode central response: %w", err)
	}
	if envelope.Success != nil && !*envelope.Success {
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

var _ interface {
	PullAttemptResetCommands(context.Context) ([]inbound.AttemptResetCommand, error)
	ReportAttemptResetOutcome(context.Context, inbound.AttemptResetOutcome) error
	AnnounceAttemptResetCapabilities(context.Context) error
} = (*AttemptResetClient)(nil)
