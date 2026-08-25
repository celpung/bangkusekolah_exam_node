package security

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
)

type JWTIssuer struct {
	secret []byte
	ttl    time.Duration
}

func NewJWTIssuer(cfg *config.Config) *JWTIssuer {
	return &JWTIssuer{secret: []byte(cfg.JWTSecret), ttl: cfg.JWTTTL}
}

func (j *JWTIssuer) Issue(_ context.Context, participantID, studentID, examID string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"pid": participantID, "sid": studentID, "exam_id": examID,
		"iat": now.Unix(), "exp": now.Add(j.ttl).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

func (j *JWTIssuer) Parse(_ context.Context, raw string) (*outbound.JWTClaims, error) {
	token, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return j.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, err
	}
	m, _ := token.Claims.(jwt.MapClaims)
	if m == nil {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return &outbound.JWTClaims{
		ParticipantID: stringFromClaim(m, "pid"),
		StudentID:     stringFromClaim(m, "sid"),
		ExamID:        stringFromClaim(m, "exam_id"),
		ExpiresAt:     int64FromClaim(m, "exp"),
		IssuedAt:      int64FromClaim(m, "iat"),
	}, nil
}

func stringFromClaim(m jwt.MapClaims, k string) string { s, _ := m[k].(string); return s }

func int64FromClaim(m jwt.MapClaims, k string) int64 {
	switch v := m[k].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	default:
		return 0
	}
}
