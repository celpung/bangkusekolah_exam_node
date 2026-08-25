package helper

import (
	"context"
	"errors"

	"github.com/celpung/bangkusekolah_exam_node/app/port/outbound"
	"gorm.io/gorm"
)

type Manager struct {
	db *gorm.DB
}

func NewTxManager(db *gorm.DB) outbound.TxManager {
	return &Manager{db: db}
}

func (m *Manager) Atomic(ctx context.Context, fn func(ctx context.Context) error) error {
	if fn == nil {
		return errors.New("transaction function is nil")
	}
	if m.db == nil {
		return errors.New("transaction manager db is nil")
	}
	if tx, ok := ctx.Value(txContextKey).(*gorm.DB); ok && tx != nil {
		return fn(ctx)
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(WithTx(ctx, tx))
	})
}
