package service

import (
	"context"
	"fmt"

	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

// RehydrateAllCaches is the startup path shared by cmd/examnode: rebuild the
// content cache for every persisted exam before the node accepts traffic.
// It returns an error instead of terminating the process — the executable
// decides the recovery strategy (log.Fatalf at startup).
func RehydrateAllCaches(ctx context.Context, repo outbound_repository.NodeRepository, contentSvc *ContentService) error {
	exams, err := repo.ListExams(ctx)
	if err != nil {
		return fmt.Errorf("list exams for cache rehydrate: %w", err)
	}
	for _, exam := range exams {
		if err := contentSvc.RebuildExam(ctx, exam.ID); err != nil {
			return fmt.Errorf("rebuild content cache for exam %s: %w", exam.ID, err)
		}
	}
	return nil
}
