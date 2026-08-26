package router

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ReadinessReporter is the subset of ContentService state that readiness
// depends on: which exams' latest cache rebuild failed.
type ReadinessReporter interface {
	UnreadyExams() map[string]error
}

// NewReadinessRouter builds the operational health surface shared by
// cmd/examnode: /livez always 200, /readyz fails closed while any exam's
// content rebuild has failed. Extracted so the executable's readiness
// contract is directly testable.
func NewReadinessRouter(readiness ReadinessReporter, ping func() error) http.Handler {
	r := chi.NewRouter()
	r.Get("/livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if ping != nil {
			if err := ping(); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "unready", "error": "database unavailable"})
				return
			}
		}
		// Per-exam cache readiness: a live bundle push whose post-commit
		// rebuild failed leaves the exam unready until a retry succeeds.
		// Stable public reason only — detailed causes are logged server-side.
		if unready := readiness.UnreadyExams(); len(unready) > 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "unready", "unready_exams": len(unready)})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
	return r
}
