package service

import (
	"context"
	"log"

	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

// RehydrateAllCaches is the startup path shared by cmd/examnode: rebuild the
// content cache for every persisted exam before the node accepts traffic.
// Any failure aborts startup — a partially hydrated node must not serve.
// Exposed as a function so integration tests exercise the real executable
// sequence rather than reimplementing the loop.
func RehydrateAllCaches(ctx context.Context, repo outbound_repository.NodeRepository, contentSvc *ContentService) {
	exams, err := repo.ListExams(ctx)
	if err != nil {
		log.Fatalf("startup: list exams for cache rehydrate: %v", err)
	}
	for _, exam := range exams {
		if err := contentSvc.RebuildExam(ctx, exam.ID); err != nil {
			log.Fatalf("startup: rebuild content cache for exam %s: %v", exam.ID, err)
		}
		log.Printf("rehydrated content cache for exam %s", exam.ID)
	}
}
