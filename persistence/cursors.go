package persistence

import (
	"context"
	"fmt"
	"iter"
	"strconv"
	"strings"
	"time"

	. "github.com/Masterminds/squirrel"
	"github.com/lann/builder"
)

const cursorBatchSize = 192

// wrapCursor adapts a cursor over db rows into one over their models. toModel pulls out the row's
// embedded model, which a type parameter can't reach on its own.
func wrapCursor[D, T any](cursor iter.Seq2[D, error], toModel func(D) *T) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		for row, err := range cursor {
			m := toModel(row)
			if m == nil {
				// Don't format row: its String() derefs the nil model (golang/go#81238).
				var zero T
				yield(zero, fmt.Errorf("unexpected nil %T: %w", zero, err))
				return
			}
			if !yield(*m, err) || err != nil {
				return
			}
		}
	}
}

type cursorKey struct {
	id     any
	values []any
}

func cursorBound(query SelectBuilder, field string) (uint64, bool, error) {
	value, ok := builder.Get(query, field)
	if !ok {
		return 0, false, nil
	}
	bound, err := strconv.ParseUint(value.(string), 10, 64)
	return bound, true, err
}

// queryCursor seeks from the previous batch's sort values instead of loading
// every id or increasing OFFSET. Each SQL result is drained before yielding, so
// artwork queries and writes can reuse even a single transaction connection.
// Batches follow the caller's transaction visibility; consumers that mutate rows
// should order by immutable keys rather than changing their own sort values.
func queryCursor[T any](ctx context.Context, r sqlRepository, query SelectBuilder) (iter.Seq2[T, error], error) {
	keyColumn := r.tableName + ".id"
	orders, err := r.cursorOrders(query, keyColumn)
	if err != nil {
		return nil, err
	}
	limit, limited, err := cursorBound(query, "Limit")
	if err != nil {
		return nil, err
	}
	offset, _, err := cursorBound(query, "Offset")
	if err != nil {
		return nil, err
	}
	source := clearOrder(query.RemoveLimit().RemoveOffset()).Column(keyColumn + " AS _cursor_key")
	keys := Select("_cursor_key")
	for _, order := range orders {
		source = source.Column(order.expression + " AS " + order.alias)
		keys = keys.Column(order.alias).OrderBy(order.sqlOrder())
	}
	keys = keys.FromSelect(source, "_cursor_source")
	batchSize := uint64(cursorBatchSize)
	if limited {
		batchSize = min(batchSize, limit)
	}
	first, err := r.queryCursorKeys(ctx, keys.Limit(batchSize).Offset(offset), len(orders))
	if err != nil {
		return nil, err
	}
	return func(yield func(T, error) bool) {
		remaining, size, batch := limit, batchSize, first
		for len(batch) > 0 {
			if err := ctx.Err(); err != nil {
				var zero T
				yield(zero, err)
				return
			}
			ids := make([]any, len(batch))
			var order strings.Builder
			order.WriteString("CASE " + keyColumn)
			for i, key := range batch {
				ids[i] = key.id
				fmt.Fprintf(&order, " WHEN ? THEN %d", i)
			}
			order.WriteString(" END")
			page := clearOrder(query.RemoveLimit().RemoveOffset()).Where(Eq{keyColumn: ids}).OrderByClause(Expr(order.String(), ids...))
			var rows []T
			if err := r.queryAll(ctx, page, &rows); err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, row := range rows {
				if !yield(row, nil) {
					return
				}
			}
			if limited {
				remaining -= uint64(len(batch))
				if remaining == 0 {
					return
				}
			}
			if uint64(len(batch)) < size {
				return
			}
			boundary := batch[len(batch)-1].values
			size = uint64(cursorBatchSize)
			if limited {
				size = min(size, remaining)
			}
			next, err := seekCursorPage(keys, orders, boundary, size)
			if err == nil {
				batch, err = r.queryCursorKeys(ctx, next, len(orders))
			}
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
		}
	}, nil
}

// Each branch is independently limited before merging. PostgreSQL can seek into
// the matching index range and stop, rather than filtering every earlier row.
func seekCursorPage(query SelectBuilder, orders []cursorOrder, values []any, size uint64) (Sqlizer, error) {
	ranges := cursorRanges(orders, values)
	if len(ranges) == 1 {
		return query.Where(ranges[0]).Limit(size), nil
	}
	var statements []string
	var args []any
	for _, condition := range ranges {
		sql, params, err := query.Where(condition).Limit(size).ToSql()
		if err != nil {
			return nil, err
		}
		statements = append(statements, "("+sql+")")
		args = append(args, params...)
	}
	clauses := make([]string, len(orders))
	for i, order := range orders {
		clauses[i] = order.sqlOrder()
	}
	return Expr("SELECT * FROM ("+strings.Join(statements, " UNION ALL ")+
		") AS _cursor_page ORDER BY "+strings.Join(clauses, ", ")+fmt.Sprintf(" LIMIT %d", size), args...), nil
}

func (r sqlRepository) queryCursorKeys(ctx context.Context, query Sqlizer, sortKeys int) (keys []cursorKey, err error) {
	sql, args, err := r.toSQL(query)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	defer func() { r.logSQL(ctx, sql, args, err, int64(len(keys)), start) }()
	rows, err := r.db.NewQuery(sql).Bind(args).WithContext(ctx).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		values, refs := make([]any, sortKeys+1), make([]any, sortKeys+1)
		for i := range values {
			refs[i] = &values[i]
		}
		if err := rows.Scan(refs...); err != nil {
			return nil, err
		}
		keys = append(keys, cursorKey{id: values[0], values: values[1:]})
	}
	return keys, rows.Err()
}
