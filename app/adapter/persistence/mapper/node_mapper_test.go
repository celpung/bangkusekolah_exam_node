package mapper

import (
	"testing"

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
