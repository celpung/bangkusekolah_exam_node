package helper

import (
	"context"

	"gorm.io/gorm"
)

type contextKey string

const txContextKey contextKey = "tx"

func GetDB(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txContextKey).(*gorm.DB); ok && tx != nil {
		return tx.WithContext(ctx)
	}
	return db.WithContext(ctx)
}

func WithTx(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, txContextKey, tx)
}
