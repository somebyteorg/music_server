package plugins

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/plugins/host"
	"github.com/navidrome/navidrome/utils/slice"
)

const (
	defaultMaxKVStoreSize = 1 * 1024 * 1024 // 1MB default
	maxKeyLength          = 256             // Max key length in bytes
)

// notExpiredFilter is the SQL condition to exclude expired keys.
const notExpiredFilter = "(expires_at IS NULL OR expires_at >= CURRENT_TIMESTAMP)"

const cleanupInterval = 1 * time.Hour

// kvstoreServiceImpl implements the host.KVStoreService interface.
// Rows are isolated by plugin id in the shared database.
type kvstoreServiceImpl struct {
	pluginName string
	db         *pluginDatabase
	maxSize    int64
	ctx        context.Context //nolint:containedctx // service lifecycle
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// newKVStoreService creates a new kvstoreServiceImpl instance using the shared PostgreSQL pool.
// The provided context controls the lifetime of the background cleanup goroutine.
func newKVStoreService(ctx context.Context, pluginName string, perm *KVStorePermission) (*kvstoreServiceImpl, error) {
	// Parse max size from permission, default to 1MB
	maxSize := int64(defaultMaxKVStoreSize)
	if perm != nil && perm.MaxSize != nil && *perm.MaxSize != "" {
		parsed, err := humanize.ParseBytes(*perm.MaxSize)
		if err != nil {
			return nil, fmt.Errorf("invalid maxSize %q: %w", *perm.MaxSize, err)
		}
		if parsed > math.MaxInt64 {
			return nil, fmt.Errorf("maxSize exceeds supported storage size")
		}
		maxSize = int64(parsed)
	}

	database := newPluginDatabase(pluginName)

	cleanupCtx, cancel := context.WithCancel(ctx)
	svc := &kvstoreServiceImpl{
		pluginName: pluginName,
		db:         database,
		maxSize:    maxSize,
		ctx:        cleanupCtx,
		cancel:     cancel,
	}
	svc.wg.Add(1)
	go svc.cleanupLoop(cleanupCtx)
	return svc, nil
}

// storageUsed returns the current total storage used by non-expired keys.
func (s *kvstoreServiceImpl) storageUsed(ctx context.Context) (int64, error) {
	return kvStorageUsed(ctx, s.db)
}

func kvStorageUsed(ctx context.Context, database *pluginDatabase) (int64, error) {
	var used int64
	err := database.QueryRowContext(ctx, `SELECT COALESCE(SUM(size), 0) FROM plugin_kvstore WHERE plugin_id = @plugin AND `+notExpiredFilter).Scan(&used)
	if err != nil {
		return 0, fmt.Errorf("calculating storage used: %w", err)
	}
	return used, nil
}

// checkStorageLimit verifies that adding delta bytes would not exceed the storage limit.
func (s *kvstoreServiceImpl) checkStorageLimit(ctx context.Context, database *pluginDatabase, delta int64) error {
	if delta <= 0 {
		return nil
	}
	used, err := kvStorageUsed(ctx, database)
	if err != nil {
		return err
	}
	newTotal := used + delta
	if newTotal > s.maxSize {
		return fmt.Errorf("storage limit exceeded: would use %s of %s allowed",
			humanize.Bytes(uint64(newTotal)), humanize.Bytes(uint64(s.maxSize)))
	}
	return nil
}

// setValue is the shared implementation for Set and SetWithTTL.
// A ttlSeconds of 0 means no expiration.
func (s *kvstoreServiceImpl) setValue(ctx context.Context, key string, value []byte, ttlSeconds int64) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if len(key) == 0 {
		return fmt.Errorf("key cannot be empty")
	}
	if len(key) > maxKeyLength {
		return fmt.Errorf("key exceeds maximum length of %d bytes", maxKeyLength)
	}

	newValueSize := int64(len(value))

	if value == nil {
		value = []byte{}
	}
	err := s.db.transaction(ctx, func(database *pluginDatabase) error {
		if _, err := database.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(@plugin, 0))"); err != nil {
			return err
		}
		var oldSize int64
		err := database.QueryRowContext(ctx, `SELECT COALESCE(size, 0) FROM plugin_kvstore WHERE plugin_id = @plugin AND key = ? AND `+notExpiredFilter, key).Scan(&oldSize)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("checking existing key: %w", err)
		}
		if err := s.checkStorageLimit(ctx, database, newValueSize-oldSize); err != nil {
			return err
		}
		var expiresAt any
		if ttlSeconds > 0 {
			if ttlSeconds > int64((1<<63-1)/int64(time.Second)) {
				return fmt.Errorf("TTL exceeds supported duration")
			}
			expiresAt = time.Now().UTC().Add(time.Duration(ttlSeconds) * time.Second)
		}
		_, err = database.ExecContext(ctx, `
            INSERT INTO plugin_kvstore (plugin_id, key, value, size, created_at, updated_at, expires_at)
            VALUES (@plugin, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?)
            ON CONFLICT(plugin_id, key) DO UPDATE SET value = excluded.value, size = excluded.size,
                updated_at = CURRENT_TIMESTAMP, expires_at = excluded.expires_at
        `, key, value, newValueSize, expiresAt)
		return err
	})
	if err != nil {
		return fmt.Errorf("storing value: %w", err)
	}

	log.Trace(ctx, "KVStore.Set", "plugin", s.pluginName, "key", key, "size", newValueSize, "ttlSeconds", ttlSeconds)
	return nil
}

