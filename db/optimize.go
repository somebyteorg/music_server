package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/consts"
	"github.com/navidrome/navidrome/log"
)

var analyzeMux sync.Mutex

// Optimize refreshes PostgreSQL planner statistics after bulk library changes.
func Optimize(ctx context.Context) error {
	analyzeMux.Lock()
	defer analyzeMux.Unlock()
	start := time.Now()
	if err := optimizeAt(ctx, Db(), start); err != nil {
		return err
	}
	log.Info(ctx, "DB analysis complete", "elapsed", time.Since(start))
	return nil
}

// OptimizeIfNeeded refreshes statistics when they are stale or a database-changing operation
// marked them for refresh.
func OptimizeIfNeeded(ctx context.Context) (bool, error) {
	analyzeMux.Lock()
	defer analyzeMux.Unlock()
	start := time.Now()
	ran, err := optimizeIfNeeded(ctx, Db(), start)
	if err != nil || !ran {
		return ran, err
	}
	log.Info(ctx, "DB analysis complete", "elapsed", time.Since(start))
	return true, nil
}

func optimizeIfNeeded(ctx context.Context, db *sql.DB, now time.Time) (bool, error) {
	due, err := optimizeDue(ctx, db, now)
	if err != nil || !due {
		return false, err
	}
	return true, optimizeAt(ctx, db, now)
}

func optimizeDue(ctx context.Context, db *sql.DB, now time.Time) (bool, error) {
	backingOff, err := analyzeRetryBackoffActive(ctx, db, now)
	if err != nil || backingOff {
		return false, err
	}

	pending, found, err := getProperty(ctx, db, consts.DBAnalyzePendingKey)
	if err != nil {
		return false, err
	}
	if found && pending == "1" {
		return true, nil
	}

	value, found, err := getProperty(ctx, db, consts.LastDBAnalyzeAtKey)
	if err != nil {
		return false, err
	}
	if !found {
		return true, nil
	}

	lastAnalyze, valid := parseAnalyzeTime(value)
	if !valid || lastAnalyze.After(now) {
		return true, nil
	}
	return now.Sub(lastAnalyze) >= consts.DBAnalyzeMaxAge, nil
}

func parseAnalyzeTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil
}

func analyzeRetryBackoffActive(ctx context.Context, db *sql.DB, now time.Time) (bool, error) {
	value, found, err := getProperty(ctx, db, consts.DBAnalyzeFailureCountKey)
	if err != nil || !found {
		return false, err
	}
	failures, _ := strconv.Atoi(value)
	if failures < 1 {
		return false, nil
	}

	value, found, err = getProperty(ctx, db, consts.LastDBAnalyzeAttemptAtKey)
	if err != nil || !found {
		return false, err
	}
	lastAttempt, valid := parseAnalyzeTime(value)
	if !valid || lastAttempt.After(now) {
		return false, nil
	}
	return now.Sub(lastAttempt) < analyzeRetryDelay(failures), nil
}

func analyzeRetryDelay(failures int) time.Duration {
	switch failures {
	case 1:
		return 30 * time.Minute
	case 2:
		return time.Hour
	case 3:
		return 2 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// MarkOptimizePending requests a statistics refresh on the next scheduled maintenance check.
func MarkOptimizePending(ctx context.Context) error {
	analyzeMux.Lock()
	defer analyzeMux.Unlock()
	return markOptimizePending(ctx, Db())
}

func markOptimizePending(ctx context.Context, db *sql.DB) error {
	return putProperty(ctx, db, consts.DBAnalyzePendingKey, "1")
}

func optimizeAt(ctx context.Context, db *sql.DB, now time.Time) error {
	if err := markOptimizePending(ctx, db); err != nil {
		return recordAnalyzeError(ctx, db, now, fmt.Errorf("marking ANALYZE pending: %w", err))
	}
	log.Debug(ctx, "Refreshing query planner statistics")
	if err := analyzeInSteps(ctx, db); err != nil {
		return recordAnalyzeError(ctx, db, now, fmt.Errorf("running ANALYZE: %w", err))
	}
	if err := recordAnalyzeSuccess(ctx, db, now); err != nil {
		return recordAnalyzeError(ctx, db, now, err)
	}
	return nil
}

const analyzeTargetsSQL = `SELECT tablename FROM pg_tables WHERE schemaname = current_schema() ORDER BY tablename`

func analyzeInSteps(ctx context.Context, db *sql.DB) error {
	targets, err := analyzeTargets(ctx, db)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if _, err := db.ExecContext(ctx, `ANALYZE "`+strings.ReplaceAll(target, `"`, `""`)+`"`); err != nil {
			return fmt.Errorf("analyzing %s: %w", target, err)
		}
	}
	return nil
}

func analyzeTargets(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, analyzeTargetsSQL)
	if err != nil {
		return nil, fmt.Errorf("listing ANALYZE targets: %w", err)
	}
	defer rows.Close()
	var targets []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("listing ANALYZE targets: %w", err)
		}
		targets = append(targets, name)
	}
	return targets, rows.Err()
}

func recordAnalyzeSuccess(ctx context.Context, pool *sql.DB, now time.Time) error {
	return WithTransaction(ctx, pool, func(tx *sql.Tx) error {
		for _, entry := range []struct{ key, value string }{
			{consts.LastDBAnalyzeAtKey, now.UTC().Format(time.RFC3339Nano)},
			{consts.DBAnalyzePendingKey, "0"}, {consts.DBAnalyzeFailureCountKey, "0"},
		} {
			if err := putProperty(ctx, tx, entry.key, entry.value); err != nil {
				return fmt.Errorf("recording ANALYZE state: %w", err)
			}
		}
		return nil
	})
}

func recordAnalyzeError(ctx context.Context, db *sql.DB, now time.Time, analyzeErr error) error {
	if err := recordAnalyzeFailure(ctx, db, now); err != nil {
		return errors.Join(analyzeErr, fmt.Errorf("recording ANALYZE failure: %w", err))
	}
	return analyzeErr
}

func recordAnalyzeFailure(ctx context.Context, pool *sql.DB, now time.Time) error {
	return WithTransaction(ctx, pool, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO property(id,value) VALUES($1,'1')
            ON CONFLICT(id) DO UPDATE SET value=(greatest(property.value::integer,0)+1)::text`, consts.DBAnalyzeFailureCountKey); err != nil {
			return err
		}
		if err := putProperty(ctx, tx, consts.DBAnalyzePendingKey, "1"); err != nil {
			return err
		}
		return putProperty(ctx, tx, consts.LastDBAnalyzeAttemptAtKey, now.UTC().Format(time.RFC3339Nano))
	})
}

type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type sqlQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func putProperty(ctx context.Context, db sqlExecer, key, value string) error {
	_, err := db.ExecContext(ctx, `insert into property(id, value) values($1, $2)
		on conflict(id) do update set value=excluded.value`, key, value)
	return err
}

func getProperty(ctx context.Context, db sqlQueryer, key string) (string, bool, error) {
	var value string
	err := db.QueryRowContext(ctx, "select value from property where id=$1", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}
