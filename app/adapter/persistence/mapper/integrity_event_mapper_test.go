package mapper

import (
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/adapter/persistence/model"
)

func TestToIntegrityEventEntityPreservesDescriptionAndMetadata(t *testing.T) {
	description := "screen capture signal"
	metadata := `{"platform":"android","violation_sequence":2}`

	event := ToIntegrityEventEntity(&model.IntegrityEvent{
		ID: "event-1", AttemptID: "attempt-1", StudentID: "student-1",
		EventType: "screen_capture_started", Description: &description,
		MetadataJSON: &metadata, CreatedAt: time.Unix(1, 0).UTC(),
	})

	if event == nil {
		t.Fatal("event = nil")
	}
	if event.Description == nil || *event.Description != description {
		t.Fatalf("description = %v, want %q", event.Description, description)
	}
	if event.MetadataJSON["platform"] != "android" || event.MetadataJSON["violation_sequence"] != float64(2) {
		t.Fatalf("metadata = %#v, want platform and violation sequence", event.MetadataJSON)
	}
}
