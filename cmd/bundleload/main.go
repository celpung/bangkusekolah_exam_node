// bundleload discovers, validates, and loads one or more exam bundles into the
// node database. After a successful load it asks a running examnode process to
// refresh its in-memory cache; a stopped process rehydrates the same snapshot at startup.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/joho/godotenv"

	nodecentral "github.com/celpung/bangkusekolah_exam_node/app/adapter/central"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/provider"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/config"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	"github.com/celpung/bangkusekolah_exam_node/app/service"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		fatalf("config: %v", err)
	}
	var (
		bundlePath = flag.String("bundle", "", "path to bundle JSON (or - for stdin)")
		pull       = flag.Bool("pull", false, "discover and load every live deployment from central")
	)
	flag.Parse()
	if *pull && *bundlePath != "" {
		fatalf("use either --pull or --bundle, not both")
	}

	ctx := context.Background()
	var bundles []inbound.ExamNodeBundle
	if *pull {
		client := nodecentral.NewBundleClient(cfg)
		bundles, err = fetchBundles(ctx, client)
		if err != nil {
			fatalf("pull bundles: %v", err)
		}
	} else {
		var raw []byte
		if *bundlePath == "" || *bundlePath == "-" {
			raw, err = io.ReadAll(os.Stdin)
		} else {
			raw, err = os.ReadFile(*bundlePath)
		}
		if err != nil {
			fatalf("read bundle: %v", err)
		}

		var bundle inbound.ExamNodeBundle
		if err := json.Unmarshal(raw, &bundle); err != nil {
			fatalf("parse bundle: %v", err)
		}
		// Verify checksum before touching the DB.
		if want := service.ComputeBundleChecksum(bundle); bundle.Checksum != want {
			fatalf("bundle checksum mismatch: got %q, want %q", bundle.Checksum, want)
		}
		bundles = []inbound.ExamNodeBundle{bundle}
	}

	db, err := provider.Connect(cfg)
	if err != nil {
		fatalf("db: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	// Fresh node databases work: apply pending migrations first.
	if err := provider.Run(sqlDB); err != nil {
		fatalf("migrate: %v", err)
	}

	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	contentSvc := service.NewContentService(repo)
	bundleSvc := service.NewBundleService(repo, txManager, contentSvc)
	for _, bundle := range bundles {
		if err := bundleSvc.LoadBundle(ctx, bundle); err != nil {
			fatalf("load deployment %s: %v", bundle.DeploymentID, err)
		}
	}
	if reloaded, err := notifyRuntimeReload(ctx, cfg); err != nil {
		fatalf("reload running examnode cache: %v", err)
	} else if reloaded {
		fmt.Println("ok running examnode cache reloaded")
	} else {
		fmt.Println("ok running examnode cache reload skipped (examnode is not running)")
	}
	if *pull {
		fmt.Printf("ok loaded %d deployment(s)\n", len(bundles))
	} else {
		fmt.Printf("ok %s\n", bundles[0].Checksum)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}
