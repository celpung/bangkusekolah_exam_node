package service

import (
	"context"
	"crypto/subtle"
	"strings"

	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type AuthService struct {
	repo   outbound_repository.NodeRepository
	issuer outbound.JWTIssuer
}

func NewAuthService(repo outbound_repository.NodeRepository, issuer outbound.JWTIssuer) *AuthService {
	return &AuthService{repo: repo, issuer: issuer}
}

// Login normalizes the code (trim, upper-case), validates the <EXAM6>-<PART6>
// shape, then looks up the participant by the exact stored code using
// constant-time comparison.
//
// Timing contract: the exam prefix is intentionally treated as public,
// non-secret information — it scopes brute force to one exam's code space
// rather than hiding it. The service does NOT claim constant-time behavior
// across the prefix-mismatch branch: a wrong prefix returns early without a
// DB lookup, and that difference is accepted by design. What IS guaranteed is
// that wrong-prefix and wrong-code rejections are indistinguishable by error
// value, and the final participant comparison is constant-time.
func (s *AuthService) Login(ctx context.Context, rawCode string) (*inbound.LoginResult, error) {
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	parts := strings.Split(code, "-")
	if len(parts) != 2 || len(parts[0]) != 6 || len(parts[1]) != 6 {
		return nil, node_error.ErrInvalidAccessCode
	}
	// Crockford alphabet check — I, L, O, U must not appear.
	if strings.ContainsAny(code, "ILOU") {
		return nil, node_error.ErrInvalidAccessCode
	}
	exam, err := s.repo.FindExam(ctx)
	if err != nil {
		return nil, err
	}
	if exam == nil {
		return nil, node_error.ErrExamNotLoaded
	}
	// Scope check: reject codes whose exam prefix does not match the loaded
	// bundle. See the timing-contract comment above for why this is an early
	// return rather than an equalized path.
	if parts[0] != exam.AccessCodePrefix {
		return nil, node_error.ErrInvalidAccessCode
	}
	participant, err := s.repo.FindParticipantByAccessCode(ctx, code)
	if err != nil {
		return nil, node_error.ErrInvalidAccessCode
	}
	// Constant-time code equality — the DB lookup already matched, but this
	// guards against a future cache layer that might do prefix-only matching.
	if subtle.ConstantTimeCompare([]byte(participant.AccessCode), []byte(code)) != 1 {
		return nil, node_error.ErrInvalidAccessCode
	}
	token, err := s.issuer.Issue(ctx, participant.ID, participant.StudentID, exam.ID)
	if err != nil {
		return nil, err
	}
	return &inbound.LoginResult{
		ParticipantID: participant.ID, StudentID: participant.StudentID,
		ExamID: exam.ID, Token: token,
	}, nil
}
