package outbound

import "context"

type TxManager interface {
	Atomic(ctx context.Context, fn func(ctx context.Context) error) error
}
