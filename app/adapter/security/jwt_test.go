package security

import (
	"context"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/config"
)

func testConfig() *config.Config {
	return &config.Config{
		JWTSecret: "test-secret-that-is-long-enough-32ch!",
		JWTTTL:    90 * time.Minute,
	}
}

func TestJWTIssuerIssueAndParseRoundTrip(t *testing.T) {
	issuer := NewJWTIssuer(testConfig())
	ctx := context.Background()
	token, err := issuer.Issue(ctx, "part-1", "stu-1", "exam-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	claims, err := issuer.Parse(ctx, token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.ParticipantID != "part-1" || claims.StudentID != "stu-1" || claims.ExamID != "exam-1" {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("exp must be in the future: %d", claims.ExpiresAt)
	}
}

func TestJWTIssuerRejectsTamperedToken(t *testing.T) {
	issuer := NewJWTIssuer(testConfig())
	ctx := context.Background()
	token, _ := issuer.Issue(ctx, "part-1", "stu-1", "exam-1")
	tampered := token[:len(token)-2] + "xx"
	if _, err := issuer.Parse(ctx, tampered); err == nil {
		t.Fatal("tampered token must fail parse")
	}
}

func TestJWTIssuerRejectsForeignSecret(t *testing.T) {
	ctx := context.Background()
	a := NewJWTIssuer(&config.Config{JWTSecret: "secret-A-32-characters-long-xxxx", JWTTTL: time.Hour})
	b := NewJWTIssuer(&config.Config{JWTSecret: "secret-B-32-characters-long-yyyy", JWTTTL: time.Hour})
	token, _ := a.Issue(ctx, "part-1", "stu-1", "exam-1")
	if _, err := b.Parse(ctx, token); err == nil {
		t.Fatal("token signed with a different secret must not verify")
	}
}

func TestJWTIssuerExpiredTokenFails(t *testing.T) {
	issuer := NewJWTIssuer(&config.Config{JWTSecret: testConfig().JWTSecret, JWTTTL: -time.Minute})
	token, err := issuer.Issue(context.Background(), "part-1", "stu-1", "exam-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := issuer.Parse(context.Background(), token); err == nil {
		t.Fatal("expired token must fail parse")
	}
}
