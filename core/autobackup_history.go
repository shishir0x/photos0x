package core

import (
	"database/sql"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// BackupFileRecord stores file metadata at the time of a successful upload.
type BackupFileRecord struct {
	Size    int64 `json:"size"`
	ModTime int64 `json:"modTime"`
}

// AutoBackupHistory maintains an SQLite-backed persistent record of uploaded files
// to prevent duplicate uploads across application restarts.
type AutoBackupHistory struct {
	mu     sync.RWMutex
	db     *sql.DB
	ownsDB bool
}

func normalizeHistoryKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	clean := filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
}

// NewAutoBackupHistory initializes the SQLite history store using the app database,
// or a custom/in-memory database path for isolated testing.
func NewAutoBackupHistory(dbPath string) *AutoBackupHistory {
	var db *sql.DB
	var err error
	ownsDB := false

	if dbPath == ":memory:" {
		db, _ = OpenDatabase(":memory:")
		ownsDB = true
	} else if dbPath != "" {
		db, err = OpenDatabase(dbPath)
		if err != nil {
			db, _ = OpenDatabase(":memory:")
		}
		ownsDB = true
	} else {
		db, err = GetAppDatabase()
		if err != nil {
			db, _ = OpenDatabase(":memory:")
			ownsDB = true
		}
	}

	return &AutoBackupHistory{
		db:     db,
		ownsDB: ownsDB,
	}
}

// NewAutoBackupHistoryWithDB creates an AutoBackupHistory instance using an existing db handle.
func NewAutoBackupHistoryWithDB(db *sql.DB) *AutoBackupHistory {
	return &AutoBackupHistory{db: db, ownsDB: false}
}

// Close closes the underlying database if owned by this instance.
func (h *AutoBackupHistory) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.ownsDB && h.db != nil {
		err := h.db.Close()
		h.db = nil
		return err
	}
	return nil
}

// Load is a no-op kept for backward compatibility with existing callers.
func (h *AutoBackupHistory) Load() {}

// Save is a no-op kept for backward compatibility since writes are committed immediately.
func (h *AutoBackupHistory) Save() error {
	return nil
}

// IsUploaded reports whether a file with identical size and modification time
// has already been recorded in SQLite.
func (h *AutoBackupHistory) IsUploaded(path string, size int64, modTime int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.db == nil {
		return false
	}

	key := normalizeHistoryKey(path)
	var dummy int
	err := h.db.QueryRow(`
		SELECT 1 FROM backup_history
		WHERE path = ? AND size = ? AND mod_time = ?
		LIMIT 1;
	`, key, size, modTime).Scan(&dummy)

	return err == nil
}

// MarkUploaded records that the file was successfully uploaded or confirmed remote.
func (h *AutoBackupHistory) MarkUploaded(path string, size int64, modTime int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.db == nil {
		return nil
	}

	key := normalizeHistoryKey(path)
	now := time.Now().Unix()

	_, err := h.db.Exec(`
		INSERT INTO backup_history (path, size, mod_time, uploaded_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			size = excluded.size,
			mod_time = excluded.mod_time,
			uploaded_at = excluded.uploaded_at;
	`, key, size, modTime, now)
	if err != nil {
		slog.Default().Error("auto-backup: failed to record file in history database", "path", path, "err", err)
	}
	return err
}

// Invalidate removes a file record from history so it will be reconsidered on next scan.
func (h *AutoBackupHistory) Invalidate(path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.db == nil {
		return nil
	}

	key := normalizeHistoryKey(path)
	_, err := h.db.Exec(`DELETE FROM backup_history WHERE path = ?;`, key)
	if err != nil {
		slog.Default().Error("auto-backup: failed to delete file from history database", "path", path, "err", err)
	}
	return err
}

// Count returns the number of recorded uploaded files in SQLite.
func (h *AutoBackupHistory) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.db == nil {
		return 0
	}

	var count int
	err := h.db.QueryRow(`SELECT COUNT(*) FROM backup_history;`).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}
