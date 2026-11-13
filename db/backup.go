package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/log"
)

const (
	backupPrefix       = "navidrome_backup"
	backupRegexString  = "^" + backupPrefix + "_(.+)\\.dump$"
	backupSuffixLayout = "2006.01.02_15.04.05.000000000"
)

var backupRegex = regexp.MustCompile(backupRegexString)

func backupPath(now time.Time) string {
	return filepath.Join(conf.Server.Backup.Path.String(), fmt.Sprintf("%s_%s.dump", backupPrefix, now.Format(backupSuffixLayout)))
}

// postgresCommand keeps the connection password out of command-line arguments.
func postgresCommand(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	cfg, err := pgx.ParseConfig(conf.Server.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid database configuration")
	}
	connection, err := url.Parse(conf.Server.DatabaseURL)
	if err != nil || (connection.Scheme != "postgres" && connection.Scheme != "postgresql") {
		return nil, fmt.Errorf("DatabaseURL must be a PostgreSQL URL for backup commands")
	}
	connection.User = url.User(cfg.User)
	query := connection.Query()
	query.Del("password")
	connection.RawQuery = query.Encode()
	command := exec.CommandContext(ctx, name, append([]string{"--dbname=" + connection.String()}, args...)...)
	command.Env = append(os.Environ(), "PGPASSWORD="+cfg.Password)
	return command, nil
}

func Backup(ctx context.Context) (string, error) {
	directory, err := conf.Server.Backup.Path.Path()
	if err != nil {
		return "", err
	}
	if directory == "" {
		directory = "."
	}
	path := backupPath(time.Now())
	command, err := postgresCommand(ctx, "pg_dump", "--format=custom", "--no-owner", "--no-privileges",
		"--strict-names", "--schema="+conf.Server.DatabaseSchema)
	if err != nil {
		return "", err
	}
	output, err := os.CreateTemp(directory, ".navidrome-backup-*.partial")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(output.Name()) }()
	command.Stdout = output
	var stderr strings.Builder
	command.Stderr = &stderr
	runErr := command.Run()
	var syncErr error
	if runErr == nil {
		syncErr = output.Sync()
	}
	closeErr := output.Close()
	if err := errors.Join(runErr, syncErr, closeErr); err != nil {
		return "", fmt.Errorf("pg_dump failed: %w: %s", err, stderr.String())
	}
	// Publish only complete archives so pruning cannot see an in-progress dump.
	if err := os.Rename(output.Name(), path); err != nil {
		return "", fmt.Errorf("publishing database backup: %w", err)
	}
	return path, nil
}

func Restore(ctx context.Context, path string, integrityCheck bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	var header [5]byte
	_, err = io.ReadFull(file, header[:])
	_ = file.Close()
	if err != nil || string(header[:]) != "PGDMP" {
		return fmt.Errorf("backup is not a PostgreSQL custom archive")
	}
	listing, err := exec.CommandContext(ctx, "pg_restore", "--list", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reading backup archive: %w: %s", err, listing)
	}
	for _, table := range []string{"goose_db_version", "media_file", "app_user"} {
		expected := " TABLE " + conf.Server.DatabaseSchema + " " + table + " "
		if !strings.Contains(string(listing), expected) {
			return fmt.Errorf("backup does not contain %s in schema %s", table, conf.Server.DatabaseSchema)
		}
	}
	if integrityCheck {
		command := exec.CommandContext(ctx, "pg_restore", "--file=-", path)
		command.Stdout = io.Discard
		var stderr strings.Builder
		command.Stderr = &stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("reading full backup archive: %w: %s", err, stderr.String())
		}
	}
	// Schema-filtered restores do not recreate the namespace itself, and dumps
	// exclude extensions in public. Prepare both before restoring into an empty DB.
	if err := WithTransaction(ctx, Db(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{conf.Server.DatabaseSchema}.Sanitize())
		return err
	}); err != nil {
		return fmt.Errorf("preparing PostgreSQL backup dependencies: %w", err)
	}
	command, err := postgresCommand(ctx, "pg_restore", "--clean", "--if-exists", "--single-transaction", "--exit-on-error",
		"--no-owner", "--no-privileges", "--strict-names", "--schema="+conf.Server.DatabaseSchema, path)
	if err != nil {
		return err
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_restore failed: %w: %s", err, output)
	}
	return nil
}

func Prune(ctx context.Context) (int, error) {
	if conf.Server.Backup.Count < 0 {
		return 0, fmt.Errorf("backup count cannot be negative")
	}
	backupDir, err := conf.Server.Backup.Path.Path()
	if err != nil {
		return 0, fmt.Errorf("backup directory not available: %w", err)
	}
	files, err := os.ReadDir(backupDir)
	if err != nil {
		return 0, fmt.Errorf("unable to read database backup entries: %w", err)
	}

	type archive struct {
		name string
		time time.Time
	}
	var backups []archive

	for _, file := range files {
		if !file.IsDir() {
			submatch := backupRegex.FindStringSubmatch(file.Name())
			if len(submatch) == 2 {
				timestamp, err := time.Parse(backupSuffixLayout, submatch[1])
				if err != nil {
					timestamp, err = time.Parse("2006.01.02_15.04.05", submatch[1])
				}
				if err == nil {
					backups = append(backups, archive{name: file.Name(), time: timestamp})
				}
			}
		}
	}

	if len(backups) <= conf.Server.Backup.Count {
		return 0, nil
	}

	slices.SortFunc(backups, func(a, b archive) int {
		return b.time.Compare(a.time)
	})

	pruneCount := 0
	var errs []error

	for _, backup := range backups[conf.Server.Backup.Count:] {
		if err := ctx.Err(); err != nil {
			return pruneCount, errors.Join(append(errs, err)...)
		}
		log.Debug(ctx, "Pruning backup", "time", backup.time)
		path := filepath.Join(backupDir, backup.name)
		err = os.Remove(path)
		if err != nil {
			errs = append(errs, err)
		} else {
			pruneCount++
		}
	}

	if len(errs) > 0 {
		err = errors.Join(errs...)
		log.Error(ctx, "Failed to delete one or more files", "errors", err)
	}

	return pruneCount, err
}
