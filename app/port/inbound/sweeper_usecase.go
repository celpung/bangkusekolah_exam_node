package inbound

import (
	"context"
	"time"

	"github.com/celpung/bangkusekolah_exam_node/app/domain/entity"
)

// SweeperUsecase finalizes abandoned attempts so no in_progress row survives
// past its due_at — the node cannot tolerate central's lazy expiry.
type SweeperUsecase interface {
	SweepExpiredAttempts(ctx context.Context) (int, error)
	Start(ctx context.Context, interval time.Duration)
}

var _ = entity.AttemptInProgress
