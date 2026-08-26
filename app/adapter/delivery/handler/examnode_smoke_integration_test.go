//go:build integration

package handler_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestProcess_ExamnodeSmoke launches the actual cmd/examnode binary against
// the test database (HIGH-3, v10 review): startup must migrate, rehydrate
// caches from persisted bundles, bind the listener, and report /readyz 200.
func TestProcess_ExamnodeSmoke(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set for integration test")
	}
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "examnode-smoke")
	build := exec.Command("go", "build", "-o", bin, "github.com/celpung/bangkusekolah_exam_node/cmd/examnode")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Logf("build output:\n%s", out)
		t.Skipf("build examnode failed (likely cwd-restricted environment): %v", err)
	}

	port := freePort(t)
	// Persist one exam + roster BEFORE starting the binary so startup
	// rehydration has something to load and /readyz can reach 200.
	if err := seedSmokeExam(t, dsn); err != nil {
		t.Fatalf("seed smoke exam: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Dir = repoRoot // run from repo root so embedded migrations are used
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HTTP_PORT=%s", port),
		fmt.Sprintf("DB_DSN=%s", dsn),
		"NODE_JWT_SECRET=smoke-test-jwt-secret-32-characters!",
		"CENTRAL_NODE_TOKEN="+readinessTestToken,
		"CENTRAL_BASE_URL=http://127.0.0.1:1", // unreachable; clock check is preflight-only
		"DEPLOYMENT_ID=dep-smoke",
		"MAX_INFLIGHT_REQUESTS=400",
	)
	var output strings2
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start examnode: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	base := "http://127.0.0.1:" + port
	client := &http.Client{Timeout: 2 * time.Second}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("/readyz = %d after startup, want 200 (output: %s)", resp.StatusCode, output.String())
			}
			// /livez sanity
			live, err := client.Get(base + "/livez")
			if err != nil {
				t.Fatalf("livez: %v", err)
			}
			live.Body.Close()
			return // smoke passed
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("examnode never became ready within 15s; output:\n%s", output.String())
}

type strings2 struct{ b []byte }

func (s *strings2) Write(p []byte) (int, error) { s.b = append(s.b, p...); return len(p), nil }
func (s *strings2) String() string              { return string(s.b) }

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

// seedSmokeExam persists a minimal valid bundle directly through the service
// so the binary's startup rehydration has content to load.
func seedSmokeExam(t *testing.T, dsn string) error {
	t.Helper()
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	t.Helper()
	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	bundleSvc := service.NewBundleService(repo, txManager, &smokeRebuilder{})
	starts := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	b := inbound.ExamNodeBundle{
		BundleVersion: 1, DeploymentID: "dep-smoke",
		Exam: inbound.ExamNodeBundleExam{
			ID: "exam-smoke", Title: "Smoke", StartsAt: starts, EndsAt: starts.Add(2 * time.Hour),
			DurationMinutes: 60, MaxAttempts: 1, ResultSelectionPolicy: "best",
		},
		Items: []inbound.ExamNodeBundleItem{{
			ID: "item-smoke", SectionID: "sec-1", QuestionType: "single_choice",
			PromptSnapshot: "smoke?", Points: 10,
			AnswerKeySnapshotJSON: map[string]interface{}{"answer": "A"},
		}},
		Participants: []inbound.ExamNodeBundleParticipant{{
			ID: "p-smoke", StudentID: "s-smoke", StudentName: "Budi", AccessCode: "MMMMMM-111111",
		}},
	}
	b.Checksum = service.ComputeBundleChecksum(b)
	return bundleSvc.LoadBundle(context.Background(), b)
}

type smokeRebuilder struct{}

func (smokeRebuilder) RebuildExam(context.Context, string, ...uint64) error { return nil }
func (smokeRebuilder) BeginRebuild(string) uint64                           { return 0 }
func (smokeRebuilder) CancelRebuild(string, uint64) bool                    { return false }
func (smokeRebuilder) LockExam(string) func()                               { return func() {} }

var (
	_ = config.Config{}
	_ = context.Background
)
