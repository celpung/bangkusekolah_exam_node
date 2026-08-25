package repository

import (
	"context"
	"encoding/json"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/mapper"
	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/model"
	helper "github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/repository/helper"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
	node_error "github.com/celpung/bangkusekolah_exam_node/app/domain/error"
	outbound_repository "github.com/celpung/bangkusekolah_exam_node/app/port/outbound/repository"
	"gorm.io/gorm"
)

type nodeRepository struct {
	db *gorm.DB
}

func NewNodeRepository(db *gorm.DB) outbound_repository.NodeRepository {
	return &nodeRepository{db: db}
}

func (r *nodeRepository) FindExam(ctx context.Context) (*entity.Exam, error) {
	db := helper.GetDB(ctx, r.db)
	var m model.Exam
	if err := db.First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, node_error.ErrExamNotLoaded
		}
		return nil, err
	}
	return mapper.ToExamEntity(&m), nil
}

func (r *nodeRepository) FindParticipantByID(ctx context.Context, id string) (*entity.Participant, error) {
	db := helper.GetDB(ctx, r.db)
	var m model.Participant
	if err := db.Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, node_error.ErrParticipantNotFound
		}
		return nil, err
	}
	return mapper.ToParticipantEntity(&m), nil
}

func (r *nodeRepository) FindActiveAttemptByParticipant(ctx context.Context, pid string) (*entity.Attempt, error) {
	db := helper.GetDB(ctx, r.db)
	var m model.Attempt
	if err := db.Where("participant_id = ? AND status = ?", pid, string(entity.AttemptInProgress)).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, node_error.ErrAttemptNotFound
		}
		return nil, err
	}
	return mapper.ToAttemptEntity(&m), nil
}

func (r *nodeRepository) FindAttemptByID(ctx context.Context, id string) (*entity.Attempt, error) {
	db := helper.GetDB(ctx, r.db)
	var m model.Attempt
	if err := db.Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, node_error.ErrAttemptNotFound
		}
		return nil, err
	}
	return mapper.ToAttemptEntity(&m), nil
}

func (r *nodeRepository) ListAnswersByAttempt(ctx context.Context, attemptID string) ([]entity.Answer, error) {
	db := helper.GetDB(ctx, r.db)
	var models []model.Answer
	if err := db.Where("attempt_id = ?", attemptID).Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]entity.Answer, 0, len(models))
	for i := range models {
		out = append(out, *mapper.ToAnswerEntity(&models[i]))
	}
	return out, nil
}

func (r *nodeRepository) CreateAttempt(ctx context.Context, a *entity.Attempt) error {
	db := helper.GetDB(ctx, r.db)
	m := mapper.ToAttemptModel(a)
	if err := db.Create(m).Error; err != nil {
		return err
	}
	return nil
}

func (r *nodeRepository) UpdateParticipant(ctx context.Context, p *entity.Participant) error {
	db := helper.GetDB(ctx, r.db)
	m := &model.Participant{ID: p.ID, StudentID: p.StudentID, StudentName: p.StudentName, AccessCode: p.AccessCode, AttemptCount: p.AttemptCount, LatestAttemptID: p.LatestAttemptID}
	if err := db.Model(&model.Participant{}).Where("id = ?", p.ID).Updates(map[string]interface{}{"attempt_count": p.AttemptCount, "latest_attempt_id": p.LatestAttemptID}).Error; err != nil {
		return err
	}
	_ = m
	return nil
}

func (r *nodeRepository) CountAttemptsByParticipant(ctx context.Context, pid string) (int, error) {
	db := helper.GetDB(ctx, r.db)
	var n int64
	if err := db.Model(&model.Attempt{}).Where("participant_id = ?", pid).Count(&n).Error; err != nil {
		return 0, err
	}
	return int(n), nil
}

func (r *nodeRepository) FindItemByID(ctx context.Context, id string) (*entity.Item, error) {
	db := helper.GetDB(ctx, r.db)
	var m model.Item
	if err := db.Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, node_error.ErrItemNotFound
		}
		return nil, err
	}
	return mapper.ToItemEntity(&m), nil
}

// Ensure json import is used for future extensions
var _ = json.Marshal
