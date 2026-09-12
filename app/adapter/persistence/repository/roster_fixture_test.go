package repository

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

func TestRosterFixtureV1DecodesTheWireContract(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "testdata", "roster", "v1.json"))
	if err != nil {
		t.Fatalf("read roster fixture: %v", err)
	}

	var got inbound.RosterEvent
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode roster fixture: %v", err)
	}
	if got.EventID != "event-1" || got.DeploymentID != "deployment-1" || got.ParticipantID != "participant-1" {
		t.Fatalf("decoded roster identity = %+v", got)
	}
	if got.Revision != 1 || !got.Deadline.Equal(time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("decoded roster timing = %+v", got)
	}
}
