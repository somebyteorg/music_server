package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/navidrome/navidrome/log"
)

// Executor is the SQL surface shared by a pool and one transaction connection.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// WithTransaction joins an existing transaction or owns the complete lifecycle
// of a new one. A panic or error rolls back; commit errors reach the caller.
func WithTransaction(ctx context.Context, source Executor, block func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx, ok := source.(*sql.Tx); ok {
		return block(tx)
	}
	pool, ok := source.(*sql.DB)
	if !ok {
		return fmt.Errorf("unsupported transaction executor %T", source)
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := block(tx); err != nil {
		return err
	}
	return tx.Commit()
}

type retryTransactionKey struct{}

// TransactionWillRetry marks statements whose enclosing transaction can be
// rerun. It lets query logging report recoverable failures at warning level.
func TransactionWillRetry(ctx context.Context) bool {
	retrying, _ := ctx.Value(retryTransactionKey{}).(bool)
	return retrying
}

// RetryTransaction reruns only PostgreSQL-aborted transactions. Callbacks must
// keep external side effects outside the retry boundary.
func RetryTransaction(ctx context.Context, scope string, block func(context.Context) error) error {
	const maxRetries = 3
	const initialDelay = 50 * time.Millisecond
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		attemptCtx := ctx
		if attempt < maxRetries {
			attemptCtx = context.WithValue(ctx, retryTransactionKey{}, true)
		}
		err := block(attemptCtx)
		if attempt == maxRetries || !IsRetryable(err) {
			return err
		}
		log.Warn(ctx, "Retrying aborted PostgreSQL transaction", "scope", scope, "attempt", attempt+1, err)
		timer := time.NewTimer(initialDelay << attempt)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
