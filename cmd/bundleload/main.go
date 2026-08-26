// bundleload loads one exam bundle JSON into the node database. The checksum
// is verified before anything touches the DB.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/joho/godotenv"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/provider"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	node_security "github.com/celpung/bangkusekolah_exam_node/app/adapter/security"
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
	)
	flag.Parse()

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

	db, err := provider.Connect(cfg)
	if err != nil {
		fatalf("db: %v", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	repo := repository.NewNodeRepository(db)
	txManager := helper.NewTxManager(db)
	contentSvc := service.NewContentService(repo)
	bundleSvc := service.NewBundleService(repo, txManager, contentSvc)

	if err := bundleSvc.LoadBundle(context.Background(), bundle); err != nil {
		fatalf("load bundle: %v", err)
	}
	fmt.Printf("ok %s\n", bundle.Checksum)
	_ = node_security.NewJWTIssuer // issuer lives in the server binary; CLI only loads data
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}
