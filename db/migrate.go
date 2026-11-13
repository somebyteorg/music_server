package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/navidrome/navidrome/log"

	"github.com/jackc/pgx/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

func migrate(ctx context.Context, conn *sql.DB) error {
	var lockID int64
	if err := conn.QueryRowContext(ctx,
		"SELECT hashtextextended(current_database() || ':navidrome:migrations:' || $1, 0)",
		conf.Server.DatabaseSchema).Scan(&lockID); err != nil {
		return err
	}

	// Serialize schema creation too: concurrent CREATE SCHEMA IF NOT EXISTS calls
	// can otherwise race on PostgreSQL's namespace uniqueness constraint.
	if err := WithTransaction(ctx, conn, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", lockID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{conf.Server.DatabaseSchema}.Sanitize())
		return err
	}); err != nil {
		return err
	}

	migrations, err := fs.Sub(embedMigrations, migrationsFolder)
	if err != nil {
		return err
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(lockID))
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, conn, migrations,
		goose.WithSessionLocker(locker), goose.WithDisableGlobalRegistry(true),
		goose.WithLogger(&migrationLogger{ctx: ctx}))
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

type migrationLogger struct{ ctx context.Context }

func (l *migrationLogger) Fatalf(format string, args ...any) {
	log.Fatal(l.ctx, fmt.Sprintf(format, args...))
}
func (l *migrationLogger) Printf(format string, args ...any) {
	log.Info(l.ctx, fmt.Sprintf(format, args...))
}
