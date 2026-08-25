package security

import "github.com/google/uuid"

// IDGenerator mints the same v4 UUIDs central's generator does, which is what
// lets central adopt a node-minted id verbatim with no mapping table.
type IDGenerator struct{}

func NewIDGenerator() *IDGenerator { return &IDGenerator{} }

func (g *IDGenerator) NewID() string { return uuid.NewString() }
