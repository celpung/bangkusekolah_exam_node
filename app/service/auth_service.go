package service

import (
	"context"
	"crypto/subtle"
	"strings"
	"sync"
	"time"

	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

const maxRateLimitKeys = 4096

type AuthService struct {
	repo   outbound_repository.NodeRepository
	issuer outbound.JWTIssuer
	ttl    time.Duration
	limit  int
	window time.Duration
	mu     sync.Mutex
	hits   map[string][]time.Time
}

func NewAuthService(repo outbound_repository.NodeRepository, issuer outbound.JWTIssuer) *AuthService {
	return NewAuthServiceWithLimits(repo, issuer, 90*time.Minute, 10, time.Minute)
}

func NewAuthServiceWithLimits(repo outbound_repository.NodeRepository, issuer outbound.JWTIssuer, ttl time.Duration, limit int, window time.Duration) *AuthService {
	return &AuthService{repo: repo, issuer: issuer, ttl: ttl, limit: limit, window: window, hits: map[string][]time.Time{}}
}

func (s *AuthService) allow(key string) bool {
	if s.limit <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	cut := now.Add(-s.window)
	h := s.hits[key]
	// filter
	n := 0
	for _, t := range h {
		if t.After(cut) {
			h[n] = t
			n++
		}
	}
	h = h[:n]
	if len(s.hits) >= maxRateLimitKeys {
		if _, exists := s.hits[key]; !exists {
			var oldestKey string
			var oldest time.Time
			for candidate, timestamps := range s.hits {
				if len(timestamps) == 0 {
					oldestKey = candidate
					break
				}
				candidateOldest := timestamps[0]
				if oldestKey == "" || candidateOldest.Before(oldest) {
					oldestKey = candidate
					oldest = candidateOldest
				}
			}
			delete(s.hits, oldestKey)
		}
	}
	if len(h) >= s.limit {
		s.hits[key] = h
		return false
	}
	h = append(h, now)
	s.hits[key] = h
	return true
}

// Login normalizes the code (trim, upper-case), validates the <EXAM6>-<PART6>
// shape, resolves the participant first, then loads the participant's exam by ID.
// DeploymentID travels in the JWT per the frozen contract (W2-T2).
func (s *AuthService) Login(ctx context.Context, rawCode string) (*inbound.LoginResult, error) {
	codeKey := strings.ToUpper(strings.TrimSpace(rawCode))
	return s.login(ctx, rawCode, "code:"+codeKey)
}

func (s *AuthService) LoginWithKey(ctx context.Context, rawCode, clientKey string) (*inbound.LoginResult, error) {
	key := strings.TrimSpace(clientKey)
	if key == "" {
		key = "code:" + strings.ToUpper(strings.TrimSpace(rawCode))
	} else {
		key = "client:" + key
	}
	return s.login(ctx, rawCode, key)
}

func (s *AuthService) login(ctx context.Context, rawCode, rateKey string) (*inbound.LoginResult, error) {
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	parts := strings.Split(code, "-")
	if len(parts) != 2 || len(parts[0]) != 6 || len(parts[1]) != 6 {
		return nil, node_error.ErrInvalidAccessCode
	}
	if strings.ContainsAny(code, "ILOU") {
		return nil, node_error.ErrInvalidAccessCode
	}
	if !s.allow(rateKey) {
		return nil, node_error.ErrTooManyAttempts
	}
	participant, err := s.repo.FindParticipantByAccessCode(ctx, code)
	if err != nil {
		return nil, node_error.ErrInvalidAccessCode
	}
	if subtle.ConstantTimeCompare([]byte(participant.AccessCode), []byte(code)) != 1 {
		return nil, node_error.ErrInvalidAccessCode
	}
	exam, err := s.repo.FindExamByID(ctx, participant.ExamID)
	if err != nil || exam == nil {
		return nil, node_error.ErrInvalidAccessCode
	}
	if parts[0] != exam.AccessCodePrefix {
		return nil, node_error.ErrInvalidAccessCode
	}
	token, err := s.issuer.Issue(ctx, participant.ID, participant.StudentID, exam.ID, exam.DeploymentID)
	if err != nil {
		return nil, err
	}
	exp := time.Now().Add(s.ttl).Unix()
	if s.issuer != nil {
		if c, _ := s.issuer.Parse(ctx, token); c != nil && c.ExpiresAt != 0 {
			exp = c.ExpiresAt
		}
	}
	return &inbound.LoginResult{
		ParticipantID: participant.ID, StudentID: participant.StudentID,
		ExamID: exam.ID, DeploymentID: exam.DeploymentID, Token: token, ExpiresAt: exp,
	}, nil
}

var _ inbound.AuthUsecase = (*AuthService)(nil)
