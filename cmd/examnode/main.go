package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/joho/godotenv"

	nodecentral "github.com/celpung/bangkusekolah_exam_node/app/adapter/central"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/handler"
	node_router "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/router"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/provider"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	node_security "github.com/celpung/bangkusekolah_exam_node/app/adapter/security"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	db, err := provider.Connect(cfg)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	// Fresh node databases work: apply pending migrations on startup.
	if err := provider.Run(sqlDB); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	idGen := &uuidGenerator{}
	issuer := node_security.NewJWTIssuer(cfg)
	contentSvc := service.NewContentService(repo)
	attemptSvc := service.NewAttemptService(repo, txManager, idGen)
	integritySvc := service.NewIntegrityService(repo, txManager, idGen)
	bundleSvc := service.NewBundleService(repo, txManager, contentSvc)
	harvestClient := nodecentral.NewHarvestClient(cfg)
	fenceClient := nodecentral.NewFenceClient(cfg.CentralBaseURL, cfg.CentralNodeToken)
	harvestSvc := service.NewHarvestService(repo, harvestClient)
	sweeperSvc := service.NewSweeperService(repo, txManager)
	harvestSvc.SetSweeper(sweeperSvc)
	authSvc := service.NewAuthService(repo, issuer)
	studentExamSvc := service.NewStudentExamService(repo)

	// Rehydrate the in-memory content cache from the persisted bundles
	// BEFORE accepting traffic — a restart must not break the sitting flow.
	// Any rebuild failure aborts startup (the executable's decision).
	if err := service.RehydrateAllCaches(context.Background(), repo, contentSvc); err != nil {
		log.Fatalf("startup cache rehydrate: %v", err)
	}

	r := chi.NewRouter()
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	// Content is pre-gzipped and served with its own Vary header; chi's
	// Compress middleware would re-wrap it, so it stays off.
	r.Use(chimiddleware.Throttle(cfg.MaxInflightRequests))

	internalH := handler.NewInternalHandler(bundleSvc)
	harvestH := handler.NewHarvestHandler(harvestSvc)
	readiness := node_router.NewReadinessRouter(contentSvc,
		func() ([]string, error) {
			exams, err := repo.ListExams(context.Background())
			if err != nil {
				return nil, err
			}
			ids := make([]string, len(exams))
			for i, e := range exams {
				ids[i] = e.ID
			}
			return ids, nil
		},
		sqlDB.Ping)
	r.Mount("/", node_router.NewRouter(issuer, cfg.CentralNodeToken, contentSvc, attemptSvc, integritySvc, internalH, harvestH, readiness, authSvc, studentExamSvc, repo))

	// Background workers: sweeper drains expired attempts each tick; harvest
	// pushes finished work to central every cfg.HarvestInterval (default 5m).
	go sweeperSvc.Start(context.Background(), cfg.SweepInterval)
	go harvestSvc.Start(context.Background(), cfg.HarvestInterval)
	if fenceRepo, ok := repo.(outbound_repository.DeploymentFenceRepository); ok {
		go startFenceReconciler(context.Background(), fenceRepo, fenceClient, cfg.HeartbeatInterval)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("examnode listening on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

func startFenceReconciler(ctx context.Context, repo outbound_repository.DeploymentFenceRepository, client *nodecentral.FenceClient, interval time.Duration) {
	if interval <= 0 {
		return
	}
	// E2E-only failure-injection hook: when set, the local fence write is
	// skipped for the matching deployment so the reconciler never sends the
	// acknowledgement. Never set in production; used by the cross-process
	// abort-fencing E2E (service app/e2e) to prove central delegation is
	// kept while local fencing has not succeeded.
	failLocalFenceDeployment := os.Getenv("E2E_FAIL_LOCAL_FENCE_DEPLOYMENT")
	reconcile := func() {
		fences, err := client.ListPendingFences(ctx)
		if err != nil {
			log.Printf("fence discovery failed: %v", err)
			return
		}
		for _, fence := range fences {
			if failLocalFenceDeployment != "" && fence.ID == failLocalFenceDeployment {
				log.Printf("E2E hook: skipping local fence write for deployment %s", fence.ID)
				continue
			}
			if err := repo.MarkDeploymentFenced(ctx, fence.ID, time.Now().UTC()); err != nil {
				log.Printf("local fence failed for deployment %s: %v", fence.ID, err)
				continue
			}
			if err := client.AcknowledgeFence(ctx, fence.ID); err != nil {
				log.Printf("fence acknowledgement failed for deployment %s: %v", fence.ID, err)
			}
		}
	}
	reconcile()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}

type uuidGenerator struct{}

func (*uuidGenerator) NewID() string { return uuid.NewString() }
