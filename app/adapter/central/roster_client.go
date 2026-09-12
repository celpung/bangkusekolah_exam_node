package central

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

var errRosterClientInvalidRequest = errors.New("invalid roster client request")

const rosterProtocolVersion = inbound.RosterProtocolVersion

type RosterClient struct {
	baseURL    string
	nodeToken  string
	httpClient *http.Client
}

type rosterCapabilitiesResponse struct {
	RosterProtocolVersion int  `json:"roster_protocol_version"`
	RosterSupported       bool `json:"roster_supported"`
}

func NewRosterClient(cfg *config.Config) *RosterClient {
	return newRosterClient(cfg.CentralBaseURL, cfg.CentralNodeToken, &http.Client{Timeout: 10 * time.Second})
}

func newRosterClient(baseURL, nodeToken string, httpClient *http.Client) *RosterClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &RosterClient{baseURL: strings.TrimRight(baseURL, "/"), nodeToken: nodeToken, httpClient: httpClient}
}

func (c *RosterClient) PullPending(ctx context.Context) ([]inbound.RosterEvent, error) {
	var events []inbound.RosterEvent
	if err := c.request(ctx, http.MethodGet, "/api/v1/exam-nodes/roster-events?limit=50", nil, &events); err != nil {
		return nil, fmt.Errorf("pull roster events: %w", err)
	}
	if events == nil {
		events = []inbound.RosterEvent{}
	}
	return events, nil
}

func (c *RosterClient) Replay(ctx context.Context, deploymentID string, afterRevision int64) ([]inbound.RosterEvent, error) {
	if strings.TrimSpace(deploymentID) == "" || afterRevision < 0 {
		return nil, errRosterClientInvalidRequest
	}
	path := "/api/v1/exam-nodes/deployments/" + url.PathEscape(deploymentID) + "/roster-events?after_revision=" + fmt.Sprintf("%d", afterRevision) + "&limit=50"
	var events []inbound.RosterEvent
	if err := c.request(ctx, http.MethodGet, path, nil, &events); err != nil {
		return nil, fmt.Errorf("replay roster events %s: %w", deploymentID, err)
	}
	if events == nil {
		events = []inbound.RosterEvent{}
	}
	return events, nil
}

func (c *RosterClient) Acknowledge(ctx context.Context, outcome inbound.RosterOutcome) error {
	if strings.TrimSpace(outcome.EventID) == "" {
		return errRosterClientInvalidRequest
	}
	path := "/api/v1/exam-nodes/roster-events/" + url.PathEscape(outcome.EventID) + "/ack"
	// The event ID is carried in the URL. Keep it out of the request body so
	// the central decoder's strict schema accepts the acknowledgement payload.
	body := struct {
		DeploymentID string `json:"deployment_id"`
		Revision     int64  `json:"revision"`
		Status       string `json:"status"`
		Code         string `json:"code"`
	}{
		DeploymentID: outcome.DeploymentID,
		Revision:     outcome.Revision,
		Status:       outcome.Status,
		Code:         outcome.Code,
	}
	if err := c.request(ctx, http.MethodPost, path, body, nil); err != nil {
		return fmt.Errorf("acknowledge roster event %s: %w", outcome.EventID, err)
	}
	return nil
}

func (c *RosterClient) AnnounceCapabilities(ctx context.Context) error {
	var response rosterCapabilitiesResponse
	if err := c.request(ctx, http.MethodPost, "/api/v1/exam-nodes/capabilities", map[string]interface{}{
		"roster_protocol_version": rosterProtocolVersion,
		"roster_supported":        true,
	}, &response); err != nil {
		return fmt.Errorf("announce roster capabilities: %w", err)
	}
	if response.RosterProtocolVersion != rosterProtocolVersion || !response.RosterSupported {
		return fmt.Errorf("central did not enable roster protocol version %d", rosterProtocolVersion)
	}
	return nil
}

func (c *RosterClient) request(ctx context.Context, method, path string, body interface{}, target interface{}) error {
	if c == nil || c.httpClient == nil || c.baseURL == "" || c.nodeToken == "" {
		return fmt.Errorf("roster client is not configured")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal roster request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build roster request: %w", err)
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
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr == nil {
			if detail := centralErrorDetail(body); detail != "" {
				return fmt.Errorf("central request returned status %d: %s", resp.StatusCode, detail)
			}
		}
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

func centralErrorDetail(body []byte) string {
	var envelope struct {
		Message string `json:"message"`
		Error   string `json:"error"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	detail := strings.TrimSpace(envelope.Message)
	if detail == "" {
		detail = strings.TrimSpace(envelope.Error)
	}
	if detail == "" {
		return strings.TrimSpace(envelope.Code)
	}
	if code := strings.TrimSpace(envelope.Code); code != "" {
		return detail + " (" + code + ")"
	}
	return detail
}

var _ interface {
	PullPending(context.Context) ([]inbound.RosterEvent, error)
	Replay(context.Context, string, int64) ([]inbound.RosterEvent, error)
	Acknowledge(context.Context, inbound.RosterOutcome) error
	AnnounceCapabilities(context.Context) error
} = (*RosterClient)(nil)
