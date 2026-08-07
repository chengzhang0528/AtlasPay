package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atlaspay/meridian-gateway/internal/security"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

var (
	ErrNotFound     = errors.New("resource not found")
	ErrConflict     = errors.New("resource conflict")
	ErrInvalidState = errors.New("invalid state transition")
)

type Store struct {
	db  *sql.DB
	enc *security.Encryptor
}

func Open(ctx context.Context, databasePath string, enc *security.Encryptor) (*Store, error) {
	if enc == nil {
		return nil, errors.New("encryptor is required")
	}
	if databasePath != ":memory:" && !strings.HasPrefix(databasePath, "file:") {
		if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	dsn := databasePath
	if databasePath != ":memory:" && !strings.HasPrefix(databasePath, "file:") {
		absolute, err := filepath.Abs(databasePath)
		if err != nil {
			return nil, err
		}
		dsn = "file:" + filepath.ToSlash(absolute)
	}
	if databasePath == ":memory:" {
		dsn = "file:meridian_memory?mode=memory&cache=shared"
	}
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Set("_txlock", "immediate")
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	dsn += separator + query.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	store := &Store{db: db, enc: enc}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) migrate(ctx context.Context) error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer tx.Rollback()
	statementNumber := 0
	for _, raw := range strings.Split(string(schema), ";") {
		statement := strings.TrimSpace(raw)
		if statement == "" {
			continue
		}
		statementNumber++
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply schema statement %d: %w", statementNumber, err)
		}
	}
	hasMerchantToken, err := tableColumnExists(ctx, tx, "merchants", "admin_token_hash")
	if err != nil {
		return fmt.Errorf("inspect merchant admin token migration: %w", err)
	}
	if !hasMerchantToken {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE merchants ADD COLUMN admin_token_hash TEXT`); err != nil {
			return fmt.Errorf("add merchant admin token column: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_merchants_admin_token_hash ON merchants(admin_token_hash) WHERE admin_token_hash IS NOT NULL`); err != nil {
		return fmt.Errorf("create merchant admin token index: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (1, ?)`, now()); err != nil {
		return fmt.Errorf("record schema version: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (2, ?)`, now()); err != nil {
		return fmt.Errorf("record merchant admin token migration: %w", err)
	}
	for _, migration := range []struct {
		table      string
		column     string
		definition string
	}{
		{table: "payments", column: "close_attempts", definition: "close_attempts INTEGER NOT NULL DEFAULT 0"},
		{table: "payments", column: "close_next_attempt_at", definition: "close_next_attempt_at TEXT"},
		{table: "refunds", column: "attempts", definition: "attempts INTEGER NOT NULL DEFAULT 0"},
		{table: "refunds", column: "next_attempt_at", definition: "next_attempt_at TEXT"},
	} {
		exists, err := tableColumnExists(ctx, tx, migration.table, migration.column)
		if err != nil {
			return fmt.Errorf("inspect payment recovery migration: %w", err)
		}
		if !exists {
			statement := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", migration.table, migration.definition)
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("add %s.%s: %w", migration.table, migration.column, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (3, ?)`, now()); err != nil {
		return fmt.Errorf("record payment recovery migration: %w", err)
	}
	return tx.Commit()
}

func tableColumnExists(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	allowed := map[string]bool{"merchants": true, "payments": true, "refunds": true}
	if !allowed[table] {
		return false, fmt.Errorf("unsupported migration table %q", table)
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func retryBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	shift := attempts - 1
	if shift > 12 {
		shift = 12
	}
	delay := time.Second * time.Duration(1<<shift)
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func parseTime(value sql.NullString) time.Time {
	if !value.Valid || value.String == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339Nano, value.String)
	return parsed
}

func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil
	}
	return &parsed
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func (s *Store) decrypt(aad, value string) (string, error) {
	plain, err := s.enc.Open(value, aad)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

type dbtx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
