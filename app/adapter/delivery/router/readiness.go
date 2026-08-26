package router

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ReadinessReporter is the subset of ContentService state that readiness
// depends on.
type ReadinessReporter interface {
	// UnreadyExams lists exams whose latest cache rebuild failed.
	UnreadyExams() map[string]error
	// CacheReadyExams lists exams whose content cache is built.
	CacheReadyExams() []string
}

// NewReadinessRouter builds the operational health surface shared by
// cmd/examnode: /livez always 200, /readyz fails closed unless at least one
// exam is loaded AND every persisted exam has a successfully built cache.
func NewReadinessRouter(readiness ReadinessReporter, loadedExamCount func() (int, error), ping func() error) http.Handler {
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
		// Fail closed: a node with no loaded bundle cannot serve students.
		loaded, err := loadedExamCount()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "unready", "error": "cannot count exams"})
			return
		}
		if loaded == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "unready", "reason": "no exams loaded"})
			return
		}
		// Every persisted exam needs a built cache. readyInDB counts cached
		// IDs that are still persisted (a replaced exam leaves a stale cache
		// entry behind until its rebuild overwrites it).
		persisted := make(map[string]struct{}, loaded)
		for _, id := range readiness.CacheReadyExams() {
			persisted[id] = struct{}{}
		}
		unready := readiness.UnreadyExams()
		notReady := 0
		for id := range unready {
			delete(persisted, id)
		}
		// After removing unready IDs, whatever remains in the cached set that
		// is not verifiable against persistence cannot be trusted; readiness
		// requires unready empty and at least one cached exam.
		notReady = len(unready)
		if notReady > 0 || len(persisted) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "unready", "reason": "exams awaiting content rebuild"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
	return r
}
