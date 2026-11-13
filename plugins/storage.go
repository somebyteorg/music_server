package plugins

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/navidrome/navidrome/db"
)

// pluginDatabase shares the application pool. Every storage query explicitly
// scopes its rows with @plugin; only this binding layer knows the plugin id.
type pluginDatabase struct {
	conn     db.Executor
	pluginID string
}

func newPluginDatabase(pluginID string) *pluginDatabase {
	return &pluginDatabase{conn: db.Db(), pluginID: pluginID}
}

func (p *pluginDatabase) bind(query string, args []any) (string, []any, error) {
	query, args, err := db.PositionalParams(query, args)
	if err != nil {
		return "", nil, err
	}
	if !strings.Contains(query, "@plugin") {
		return "", nil, fmt.Errorf("plugin storage query has no plugin scope")
	}
	query = strings.ReplaceAll(query, "@plugin", fmt.Sprintf("$%d", len(args)+1))
	return query, append(args, p.pluginID), nil
}

func (p *pluginDatabase) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	query, args, err := p.bind(query, args)
	if err != nil {
		return nil, err
	}
	return p.conn.ExecContext(ctx, query, args...)
}

func (p *pluginDatabase) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	query, args, err := p.bind(query, args)
	if err != nil {
		return nil, err
	}
	return p.conn.QueryContext(ctx, query, args...)
}

type scanResult interface{ Scan(...any) error }
type failedScan struct{ err error }

func (f failedScan) Scan(...any) error { return f.err }

func (p *pluginDatabase) QueryRowContext(ctx context.Context, query string, args ...any) scanResult {
	query, args, err := p.bind(query, args)
	if err != nil {
		return failedScan{err}
	}
	return p.conn.QueryRowContext(ctx, query, args...)
}

func (p *pluginDatabase) transaction(ctx context.Context, block func(*pluginDatabase) error) error {
	return db.WithTransaction(ctx, p.conn, func(tx *sql.Tx) error { return block(&pluginDatabase{conn: tx, pluginID: p.pluginID}) })
}
