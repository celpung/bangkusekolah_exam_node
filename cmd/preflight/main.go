// preflight runs the D-1 readiness checklist: bundle integrity per exam,
// disk space, and clock offset. Prints PASS or "FAIL: <reason>" and exits 1
// on failure. A node with no loaded exams fails closed.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/provider"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

const (
	minDiskFreeBytes = 1 << 30 // 1GB
	maxClockOffset   = 2 * time.Second
)

// deploymentExpectation carries central's expected per-exam counts. Task 20's
// harvest client will fetch this from the deployment record; until then it is
// provided explicitly via --deployment (validated JSON) or counts are checked
// structurally only.
type deploymentExpectation struct {
	Exams map[string]struct {
		ItemCount        int `json:"item_count"`
		ParticipantCount int `json:"participant_count"`
	} `json:"exams"`
}

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		fail("config: %v", err)
	}
	var deployPath string
	flag.StringVar(&deployPath, "deployment", "", "path to central deployment expectations JSON {\"exams\":{\"<id>\":{\"item_count\":N,\"participant_count\":M}}}")
	flag.Parse()

	db, err := provider.Connect(cfg)
	if err != nil {
		fail("db: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	contentSvc := service.NewContentService(repo)
	bundleSvc := service.NewBundleService(repo, txManager, contentSvc)
	ctx := context.Background()

	var expects deploymentExpectation
	if deployPath != "" {
		raw, err := os.ReadFile(deployPath)
		if err != nil {
			fail("read deployment expectations: %v", err)
		}
		if err := json.Unmarshal(raw, &expects); err != nil {
			fail("parse deployment expectations: %v", err)
		}
	}

	exams, err := repo.ListExams(ctx)
	if err != nil {
		fail("list exams: %v", err)
	}
	// Fail closed: a node with no loaded bundle is not ready to sit students.
	if len(exams) == 0 {
		fail("no exams loaded on this node — run bundleload first")
	}

	for _, exam := range exams {
		items, err := repo.ListItemsByExamID(ctx, exam.ID)
		if err != nil {
			fail("list items for %s: %v", exam.ID, err)
		}
		participants, err := repo.ListParticipantsByExam(ctx, exam.ID)
		if err != nil {
			fail("list participants for %s: %v", exam.ID, err)
		}
		itemCount, partCount := len(items), len(participants)

		// Full BundleService.Preflight: content-hash verification against the
		// load-time rows plus structural checks.
		if expect, ok := expects.Exams[exam.ID]; ok {
			if err := bundleSvc.Preflight(ctx, exam.ID, expect.ItemCount, expect.ParticipantCount); err != nil {
				fail("preflight exam %s vs deployment: %v", exam.ID, err)
			}
			continue
		}

		// Without deployment expectations, verify structure + content hash
		// against what LoadBundle recorded (counts are self-consistent).
		if itemCount == 0 {
			fail("exam %s has no items", exam.ID)
		}
		if partCount == 0 {
			fail("exam %s has no participants", exam.ID)
		}
		if exam.BundleChecksum == "" {
			fail("exam %s has no bundle checksum", exam.ID)
		}
		if recomputed := service.ContentHash(items, participants, &exam); recomputed != exam.ContentHash {
			fail("exam %s stored content hash %q does not match rows (%q)", exam.ID, exam.ContentHash, recomputed)
		}

		// Cache readiness: a DB-committed but never-rebuilt bundle must not
		// pass readiness — students would get ErrExamNotLoaded at login time.
		if _, _, _, _, err := contentSvc.GetExamContent(ctx, exam.ID); err != nil {
			fail("exam %s content cache not ready (rebuild failed or not run): %v", exam.ID, err)
		}
	}

	if free := diskFree("."); free < minDiskFreeBytes {
		fail("disk free %d bytes, want >%d", free, minDiskFreeBytes)
	}
	if offset := clockSkewEstimate(cfg); offset > maxClockOffset {
		fail("clock offset ~%v, want <%v", offset.Round(time.Millisecond), maxClockOffset)
	}
	fmt.Println("PASS")
}

func fail(format string, args ...interface{}) {
	fmt.Printf("FAIL: "+format+"\n", args...)
	os.Exit(1)
}

func diskFree(path string) uint64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}

// clockSkewEstimate compares local time against the Date header of a HEAD
// request to central — no NTP port needed on locked-down VPS networks.
func clockSkewEstimate(cfg *config.Config) time.Duration {
	if cfg.CentralBaseURL == "" {
		return 0
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Head(cfg.CentralBaseURL + "/healthz")
	if err != nil {
		return 0 // unreachable central is not a clock problem; harvest will report it
	}
	defer resp.Body.Close()
	dateHeader := resp.Header.Get("Date")
	if dateHeader == "" {
		return 0
	}
	central, err := http.ParseTime(dateHeader)
	if err != nil {
		return 0
	}
	offset := time.Since(central)
	if offset < 0 {
		offset = -offset
	}
	return offset
}
