// examharvest runs one synchronous harvest drain: sweeper first (auto-submit
// expired attempts), then push all finished unpushed attempts to central.
// One-shot, follows the cmd/examrepair precedent. Runbook usage:
//
//	go run ./cmd/examharvest --force
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/joho/godotenv"

	nodecentral "github.com/celpung/bangkusekolah_exam_node/app/adapter/central"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/provider"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

func main() {
	godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		fatalf("config: %v", err)
	}
	force := flag.Bool("force", false, "drain now even if the last tick just ran")
	flag.Parse()
	if !*force {
		fmt.Println("nothing to do: pass --force to run a drain now")
		return
	}

	db, err := provider.Connect(cfg)
	if err != nil {
		fatalf("db: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	client := nodecentral.NewHarvestClient(cfg)
	harvestSvc := service.NewHarvestService(repo, client)

	ctx := context.Background()

	// Sweeper first, then harvest — auto-submit expired attempts so they are
	// included in this drain (regression guard for exam_service.go:1616).
	sweeperSvc := service.NewSweeperService(repo, txManager)
	swept, err := sweeperSvc.SweepExpiredAttempts(ctx)
	if err != nil {
		fatalf("sweep: %v", err)
	}
	fmt.Printf("swept %d expired attempts\n", swept)

	n, err := harvestSvc.DrainFinal(ctx)
	if err != nil {
		fatalf("harvest: %v", err)
	}
	fmt.Printf("harvested %d attempts\n", n)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}
