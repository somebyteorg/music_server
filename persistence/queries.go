package persistence

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/lann/builder"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/pocketbase/dbx"
)

func (r sqlRepository) executeSQL(ctx context.Context, sq Sqlizer) (int64, error) {
	query, args, err := r.toSQL(sq)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	var c int64
	res, err := r.db.NewQuery(query).Bind(args).WithContext(ctx).Execute()
	if res != nil {
		c, _ = res.RowsAffected()
	}
	r.logSQL(ctx, query, args, err, c, start)
	return c, err
}

func (r sqlRepository) toSQL(sq Sqlizer) (string, dbx.Params, error) {
	query, args, err := sq.ToSql()
	if err != nil {
		return "", nil, err
	}
	query, params, err := db.NamedParams(query, args)
	return query, dbx.Params(params), err
}

func clearOrder(query SelectBuilder) SelectBuilder {
	return builder.Delete(query, "OrderByParts").(SelectBuilder)
}

func (r sqlRepository) queryOne(ctx context.Context, sq Sqlizer, response any) error {
	query, args, err := r.toSQL(sq)
	if err != nil {
		return err
	}
	start := time.Now()
	err = r.db.NewQuery(query).Bind(args).WithContext(ctx).One(response)
	if errors.Is(err, sql.ErrNoRows) {
		r.logSQL(ctx, query, args, nil, 0, start)
		return model.ErrNotFound
	}
	r.logSQL(ctx, query, args, err, 1, start)
	return err
}

func (r sqlRepository) queryAll(ctx context.Context, sq SelectBuilder, response any) error {
	query, args, err := r.toSQL(sq)
	if err != nil {
		return err
	}
	start := time.Now()
	err = r.db.NewQuery(query).Bind(args).WithContext(ctx).All(response)
	if errors.Is(err, sql.ErrNoRows) {
		r.logSQL(ctx, query, args, nil, -1, start)
		return model.ErrNotFound
	}
	r.logSQL(ctx, query, args, err, int64(reflect.ValueOf(response).Elem().Len()), start)
	return err
}

// queryAllSlice is a helper function to query a single column and return the result in a slice
func (r sqlRepository) queryAllSlice(ctx context.Context, sq SelectBuilder, response any) error {
	query, args, err := r.toSQL(sq)
	if err != nil {
		return err
	}
	start := time.Now()
	err = r.db.NewQuery(query).Bind(args).WithContext(ctx).Column(response)
	if errors.Is(err, sql.ErrNoRows) {
		r.logSQL(ctx, query, args, nil, -1, start)
		return model.ErrNotFound
	}
	r.logSQL(ctx, query, args, err, int64(reflect.ValueOf(response).Elem().Len()), start)
	return err
}

func (r sqlRepository) logSQL(ctx context.Context, sql string, args dbx.Params, err error, rowsAffected int64, start time.Time) {
	elapsed := time.Since(start)
	fields := []any{ctx, "SQL: `" + sql + "`", "args", args, "rowsAffected", rowsAffected, "elapsedTime", elapsed}
	if err == nil || errors.Is(err, context.Canceled) {
		log.Trace(append(fields, err)...)
		return
	}
	if state := db.SQLState(err); state != "" {
		fields = append(fields, "sqlState", state)
	}
	if db.IsRetryable(err) && db.TransactionWillRetry(ctx) {
		log.Warn(append(fields, err)...)
		return
	}
	log.Error(append(fields, err)...)
}