// Set stores a byte value with the given key.
func (s *kvstoreServiceImpl) Set(ctx context.Context, key string, value []byte) error {
	return s.setValue(ctx, key, value, 0)
}

// SetWithTTL stores a byte value with the given key and a time-to-live.
func (s *kvstoreServiceImpl) SetWithTTL(ctx context.Context, key string, value []byte, ttlSeconds int64) error {
	if ttlSeconds <= 0 {
		return fmt.Errorf("ttlSeconds must be greater than 0")
	}
	return s.setValue(ctx, key, value, ttlSeconds)
}

// Get retrieves a byte value from storage.
func (s *kvstoreServiceImpl) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, false, err
	}
	var value []byte
	err := s.db.QueryRowContext(ctx, `SELECT value FROM plugin_kvstore WHERE plugin_id = @plugin AND key = ? AND `+notExpiredFilter, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading value: %w", err)
	}

	log.Trace(ctx, "KVStore.Get", "plugin", s.pluginName, "key", key, "found", true)
	return value, true, nil
}

// Delete removes a value from storage.
func (s *kvstoreServiceImpl) Delete(ctx context.Context, key string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM plugin_kvstore WHERE plugin_id = @plugin AND key = ?`, key)
	if err != nil {
		return fmt.Errorf("deleting value: %w", err)
	}

	log.Trace(ctx, "KVStore.Delete", "plugin", s.pluginName, "key", key)
	return nil
}

// Has checks if a key exists in storage.
func (s *kvstoreServiceImpl) Has(ctx context.Context, key string) (bool, error) {
	if err := s.ctx.Err(); err != nil {
		return false, err
	}
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM plugin_kvstore WHERE plugin_id = @plugin AND key = ? AND `+notExpiredFilter, key).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking key: %w", err)
	}

	return count > 0, nil
}

