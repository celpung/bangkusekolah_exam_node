package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// NodeTokenAuth guards the internal (central → node) routes. It accepts only
// the shared node token from config — a student JWT is never valid here.
func NodeTokenAuth(nodeToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := strings.TrimSpace(r.Header.Get("Authorization"))
			if !strings.HasPrefix(raw, "Bearer ") {
				http.Error(w, "missing node token", http.StatusUnauthorized)
				return
			}
			token := strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
			if nodeToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(nodeToken)) != 1 {
				http.Error(w, "invalid node token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
