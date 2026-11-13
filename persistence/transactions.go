package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

// withBuilderTransaction is the bridge from repository builders to the shared
// SQL transaction owner. A scoped builder never opens a second connection.
func withBuilderTransaction(ctx context.Context, source dbx.Builder, block func(dbx.Builder) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch source := source.(type) {
	case *dbx.Tx:
		return block(source)
	case *dbx.DB:
		return db.WithTransaction(ctx, source.DB(), func(tx *sql.Tx) error { return block(source.Wrap(tx)) })
	default:
		return fmt.Errorf("unsupported repository builder %T", source)
	}
}

func scopeLabel(scope []string) string {
	if len(scope) > 0 {
		return scope[0]
	}
	return ""
}

func (s *SQLStore) WithTx(ctx context.Context, block func(model.DataStore) error, scope ...string) error {
	start := time.Now()
	err := withBuilderTransaction(ctx, s.db, func(builder dbx.Builder) error { return block(newSQLStore(builder)) })
	log.Trace(ctx, "Transaction finished", "scope", scopeLabel(scope), "elapsed", time.Since(start), err)
	return err
}

func (s *SQLStore) WithTxRetry(ctx context.Context, block func(context.Context, model.DataStore) error, scope ...string) error {
	if _, nested := s.db.(*dbx.Tx); nested {
		return block(ctx, s)
	}
	return db.RetryTransaction(ctx, scopeLabel(scope), func(attemptCtx context.Context) error {
		return s.WithTx(attemptCtx, func(tx model.DataStore) error { return block(attemptCtx, tx) }, scope...)
	})
}

func (r sqlRepository) withTx(ctx context.Context, block func(sqlRepository) error) error {
	return withBuilderTransaction(ctx, r.db, func(builder dbx.Builder) error { scoped := r; scoped.db = builder; return block(scoped) })
}
