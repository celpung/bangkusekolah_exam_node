package repository

import "github.com/celpung/bangkusekolah_exam_node/app/port/outbound"

// The roster persistence methods live on nodeRepository so roster writes share
// the same transaction-aware DB adapter as the sitting flow.
var _ outbound.RosterRepository = (*nodeRepository)(nil)

var _ outbound.RosterPreflightRepository = (*nodeRepository)(nil)