// List returns all keys matching the given prefix.
func (s *kvstoreServiceImpl) List(ctx context.Context, prefix string) ([]string, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	var rows *sql.Rows
	var err error

	if prefix == "" {
		rows, err = s.db.QueryContext(ctx, `SELECT key FROM plugin_kvstore WHERE plugin_id = @plugin AND `+notExpiredFilter+` ORDER BY key`)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT key FROM plugin_kvstore WHERE plugin_id = @plugin AND starts_with(key, ?) AND `+notExpiredFilter+` ORDER BY key`, prefix)
	}
	if err != nil {
		return nil, fmt.Errorf("listing keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scanning key: %w", err)
		}
		keys = append(keys, key)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating keys: %w", err)
	}

	log.Trace(ctx, "KVStore.List", "plugin", s.pluginName, "prefix", prefix, "count", len(keys))
	return keys, nil
}

// GetStorageUsed returns the total storage used by this plugin in bytes.
func (s *kvstoreServiceImpl) GetStorageUsed(ctx context.Context) (int64, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	used, err := s.storageUsed(ctx)
	if err != nil {
		return 0, err
	}
	log.Trace(ctx, "KVStore.GetStorageUsed", "plugin", s.pluginName, "bytes", used)
	return used, nil
}

// DeleteByPrefix removes all keys matching the given prefix.
func (s *kvstoreServiceImpl) DeleteByPrefix(ctx context.Context, prefix string) (int64, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	if prefix == "" {
		return 0, fmt.Errorf("prefix cannot be empty")
	}

	result, err := s.db.ExecContext(ctx, `DELETE FROM plugin_kvstore WHERE plugin_id = @plugin AND starts_with(key, ?)`, prefix)
	if err != nil {
		return 0, fmt.Errorf("deleting keys: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("getting deleted count: %w", err)
	}

	log.Trace(ctx, "KVStore.DeleteByPrefix", "plugin", s.pluginName, "prefix", prefix, "deletedCount", count)
	return count, nil
}

// GetMany retrieves multiple values in a single call, processing keys in batches.
func (s *kvstoreServiceImpl) GetMany(ctx context.Context, keys []string) (map[string][]byte, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return map[string][]byte{}, nil
	}

	const batchSize = 200
	result := make(map[string][]byte)
	for chunk := range slice.CollectChunks(slices.Values(keys), batchSize) {
		placeholders := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, key := range chunk {
			placeholders[i] = "?"
			args[i] = key
		}

		query := `SELECT key, value FROM plugin_kvstore WHERE plugin_id = @plugin AND key IN (` + strings.Join(placeholders, ",") + `) AND ` + notExpiredFilter //nolint:gosec // placeholders are always "?"
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("querying values: %w", err)
		}

		for rows.Next() {
			var key string
			var value []byte
			if err := rows.Scan(&key, &value); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scanning value: %w", err)
			}
			result[key] = value
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterating values: %w", err)
		}
		rows.Close()
	}

	log.Trace(ctx, "KVStore.GetMany", "plugin", s.pluginName, "requested", len(keys), "found", len(result))
	return result, nil
}

// cleanupLoop periodically removes expired keys from the database.
// It stops when the provided context is cancelled.
func (s *kvstoreServiceImpl) cleanupLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupExpired(ctx)
		}
	}
}

// cleanupExpired removes all expired keys from the database to reclaim disk space.
func (s *kvstoreServiceImpl) cleanupExpired(ctx context.Context) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM plugin_kvstore WHERE plugin_id = @plugin AND expires_at IS NOT NULL AND expires_at < CURRENT_TIMESTAMP`)
	if err != nil {
		log.Error(ctx, "KVStore cleanup: failed to delete expired keys", "plugin", s.pluginName, err)
		return
	}
	if count, err := result.RowsAffected(); err == nil && count > 0 {
		log.Debug("KVStore cleanup completed", "plugin", s.pluginName, "deletedKeys", count)
	}
}

// Close stops the cleanup goroutine without closing the shared PostgreSQL pool.
func (s *kvstoreServiceImpl) Close() error {
	log.Debug("Closing plugin kvstore", "plugin", s.pluginName)
	s.cancel()
	s.wg.Wait()
	return nil
}

// Compile-time verification
var _ host.KVStoreService = (*kvstoreServiceImpl)(nil)
