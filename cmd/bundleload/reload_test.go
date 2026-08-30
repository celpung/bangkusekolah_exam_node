package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotifyRuntimeReloadAtSendsNodeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/internal/v1/cache/reload" {
			t.Errorf("path = %s, want /internal/v1/cache/reload", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-node-token" {
			t.Errorf("authorization header = %q, want bearer node token", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	reloaded, err := notifyRuntimeReloadAt(context.Background(), server.URL+"/internal/v1/cache/reload", "test-node-token", server.Client())
	if err != nil {
		t.Fatalf("notifyRuntimeReloadAt() error = %v", err)
	}
	if !reloaded {
		t.Fatal("notifyRuntimeReloadAt() reloaded = false, want true")
	}
}

func TestNotifyRuntimeReloadAtRejectsRuntimeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	if _, err := notifyRuntimeReloadAt(context.Background(), server.URL, "test-node-token", server.Client()); err == nil {
		t.Fatal("notifyRuntimeReloadAt() error = nil, want runtime failure")
	}
}
