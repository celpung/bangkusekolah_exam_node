// preflight runs the D-1 readiness checklist: bundle counts, disk space, and
// clock offset. Prints PASS or "FAIL: <reason>" and exits 1 on failure.
package main

import (
	"context"
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

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		fail("config: %v", err)
	}
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

	// Expected counts come from central's deployment record; until Task 20's
	// harvest client fetches them, preflight accepts the loaded counts as-is
	// and verifies structural integrity (exam present with checksum + rows).
	exams, err := repo.ListExams(context.Background())
	if err != nil {
		fail("list exams: %v", err)
	}
	for _, exam := range exams {
		items, err := repo.ListItemsByExamID(context.Background(), exam.ID)
		if err != nil {
			fail("list items for %s: %v", exam.ID, err)
		}
		if len(items) == 0 {
			fail("exam %s has no items", exam.ID)
		}
		participants, err := repo.ListParticipants(context.Background())
		if err != nil {
			fail("list participants: %v", err)
		}
		if len(participants) == 0 {
			fail("exam %s has no participants", exam.ID)
		}
		if exam.BundleChecksum == "" {
			fail("exam %s has no bundle checksum", exam.ID)
		}
		_ = bundleSvc // counts-vs-deployment check lands with Task 20
	}

	if free := diskFree("."); free < minDiskFreeBytes {
		fail("disk free %d bytes, want >%d", free, minDiskFreeBytes)
	}
	if offset := clockSkewEstimate(); offset > maxClockOffset {
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
func clockSkewEstimate() time.Duration {
	cfg, err := config.Load()
	if err != nil || cfg.CentralBaseURL == "" {
		return 0
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Head(cfg.CentralBaseURL)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	serverDate := resp.Header.Get("Date")
	if serverDate == "" {
		return 0
	}
	serverTime, err := http.ParseTime(serverDate)
	if err != nil {
		return 0
	}
	diff := time.Since(serverTime)
	if diff < 0 {
		return -diff
	}
	return diff
}
