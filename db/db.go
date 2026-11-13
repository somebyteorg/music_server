package db

import (
	"context"
	"database/sql"
	"embed"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/utils/singleton"
)

const (
	Dialect          = "postgres"
	Driver           = "pgx"
	NaturalCollation = "nd_natural"
	migrationsFolder = "migrations"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// Db returns the connection pool shared by repositories and plugin storage.
func Db() *sql.DB {
	return singleton.GetInstance(func() *sql.DB {
		cfg, err := pgx.ParseConfig(conf.Server.DatabaseURL)
		if err != nil {
			log.Fatal("Invalid PostgreSQL connection configuration", err)
		}
		cfg.RuntimeParams["application_name"] = "navidrome"
		cfg.RuntimeParams["timezone"] = "UTC"
		cfg.RuntimeParams["search_path"] = pgx.Identifier{conf.Server.DatabaseSchema}.Sanitize() + ",public"
		if cfg.ConnectTimeout == 0 {
			cfg.ConnectTimeout = 10 * time.Second
		}
		conn := stdlib.OpenDB(*cfg, stdlib.OptionAfterConnect(func(_ context.Context, conn *pgx.Conn) error {
			conn.TypeMap().RegisterType(&pgtype.Type{Name: "timestamptz", OID: pgtype.TimestamptzOID,
				Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC}})
			return nil
		}))
		maxConnections := conf.MaxOpenConns()
		conn.SetMaxOpenConns(maxConnections)
		conn.SetMaxIdleConns(maxConnections)
		conn.SetConnMaxIdleTime(5 * time.Minute)
		return conn
	})
}

func Close(ctx context.Context) {
	if err := Db().Close(); err != nil {
		log.Error(context.WithoutCancel(ctx), "Error closing database", err)
	}
}

func Init(ctx context.Context) func() {
	conn := Db()
	if err := conn.PingContext(ctx); err != nil {
		log.Fatal(ctx, "Cannot connect to PostgreSQL", err)
	}
	if err := migrate(ctx, conn); err != nil {
		log.Fatal(ctx, "Failed to initialize PostgreSQL schema", err)
	}
	return func() { Close(ctx) }
}

// Exists checks for an initialized application schema without changing the database.
func Exists(ctx context.Context) (bool, error) {
	var exists bool
	err := Db().QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", pgx.Identifier{conf.Server.DatabaseSchema, "goose_db_version"}.Sanitize()).Scan(&exists)
	return exists, err
}
