package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
)

type ContentService struct {
	repo  outbound_repository.NodeRepository
	mu    sync.RWMutex
	cache *contentCache
}

type contentCache struct {
	content   *inbound.ExamContent
	etag      string
	gzipBytes []byte
	rawBytes  []byte
}

func NewContentService(repo outbound_repository.NodeRepository) *ContentService {
	return &ContentService{repo: repo}
}

// Rebuild must be called by BundleService after LoadBundle (Task 19). It is
// the only writer; GetExamContent serves from the immutable snapshot.
func (s *ContentService) Rebuild(ctx context.Context) error {
	exam, err := s.repo.FindExam(ctx)
	if err != nil {
		return err
	}
	items, err := s.repo.ListItemsByExam(ctx)
	if err != nil {
		return err
	}
	content := &inbound.ExamContent{Exam: exam}
	for i := range items {
		it := &items[i]
		content.Items = append(content.Items, inbound.ExamItemDTO{
			ID: it.ID, SectionID: it.SectionID, SectionTitle: it.SectionTitle,
			SectionSortOrder: it.SectionSortOrder, SortOrder: it.SortOrder,
			QuestionType: string(it.QuestionType), Prompt: it.PromptSnapshot,
			Options: it.OptionsSnapshotJSON, Points: it.Points,
			RequiresManualGrading: it.RequiresManualGrading,
		})
	}
	// Deterministic JSON — encoding/json emits struct fields in declaration
	// order and map keys sorted, so the ETag is stable across restarts.
	raw, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("marshal content: %w", err)
	}
	sum := sha256.Sum256(raw)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"` // quoted per RFC 7232
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(raw); err != nil {
		return fmt.Errorf("gzip content: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip writer: %w", err)
	}

	s.mu.Lock()
	s.cache = &contentCache{content: content, etag: etag, gzipBytes: buf.Bytes(), rawBytes: raw}
	s.mu.Unlock()
	return nil
}

func (s *ContentService) GetExamContent(_ context.Context) (*inbound.ExamContent, string, []byte, []byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return nil, "", nil, nil, node_error.ErrExamNotLoaded
	}
	return s.cache.content, s.cache.etag, s.cache.gzipBytes, s.cache.rawBytes, nil
}
