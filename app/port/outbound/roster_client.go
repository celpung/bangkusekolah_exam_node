package outbound

import (
	"context"

	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type RosterClient interface {
	PullPending(ctx context.Context) ([]inbound.RosterEvent, error)
	Replay(ctx context.Context, deploymentID string, afterRevision int64) ([]inbound.RosterEvent, error)
	Acknowledge(ctx context.Context, outcome inbound.RosterOutcome) error
	AnnounceCapabilities(ctx context.Context) error
}
