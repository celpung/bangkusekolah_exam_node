package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/handler"
	node_router "github.com/celpung/bangkusekolah_exam_node/app/adapter/delivery/router"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/provider"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	node_security "github.com/celpung/bangkusekolah_exam_node/app/adapter/security"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
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

	r.Get("/livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := sqlDB.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "unready", "error": err.Error()})
			return
		}
		// Per-exam cache readiness: a live bundle push whose post-commit
		// rebuild failed leaves the exam unready until a retry succeeds.
		unready := contentSvc.UnreadyExams()
		if len(unready) > 0 {
			causes := make(map[string]string, len(unready))
			for id, cause := range unready {
				causes[id] = cause.Error()
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "unready", "unready_exams": causes})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})

	internalH := handler.NewInternalHandler(bundleSvc)
	r.Mount("/", node_router.NewRouter(issuer, cfg.CentralNodeToken, contentSvc, attemptSvc, integritySvc, internalH))

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

type uuidGenerator struct{}

func (*uuidGenerator) NewID() string { return uuid.NewString() }
