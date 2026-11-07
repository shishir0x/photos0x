package core

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	appDBMu sync.Mutex
	appDB   *sql.DB
)

// DefaultDatabasePath resolves the location of the photos0x.db SQLite database
// adhering to the same portable-mode and config directory rules as photos0x.config / gotohp.config.
func DefaultDatabasePath() string {
	ensureConfigLoaded()
	cfgDir := filepath.Dir(ConfigPath)
	if cfgDir == "" || cfgDir == "." {
		userConfigDir := getUserConfigDir()
		p1 := filepath.Join(userConfigDir, "photos0x", "photos0x.db")
		if _, err := os.Stat(p1); err == nil {
			return p1
		}
		p2 := filepath.Join(userConfigDir, "gotohp", "gotohp.db")
		if _, err := os.Stat(p2); err == nil {
			return p2
		}
		return p1
	}

	p1 := filepath.Join(cfgDir, "photos0x.db")
	if _, err := os.Stat(p1); err == nil {
		return p1
	}
	p2 := filepath.Join(cfgDir, "gotohp.db")
	if _, err := os.Stat(p2); err == nil {
		return p2
	}
	return p1
}

// OpenDatabase opens or creates an SQLite database at dbPath and runs migrations.
// Configures WAL mode, busy timeout, and foreign keys.
func OpenDatabase(dbPath string) (*sql.DB, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	dsn := dbPath
	if dbPath != ":memory:" {
		dsn = fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", dbPath)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Limit idle connections for embedded SQLite
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to connect to sqlite database: %w", err)
	}

	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	return db, nil
}

// GetAppDatabase returns the shared application SQLite database instance,
// initializing it if necessary.
func GetAppDatabase() (*sql.DB, error) {
	appDBMu.Lock()
	defer appDBMu.Unlock()

	if appDB != nil {
		return appDB, nil
	}

	dbPath := DefaultDatabasePath()
	db, err := OpenDatabase(dbPath)
	if err != nil {
		return nil, err
	}
	appDB = db
	return appDB, nil
}

// CloseAppDatabase cleanly shuts down the shared application database.
func CloseAppDatabase() error {
	appDBMu.Lock()
	defer appDBMu.Unlock()

	if appDB != nil {
		err := appDB.Close()
		appDB = nil
		return err
	}
	return nil
}

func runMigrations(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	var currentVersion int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations;`)
	if err := row.Scan(&currentVersion); err != nil {
		return fmt.Errorf("failed to query schema version: %w", err)
	}

	// Migration 1: backup_history table
	if currentVersion < 1 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		_, err = tx.Exec(`
			CREATE TABLE IF NOT EXISTS backup_history (
				path TEXT PRIMARY KEY,
				size INTEGER NOT NULL,
				mod_time INTEGER NOT NULL,
				uploaded_at INTEGER NOT NULL
			);
			CREATE INDEX IF NOT EXISTS idx_backup_history_lookup ON backup_history(path, size, mod_time);
			INSERT INTO schema_migrations (version, applied_at) VALUES (1, unixepoch());
		`)
		if err != nil {
			return fmt.Errorf("migration 1 failed: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration 1: %w", err)
		}
	}

	return nil
}

// MigrateJSONHistory reads a legacy autobackup_history.json file and safely imports
// all entries into the SQLite backup_history table within a single transaction.
// Upon successful commit, renames the JSON file to .json.bak.
func MigrateJSONHistory(db *sql.DB, jsonPath string) error {
	if db == nil || jsonPath == "" {
		return nil
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No legacy JSON history exists, clean state
		}
		return fmt.Errorf("cannot read legacy json history: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 0 {
		// Empty legacy history file, rename to .bak if possible
		bakPath := jsonPath + ".bak"
		if _, err := os.Stat(bakPath); os.IsNotExist(err) {
			_ = os.Rename(jsonPath, bakPath)
		}
		return nil
	}

	var records map[string]BackupFileRecord
	if err := json.Unmarshal([]byte(trimmed), &records); err != nil {
		return fmt.Errorf("malformed legacy json history in %s: %w", jsonPath, err)
	}

	if len(records) == 0 {
		// Empty legacy history, rename to .bak if possible
		bakPath := jsonPath + ".bak"
		if _, err := os.Stat(bakPath); os.IsNotExist(err) {
			_ = os.Rename(jsonPath, bakPath)
		}
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT INTO backup_history (path, size, mod_time, uploaded_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(path) DO NOTHING;
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().Unix()
	for path, rec := range records {
		normKey := normalizeHistoryKey(path)
		if normKey == "" || rec.Size < 0 {
			continue
		}
		if _, err := stmt.Exec(normKey, rec.Size, rec.ModTime, now); err != nil {
			return fmt.Errorf("failed to insert migrated record for %s: %w", path, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration transaction: %w", err)
	}

	// Safely archive the JSON file as .bak only after successful commit
	bakPath := jsonPath + ".bak"
	if _, err := os.Stat(bakPath); os.IsNotExist(err) {
		_ = os.Rename(jsonPath, bakPath)
	}

	return nil
}
