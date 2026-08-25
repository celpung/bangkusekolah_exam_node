package security

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

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

// signWith signs arbitrary HS* tokens so Parse's algorithm pin can be tested
// against variants we never issue.
func signWith(method jwt.SigningMethod, secret string) string {
	claims := jwt.MapClaims{
		"pid": "part-1", "sid": "stu-1", "exam_id": "exam-1",
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(method, claims)
	signed, _ := tok.SignedString([]byte(secret))
	return signed
}

func TestJWTIssuerRejectsHS384AndHS512(t *testing.T) {
	issuer := NewJWTIssuer(testConfig())
	secret := testConfig().JWTSecret
	for _, method := range []*jwt.SigningMethodHMAC{jwt.SigningMethodHS384, jwt.SigningMethodHS512} {
		if _, err := issuer.Parse(context.Background(), signWith(method, secret)); err == nil {
			t.Fatalf("%s token must be rejected even with a valid signature", method.Alg())
		}
	}
}

func TestJWTIssuerRejectsMissingIdentityClaims(t *testing.T) {
	issuer := NewJWTIssuer(testConfig())
	now := time.Now().Unix()
	cases := map[string]jwt.MapClaims{
		"missing sid":     {"pid": "part-1", "exam_id": "exam-1", "iat": now, "exp": now + 3600},
		"missing exam_id": {"pid": "part-1", "sid": "stu-1", "iat": now, "exp": now + 3600},
		"missing exp":     {"pid": "part-1", "sid": "stu-1", "exam_id": "exam-1", "iat": now},
		"missing iat":     {"pid": "part-1", "sid": "stu-1", "exam_id": "exam-1", "exp": now + 3600},
		"zero exp":        {"pid": "part-1", "sid": "stu-1", "exam_id": "exam-1", "iat": now, "exp": 0},
	}
	for name, claims := range cases {
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		signed, err := tok.SignedString([]byte(issuer.secret))
		if err != nil {
			t.Fatalf("%s: sign: %v", name, err)
		}
		if _, err := issuer.Parse(context.Background(), signed); err == nil {
			t.Fatalf("%s: token missing a mandatory claim must be rejected", name)
		}
	}
}
