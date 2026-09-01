package mapper

import (
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/model"
	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

func TestAttemptMapperPreservesDeviceBinding(t *testing.T) {
	deviceID := "install-1"
	m := ToAttemptModel(&entity.Attempt{ID: "attempt-1", DeviceID: deviceID})
	if m.DeviceID == nil || *m.DeviceID != deviceID {
		t.Fatalf("model lost device binding: %v", m.DeviceID)
	}

	e := ToAttemptEntity(&model.Attempt{ID: "attempt-1", DeviceID: &deviceID})
	if e.DeviceID != deviceID {
		t.Fatalf("entity lost device binding: %q", e.DeviceID)
	}
}

func TestAttemptMapperKeepsLegacyDeviceBindingEmpty(t *testing.T) {
	m := ToAttemptModel(&entity.Attempt{ID: "attempt-legacy"})
	if m.DeviceID != nil {
		t.Fatalf("legacy empty device binding should be nullable, got %v", m.DeviceID)
	}

	e := ToAttemptEntity(&model.Attempt{ID: "attempt-legacy"})
	if e.DeviceID != "" {
		t.Fatalf("legacy nil device binding should map to empty, got %q", e.DeviceID)
	}
}

func TestExamMapperNormalizesScheduleToUTC(t *testing.T) {
	loc := time.FixedZone("Asia/Bangkok", 7*60*60)
	starts := time.Date(2026, 9, 2, 15, 0, 0, 0, loc)
	ends := starts.Add(time.Hour)

	entityExam := ToExamEntity(&model.Exam{ID: "exam-1", StartsAt: starts, EndsAt: ends})
	if !entityExam.StartsAt.Equal(starts) || entityExam.StartsAt.Location() != time.UTC {
		t.Fatalf("entity starts_at = %v (%v), want UTC instant %v", entityExam.StartsAt, entityExam.StartsAt.Location(), starts)
	}
	if !entityExam.EndsAt.Equal(ends) || entityExam.EndsAt.Location() != time.UTC {
		t.Fatalf("entity ends_at = %v (%v), want UTC instant %v", entityExam.EndsAt, entityExam.EndsAt.Location(), ends)
	}

	modelExam := ToExamModel(&entity.Exam{ID: "exam-1", StartsAt: starts, EndsAt: ends})
	if !modelExam.StartsAt.Equal(starts) || modelExam.StartsAt.Location() != time.UTC {
		t.Fatalf("model starts_at = %v (%v), want UTC instant %v", modelExam.StartsAt, modelExam.StartsAt.Location(), starts)
	}
	if !modelExam.EndsAt.Equal(ends) || modelExam.EndsAt.Location() != time.UTC {
		t.Fatalf("model ends_at = %v (%v), want UTC instant %v", modelExam.EndsAt, modelExam.EndsAt.Location(), ends)
	}
}
