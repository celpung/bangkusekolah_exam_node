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
// cmd/examnode: /livez always 200, /readyz fails closed unless the persisted
// exam ID set exactly equals the cache-ready exam ID set (and is non-empty).
func NewReadinessRouter(readiness ReadinessReporter, loadedExamIDs func() ([]string, error), ping func() error) http.Handler {
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
		persistedIDs, err := loadedExamIDs()
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "unready", "error": "cannot list exams"})
			return
		}
		// Fail closed: a node with no loaded bundle cannot serve students.
		if len(persistedIDs) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "unready", "reason": "no exams loaded"})
			return
		}
		// Exact set comparison: persisted IDs must equal cache-ready IDs and
		// no exam may be in the unready set. This catches a partially built
		// cache, an unready rebuild, AND stale cache entries for exams that
		// were replaced or deleted.
		cached := make(map[string]struct{}, len(readiness.CacheReadyExams()))
		for _, id := range readiness.CacheReadyExams() {
			cached[id] = struct{}{}
		}
		unready := readiness.UnreadyExams()
		if len(unready) > 0 || !sameSet(persistedIDs, cached) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "unready", "reason": "exams awaiting content rebuild"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
	return r
}

// sameSet reports whether both slices contain exactly the same string IDs,
// ignoring order and duplicates.
func sameSet(a []string, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	setA := make(map[string]struct{}, len(a))
	for _, id := range a {
		setA[id] = struct{}{}
	}
	for id := range b {
		if _, ok := setA[id]; !ok {
			return false
		}
	}
	return true
}
