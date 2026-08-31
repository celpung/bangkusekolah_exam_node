package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
)

type stubIssuer struct {
	claims *outbound.JWTClaims
	err    error
}

func (s *stubIssuer) Issue(_ context.Context, _, _, _, _ string) (string, error) { return "tok", nil }
func (s *stubIssuer) Parse(_ context.Context, _ string) (*outbound.JWTClaims, error) {
	return s.claims, s.err
}

func runAuth(t *testing.T, issuer outbound.JWTIssuer, header string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var captured string
	h := AuthMiddleware(issuer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = ParticipantIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exam-attempts/att-1", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, captured
}

func TestAuthMiddlewareInjectsParticipantIDFromValidToken(t *testing.T) {
	claims := &outbound.JWTClaims{ParticipantID: "part-1", StudentID: "stu-1", ExamID: "exam-1", DeploymentID: "dep-1", ExpiresAt: 9999999999, IssuedAt: 1}
	rec, pid := runAuth(t, &stubIssuer{claims: claims}, "Bearer valid.token")
	if rec.Code != http.StatusOK || pid != "part-1" {
		t.Fatalf("code=%d pid=%q", rec.Code, pid)
	}
}

func TestAuthMiddlewareRejectsMissingBearerToken(t *testing.T) {
	rec, _ := runAuth(t, &stubIssuer{claims: &outbound.JWTClaims{ParticipantID: "part-1"}}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: code=%d want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing bearer token") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestAuthMiddlewareRejectsNonBearerScheme(t *testing.T) {
	rec, _ := runAuth(t, &stubIssuer{claims: &outbound.JWTClaims{ParticipantID: "part-1"}}, "Basic dXNlcjpwYXNz")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("basic scheme: code=%d want 401", rec.Code)
	}
}

func TestAuthMiddlewareRejectsInvalidToken(t *testing.T) {
	rec, _ := runAuth(t, &stubIssuer{err: errStubParse}, "Bearer bad.token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token: code=%d want 401", rec.Code)
	}
}

func TestAuthMiddlewareRejectsTokenWithoutParticipantID(t *testing.T) {
	rec, _ := runAuth(t, &stubIssuer{claims: &outbound.JWTClaims{}}, "Bearer valid.token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty pid: code=%d want 401", rec.Code)
	}
}

func TestAuthMiddlewareRejectsTokenWithoutDeploymentID(t *testing.T) {
	claims := &outbound.JWTClaims{ParticipantID: "part-1", StudentID: "stu-1", ExamID: "exam-1", ExpiresAt: 999, IssuedAt: 1}
	rec, _ := runAuth(t, &stubIssuer{claims: claims}, "Bearer valid.token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing deployment_id: code=%d want 401", rec.Code)
	}
}

var errStubParse = &parseError{}

type parseError struct{}

func (*parseError) Error() string { return "parse failed" }

type fenceChecker struct{ fenced bool }

func (f fenceChecker) MarkDeploymentFenced(context.Context, string, time.Time) error { return nil }
func (f fenceChecker) IsDeploymentFenced(context.Context, string, string) (bool, error) {
	return f.fenced, nil
}

func TestAuthMiddlewareRejectsFencedDeployment(t *testing.T) {
	claims := &outbound.JWTClaims{ParticipantID: "part-1", StudentID: "stu-1", ExamID: "exam-1", DeploymentID: "dep-1", ExpiresAt: 9999999999, IssuedAt: 1}
	h := AuthMiddleware(&stubIssuer{claims: claims}, fenceChecker{fenced: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/student/exam-attempts/att-1", nil)
	req.Header.Set("Authorization", "Bearer valid.token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("fenced deployment: code=%d want 401", rec.Code)
	}
}
