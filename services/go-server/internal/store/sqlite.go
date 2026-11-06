package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func NewDB(dbPath string) (*sql.DB, error) {
	if dbPath != ":memory:" && !strings.HasPrefix(dbPath, "file:") {
		dir := filepath.Dir(dbPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("create sqlite dir %s: %w", dir, err)
			}
		}
	}

	dsn := dbPath
	if dbPath != ":memory:" && !strings.Contains(dbPath, "?") {
		dsn = fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", dbPath)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}

	// Configure PRAGMAs for high concurrency to eliminate SQLITE_BUSY lock errors
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("execute pragma %s: %w", p, err)
		}
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return db, nil
}

func runMigrations(db *sql.DB) error {
	initQuery := `CREATE TABLE IF NOT EXISTS schema_migrations (
		version     INTEGER PRIMARY KEY,
		applied_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(initQuery); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	applied := make(map[int]bool)
	for rows.Next() {
		var ver int
		if err := rows.Scan(&ver); err != nil {
			return fmt.Errorf("scan migration version: %w", err)
		}
		applied[ver] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, file := range files {
		var ver int
		parts := strings.SplitN(file, "_", 2)
		if len(parts) > 0 {
			if v, err := strconv.Atoi(parts[0]); err == nil {
				ver = v
			}
		}
		if ver > 0 && applied[ver] {
			continue
		}

		content, err := migrationFiles.ReadFile("migrations/" + file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}

		if err := applyMigration(db, ver, string(content)); err != nil {
			return fmt.Errorf("apply migration %s: %w", file, err)
		}
	}

	return nil
}

func applyMigration(db *sql.DB, version int, query string) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec(query); err != nil {
		return err
	}

	if version > 0 {
		if _, err := tx.Exec("INSERT OR IGNORE INTO schema_migrations (version) VALUES (?)", version); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// TableSchemaSpec defines a required table and its essential columns.
type TableSchemaSpec struct {
	TableName string
	Columns   []string
}

// CriticalTables defines the schema assertions that must pass at startup (ADR-012 D2).
var CriticalTables = []TableSchemaSpec{
	{
		TableName: "schema_migrations",
		Columns:   []string{"version", "applied_at"},
	},
	{
		TableName: "hotset_verdict_history",
		Columns:   []string{"camera_id", "direction", "region_id", "pcu_total", "saturation_index", "severity", "observed_at"},
	},
	{
		TableName: "camera_verdicts",
		Columns:   []string{"camera_id", "observed_at", "severity", "flood_severity"},
	},
	{
		TableName: "camera_capacities",
		Columns:   []string{"camera_id", "learned_max"},
	},
	{
		TableName: "flood_subscriptions",
		Columns:   []string{"id", "channel_name", "chat_id", "target_type", "target_id"},
	},
}

// AssertSchemaReady verifies that all essential database tables and columns exist.
// Returns an error if any table or required column is absent (ADR-012 D2).
func AssertSchemaReady(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("schema assertion failed: database handle is nil")
	}

	for _, spec := range CriticalTables {
		var exists int
		err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", spec.TableName).Scan(&exists)
		if err != nil {
			return fmt.Errorf("schema assertion failed for table %s: %w", spec.TableName, err)
		}
		if exists == 0 {
			return fmt.Errorf("schema assertion failed: table %q is missing (ensure migrations are applied)", spec.TableName)
		}

		rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", spec.TableName))
		if err != nil {
			return fmt.Errorf("schema assertion failed querying columns for table %s: %w", spec.TableName, err)
		}

		existingCols := make(map[string]bool)
		for rows.Next() {
			var cid int
			var name, colType string
			var notNull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err == nil {
				existingCols[name] = true
			}
		}
		rows.Close() //nolint:errcheck

		for _, col := range spec.Columns {
			if !existingCols[col] {
				return fmt.Errorf("schema assertion failed: table %q is missing required column %q", spec.TableName, col)
			}
		}
	}

	return nil
}
