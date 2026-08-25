package service

import (
	"context"
	"errors"
	"testing"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type fakeAuthRepo struct {
	outbound_repository.NodeRepository
	exam         *entity.Exam
	participants map[string]*entity.Participant // key: access_code
}

func (f *fakeAuthRepo) FindExam(_ context.Context) (*entity.Exam, error) { return f.exam, nil }
func (f *fakeAuthRepo) FindParticipantByAccessCode(_ context.Context, code string) (*entity.Participant, error) {
	if p, ok := f.participants[code]; ok {
		return p, nil
	}
	return nil, node_error.ErrInvalidAccessCode
}

type fakeIssuer struct {
	token  string
	claims map[string]*fakeClaim
}

type fakeClaim struct{ pid, sid, eid string }

func (f *fakeIssuer) Issue(_ context.Context, pid, sid, eid string) (string, error) {
	tok := "jwt-" + pid
	if f.token != "" {
		tok = f.token
	}
	if f.claims == nil {
		f.claims = map[string]*fakeClaim{}
	}
	f.claims[tok] = &fakeClaim{pid, sid, eid}
	return tok, nil
}
func (f *fakeIssuer) Parse(_ context.Context, _ string) (*outbound.JWTClaims, error) {
	return nil, nil
}

func authFixture() (*AuthService, *fakeAuthRepo) {
	exam := &entity.Exam{ID: "exam-1", AccessCodePrefix: "K7M2QX", Title: "UTS"}
	repo := &fakeAuthRepo{
		exam: exam,
		participants: map[string]*entity.Participant{
			"K7M2QX-3B9FTD": {ID: "part-1", StudentID: "stu-1", StudentName: "Budi", AccessCode: "K7M2QX-3B9FTD"},
		},
	}
	issuer := &fakeIssuer{}
	svc := &AuthService{repo: repo, issuer: issuer}
	return svc, repo
}

func TestLoginSucceedsWithCorrectCode(t *testing.T) {
	svc, _ := authFixture()
	res, err := svc.Login(context.Background(), "K7M2QX-3B9FTD")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if res.ParticipantID != "part-1" || res.StudentID != "stu-1" || res.ExamID != "exam-1" || res.Token == "" {
		t.Fatalf("login result = %+v", res)
	}
}

func TestLoginNormalizesCodeAndRejectsWrongSuffix(t *testing.T) {
	svc, _ := authFixture()
	// lower-case + trimmed must still succeed (codes are case-insensitive on input,
	// stored upper-case; comparison is constant-time on normalized form)
	if _, err := svc.Login(context.Background(), " k7m2qx-3b9ftd "); err != nil {
		t.Fatalf("normalized login: %v", err)
	}
	if _, err := svc.Login(context.Background(), "K7M2QX-WRONG1"); !errors.Is(err, node_error.ErrInvalidAccessCode) {
		t.Fatalf("wrong suffix: got %v, want ErrInvalidAccessCode", err)
	}
}

func TestLoginRejectsWrongExamPrefix(t *testing.T) {
	svc, _ := authFixture()
	// Wrong exam prefix — brute force is bounded to one exam's code space.
	_, err := svc.Login(context.Background(), "WRONG1-3B9FTD")
	if !errors.Is(err, node_error.ErrInvalidAccessCode) {
		t.Fatalf("wrong exam prefix: got %v, want ErrInvalidAccessCode", err)
	}
}

func TestLoginRejectsMalformedCode(t *testing.T) {
	svc, _ := authFixture()
	for _, code := range []string{"", "K7M2QX", "K7M2QX-", "-3B9FTD", "K7M2QX-3B9FTD-EXTRA", "K7M2QX/3B9FTD"} {
		if _, err := svc.Login(context.Background(), code); !errors.Is(err, node_error.ErrInvalidAccessCode) {
			t.Fatalf("code %q: got %v, want ErrInvalidAccessCode", code, err)
		}
	}
}

// TestLoginRejectsCrockfordExcludes pins the Crockford alphabet: I, L, O, U
// never appear in a valid code, so any code containing them is malformed.
func TestLoginRejectsCrockfordExcludes(t *testing.T) {
	svc, _ := authFixture()
	for _, code := range []string{"K7M2IX-3B9FTD", "K7M2QX-3BL9TD", "K7M2QX-3BOUTD"} {
		if _, err := svc.Login(context.Background(), code); !errors.Is(err, node_error.ErrInvalidAccessCode) {
			t.Fatalf("code %q contains Crockford-excluded char, want ErrInvalidAccessCode, got %v", code, err)
		}
	}
}

func TestLoginIsConstantTimeForUnknownCode(t *testing.T) {
	// The service must not branch on "prefix exists" vs "suffix wrong" in a
	// way that leaks timing. This test asserts the error is identical in both
	// cases so a future refactor does not reintroduce an oracle.
	svc, _ := authFixture()
	_, err1 := svc.Login(context.Background(), "K7M2QX-WRONG1")
	_, err2 := svc.Login(context.Background(), "XXXXXX-WRONG1")
	if !errors.Is(err1, node_error.ErrInvalidAccessCode) || !errors.Is(err2, node_error.ErrInvalidAccessCode) {
		t.Fatalf("both must be ErrInvalidAccessCode: %v / %v", err1, err2)
	}
	if err1.Error() != err2.Error() {
		t.Fatalf("error messages must be identical: %q vs %q", err1.Error(), err2.Error())
	}
}

func TestLoginRejectsWhenExamNotLoaded(t *testing.T) {
	svc, repo := authFixture()
	repo.exam = nil
	if _, err := svc.Login(context.Background(), "K7M2QX-3B9FTD"); !errors.Is(err, node_error.ErrExamNotLoaded) {
		t.Fatalf("want ErrExamNotLoaded, got %v", err)
	}
}
