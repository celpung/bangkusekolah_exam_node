package outbound

import (
	"context"

	"github.com/celpung/bangkusekolah_exam_node/app/port/inbound"
)

type AttemptResetClient interface {
	PullAttemptResetCommands(ctx context.Context) ([]inbound.AttemptResetCommand, error)
	ReportAttemptResetOutcome(ctx context.Context, outcome inbound.AttemptResetOutcome) error
}

// AttemptResetCapabilityClient is optional so the worker remains compatible
// with small test clients while production nodes perform the capability
// handshake before polling commands.
type AttemptResetCapabilityClient interface {
	AnnounceAttemptResetCapabilities(ctx context.Context) error
}
