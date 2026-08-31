//go:build load
// +build load

package load

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/provider"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
	_ "github.com/go-sql-driver/mysql"
)

// TestHTTPLoginBurst exercises the real node HTTP router and auth middleware,
// rather than calling AttemptService directly. It uses 1000 unique participants
// and access codes against a real node process and a fresh disposable database.
func TestHTTPLoginBurst(t *testing.T) {
	if testing.Short() {
		t.Skip("skip HTTP burst load test in -short")
	}

	const dbName = "bangkusekolah_w5_http_load_test"
	const students = 1000
	const loginLimit = 2000
	const maxInflight = 2000
	const jwtSecret = "w5-http-load-test-secret-01234567890123456789"
	baseDSN := "root@tcp(127.0.0.1:3308)/"
	dsn := baseDSN + dbName + "?charset=utf8mb4&parseTime=true&loc=Local"

	admin, err := sql.Open("mysql", baseDSN+"mysql?parseTime=true")
	if err != nil {
		t.Fatalf("open mysql admin: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec("DROP DATABASE IF EXISTS `" + dbName + "`"); err != nil {
		t.Fatalf("drop HTTP load database: %v", err)
	}
	if _, err := admin.Exec("CREATE DATABASE `" + dbName + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("prepare HTTP load database: %v", err)
	}
	defer admin.Exec("DROP DATABASE IF EXISTS `" + dbName + "`")

	t.Setenv("DB_DSN", dsn)
	t.Setenv("NODE_JWT_SECRET", jwtSecret)
	t.Setenv("CENTRAL_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("CENTRAL_NODE_TOKEN", "w5-http-load-central-token")
	t.Setenv("HTTP_PORT", "0")
	t.Setenv("LOGIN_RATE_LIMIT", fmt.Sprint(loginLimit))
	t.Setenv("MAX_INFLIGHT_REQUESTS", fmt.Sprint(maxInflight))
	t.Setenv("DB_MAX_OPEN_CONNS", "100")
	t.Setenv("DB_MAX_IDLE_CONNS", "50")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load node config: %v", err)
	}
	db, err := provider.Connect(cfg)
	if err != nil {
		t.Fatalf("connect node database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("node sql handle: %v", err)
	}
	if err := provider.Run(sqlDB); err != nil {
		sqlDB.Close()
		t.Fatalf("node migrations: %v", err)
	}
	bundle := syntheticBundle(students, 1)
	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	contentSvc := service.NewContentService(repo)
	bundleSvc := service.NewBundleService(repo, txManager, contentSvc)
	if err := bundleSvc.LoadBundle(context.Background(), bundle); err != nil {
		sqlDB.Close()
		t.Fatalf("load HTTP burst bundle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close seed DB: %v", err)
	}

	root := filepath.Clean(filepath.Join(mustGetwd(t), "../.."))
	binary := filepath.Join(t.TempDir(), "examnode-http-load")
	build := exec.Command("go", "build", "-o", binary, "./cmd/examnode")
	build.Dir = root
	build.Env = os.Environ()
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build examnode: %v\n%s", err, output)
	}

	port := freeTCPPort(t)
	baseURL := "http://127.0.0.1:" + port
	node := exec.Command(binary)
	node.Dir = root
	node.Env = replaceEnv(os.Environ(), map[string]string{
		"DB_DSN":                dsn,
		"NODE_JWT_SECRET":       jwtSecret,
		"CENTRAL_BASE_URL":      "http://127.0.0.1:1",
		"CENTRAL_NODE_TOKEN":    "w5-http-load-central-token",
		"HTTP_PORT":             port,
		"LOGIN_RATE_LIMIT":      fmt.Sprint(loginLimit),
		"MAX_INFLIGHT_REQUESTS": fmt.Sprint(maxInflight),
		"DB_MAX_OPEN_CONNS":     "100",
		"DB_MAX_IDLE_CONNS":     "50",
	})
	node.Stdout = io.Discard
	node.Stderr = io.Discard
	if err := node.Start(); err != nil {
		t.Fatalf("start examnode: %v", err)
	}
	t.Cleanup(func() {
		_ = node.Process.Kill()
		_, _ = node.Process.Wait()
	})
	waitHTTPReady(t, baseURL, 45*time.Second)

	client := &http.Client{Transport: &http.Transport{MaxIdleConns: students, MaxIdleConnsPerHost: students}, Timeout: 30 * time.Second}
	start := time.Now()
	barrier := make(chan struct{})
	var success atomic.Int64
	var wg sync.WaitGroup
	var failureMu sync.Mutex
	failures := make([]string, 0, 5)
	for _, participant := range bundle.Participants {
		wg.Add(1)
		go func(code string) {
			defer wg.Done()
			<-barrier
			body := strings.NewReader(fmt.Sprintf(`{"code":%q}`, code))
			req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/auth/exam-login", body)
			if err != nil {
				failureMu.Lock()
				if len(failures) < 5 {
					failures = append(failures, err.Error())
				}
				failureMu.Unlock()
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				failureMu.Lock()
				if len(failures) < 5 {
					failures = append(failures, err.Error())
				}
				failureMu.Unlock()
				return
			}
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err == nil && resp.StatusCode == http.StatusOK {
				var envelope struct {
					Data struct {
						Token string `json:"token"`
					} `json:"data"`
				}
				if json.Unmarshal(data, &envelope) == nil && envelope.Data.Token != "" {
					success.Add(1)
					return
				}
			}
			failureMu.Lock()
			if len(failures) < 5 {
				failures = append(failures, fmt.Sprintf("status=%d", resp.StatusCode))
			}
			failureMu.Unlock()
		}(participant.AccessCode)
	}
	close(barrier)
	wg.Wait()
	elapsed := time.Since(start)
	if got := success.Load(); got != students {
		t.Fatalf("HTTP login burst: %d/%d succeeded in %v; failures=%v", got, students, elapsed, failures)
	}
	if elapsed > 60*time.Second {
		t.Fatalf("HTTP login burst took %v, want <=60s", elapsed)
	}
	t.Logf("HTTP login burst: %d/%d in %v (%.0f rps)", students, success.Load(), elapsed, float64(success.Load())/elapsed.Seconds())
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}

func replaceEnv(base []string, overrides map[string]string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key := entry
		if index := strings.IndexByte(entry, '='); index >= 0 {
			key = entry[:index]
		}
		if _, replaced := overrides[key]; !replaced {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func freeTCPPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split port: %v", err)
	}
	return port
}

func waitHTTPReady(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL + "/readyz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("node never became ready within %s", timeout)
}
