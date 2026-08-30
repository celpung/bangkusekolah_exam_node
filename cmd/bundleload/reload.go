package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"syscall"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
)

// notifyRuntimeReload asks an already-running examnode process to rebuild its
// in-memory content cache after bundleload replaced the database snapshot.
// Connection refused is safe: the process will rebuild from the database during
// startup. Any responding process must support the endpoint, otherwise the
// caller must stop rather than report a stale runtime as ready.
func notifyRuntimeReload(ctx context.Context, cfg *config.Config) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("node config is required")
	}
	port := cfg.HTTPPort
	if port == "" {
		port = "8080"
	}
	return notifyRuntimeReloadAt(
		ctx,
		"http://127.0.0.1:"+port+"/internal/v1/cache/reload",
		cfg.CentralNodeToken,
		&http.Client{Timeout: 15 * time.Second},
	)
}

func notifyRuntimeReloadAt(ctx context.Context, endpoint, nodeToken string, client *http.Client) (bool, error) {
	if endpoint == "" || nodeToken == "" {
		return false, fmt.Errorf("runtime reload endpoint and node token are required")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("build runtime reload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+nodeToken)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return false, nil
		}
		return false, fmt.Errorf("reload running examnode cache: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return false, fmt.Errorf("running examnode cache reload returned HTTP %d", resp.StatusCode)
	}
	return true, nil
}
