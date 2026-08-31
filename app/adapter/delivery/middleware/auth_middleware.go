package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type ctxKey string

const (
	CtxParticipantID ctxKey = "participant_id"
	CtxStudentID     ctxKey = "student_id"
	CtxExamID        ctxKey = "exam_id"
	CtxDeploymentID  ctxKey = "deployment_id"
)

// AuthMiddleware validates the student's JWT and injects identity into context.
// It does no DB lookup — identity is carried in the token.
func AuthMiddleware(issuer outbound.JWTIssuer, fenceRepos ...outbound_repository.DeploymentFenceRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := strings.TrimSpace(r.Header.Get("Authorization"))
			if !strings.HasPrefix(raw, "Bearer ") {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			claims, err := issuer.Parse(r.Context(), strings.TrimSpace(strings.TrimPrefix(raw, "Bearer ")))
			if err != nil || claims == nil || claims.ParticipantID == "" || claims.ExamID == "" || claims.DeploymentID == "" {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			if len(fenceRepos) > 0 {
				fenced, fenceErr := fenceRepos[0].IsDeploymentFenced(r.Context(), claims.ExamID, claims.DeploymentID)
				if fenceErr != nil {
					helper.Error(w, http.StatusServiceUnavailable, "deployment fence state unavailable")
					return
				}
				if fenced {
					helper.Error(w, http.StatusUnauthorized, "deployment is fenced")
					return
				}
			}
			ctx := context.WithValue(r.Context(), CtxParticipantID, claims.ParticipantID)
			ctx = context.WithValue(ctx, CtxStudentID, claims.StudentID)
			ctx = context.WithValue(ctx, CtxExamID, claims.ExamID)
			ctx = context.WithValue(ctx, CtxDeploymentID, claims.DeploymentID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func ParticipantIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(CtxParticipantID).(string)
	return v, ok && v != ""
}

func StudentIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(CtxStudentID).(string)
	return v, ok && v != ""
}

func ExamIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(CtxExamID).(string)
	return v, ok && v != ""
}

func DeploymentIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(CtxDeploymentID).(string)
	return v, ok && v != ""
}
