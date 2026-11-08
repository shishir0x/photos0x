package core

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"fmt"
	"strconv"
	"sync"
	"time"
)

type UploadSession struct {
	ID           string              `json:"id"`
	StartedAt    int64               `json:"startedAt"`
	FinishedAt   int64               `json:"finishedAt"`
	Account      string              `json:"account"`
	AlbumName    string              `json:"albumName"`
	TotalFiles   int                 `json:"totalFiles"`
	SuccessCount int                 `json:"successCount"`
	FailCount    int                 `json:"failCount"`
	SkippedCount int                 `json:"skippedCount"`
	TotalBytes   int64               `json:"totalBytes"`
	Items        []UploadSessionItem `json:"items,omitempty"`
}

type UploadSessionItem struct {
	ID           int64  `json:"id"`
	SessionID    string `json:"sessionId"`
	Path         string `json:"path"`
	FileName     string `json:"fileName"`
	Status       string `json:"status"` // "success", "fail", "skipped"
	ErrorMessage string `json:"errorMessage"`
	MediaKey     string `json:"mediaKey"`
	Size         int64  `json:"size"`
	CreatedAt    int64  `json:"createdAt"`
}

var (
	defaultSessionHistoryMu sync.Mutex
	defaultSessionHistory   *SessionHistoryStore
)

// SessionHistoryStore persists upload session results in SQLite.
type SessionHistoryStore struct {
	mu     sync.RWMutex
	db     *sql.DB
	ownsDB bool
}

func GetDefaultSessionHistory() *SessionHistoryStore {
	defaultSessionHistoryMu.Lock()
	defer defaultSessionHistoryMu.Unlock()
	if defaultSessionHistory == nil {
		defaultSessionHistory = NewSessionHistoryStore("")
	}
	return defaultSessionHistory
}

func SetDefaultSessionHistory(s *SessionHistoryStore) {
	defaultSessionHistoryMu.Lock()
	defer defaultSessionHistoryMu.Unlock()
	defaultSessionHistory = s
}

func NewSessionHistoryStore(dbPath string) *SessionHistoryStore {
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

	return &SessionHistoryStore{
		db:     db,
		ownsDB: ownsDB,
	}
}

func (s *SessionHistoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownsDB && s.db != nil {
		err := s.db.Close()
		s.db = nil
		return err
	}
	return nil
}

// RecordSession inserts an upload session and all of its per-file results in a single transaction.
func (s *SessionHistoryStore) RecordSession(session UploadSession, items []UploadSessionItem) error {
	if session.ID == "" {
		session.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if session.StartedAt <= 0 {
		session.StartedAt = time.Now().Unix()
	}
	if session.FinishedAt <= 0 {
		session.FinishedAt = time.Now().Unix()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return fmt.Errorf("session history database not initialized")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin session transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		INSERT INTO upload_sessions (
			id, started_at, finished_at, account, album_name,
			total_files, success_count, fail_count, skipped_count, total_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, session.ID, session.StartedAt, session.FinishedAt, session.Account, session.AlbumName,
		session.TotalFiles, session.SuccessCount, session.FailCount, session.SkippedCount, session.TotalBytes)
	if err != nil {
		return fmt.Errorf("failed to insert upload session: %w", err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO upload_session_items (
			session_id, path, file_name, status, error_message, media_key, size, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare items insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().Unix()
	for _, it := range items {
		createdAt := it.CreatedAt
		if createdAt <= 0 {
			createdAt = now
		}
		_, err := stmt.Exec(session.ID, it.Path, it.FileName, it.Status, it.ErrorMessage, it.MediaKey, it.Size, createdAt)
		if err != nil {
			return fmt.Errorf("failed to insert session item: %w", err)
		}
	}

	return tx.Commit()
}

// ListSessions returns recent upload sessions without item details.
func (s *SessionHistoryStore) ListSessions(limit int) ([]UploadSession, error) {
	if limit <= 0 {
		limit = 50
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return []UploadSession{}, nil
	}

	rows, err := s.db.Query(`
		SELECT id, started_at, finished_at, account, album_name,
		       total_files, success_count, fail_count, skipped_count, total_bytes
		FROM upload_sessions
		ORDER BY started_at DESC
		LIMIT ?;
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list upload sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []UploadSession
	for rows.Next() {
		var sess UploadSession
		if err := rows.Scan(
			&sess.ID, &sess.StartedAt, &sess.FinishedAt, &sess.Account, &sess.AlbumName,
			&sess.TotalFiles, &sess.SuccessCount, &sess.FailCount, &sess.SkippedCount, &sess.TotalBytes,
		); err != nil {
			return nil, fmt.Errorf("failed to scan session row: %w", err)
		}
		sessions = append(sessions, sess)
	}
	if sessions == nil {
		sessions = []UploadSession{}
	}
	return sessions, rows.Err()
}

// GetSessionDetails retrieves a session along with its full list of per-file results.
func (s *SessionHistoryStore) GetSessionDetails(sessionID string) (*UploadSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, fmt.Errorf("session history database not initialized")
	}

	var sess UploadSession
	err := s.db.QueryRow(`
		SELECT id, started_at, finished_at, account, album_name,
		       total_files, success_count, fail_count, skipped_count, total_bytes
		FROM upload_sessions
		WHERE id = ?;
	`, sessionID).Scan(
		&sess.ID, &sess.StartedAt, &sess.FinishedAt, &sess.Account, &sess.AlbumName,
		&sess.TotalFiles, &sess.SuccessCount, &sess.FailCount, &sess.SkippedCount, &sess.TotalBytes,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query session details: %w", err)
	}

	rows, err := s.db.Query(`
		SELECT id, session_id, path, file_name, status, error_message, media_key, size, created_at
		FROM upload_session_items
		WHERE session_id = ?
		ORDER BY id ASC;
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query session items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var it UploadSessionItem
		if err := rows.Scan(
			&it.ID, &it.SessionID, &it.Path, &it.FileName, &it.Status,
			&it.ErrorMessage, &it.MediaKey, &it.Size, &it.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan session item: %w", err)
		}
		sess.Items = append(sess.Items, it)
	}
	if sess.Items == nil {
		sess.Items = []UploadSessionItem{}
	}

	return &sess, rows.Err()
}

// DeleteSession removes a single session and its cascading items.
func (s *SessionHistoryStore) DeleteSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM upload_sessions WHERE id = ?;`, sessionID)
	return err
}

// ClearSessions deletes all sessions and items.
func (s *SessionHistoryStore) ClearSessions() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM upload_sessions;`)
	return err
}

// ExportSessionCSV exports all items of a session formatted as standard RFC 4180 CSV text.
func (s *SessionHistoryStore) ExportSessionCSV(sessionID string) (string, error) {
	sess, err := s.GetSessionDetails(sessionID)
	if err != nil {
		return "", err
	}
	if sess == nil {
		return "", fmt.Errorf("session not found")
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	_ = w.Write([]string{"File Name", "Path", "Status", "Media Key", "Size (Bytes)", "Error Message", "Date"})
	for _, it := range sess.Items {
		dateStr := time.Unix(it.CreatedAt, 0).Format(time.RFC3339)
		_ = w.Write([]string{
			it.FileName,
			it.Path,
			it.Status,
			it.MediaKey,
			strconv.FormatInt(it.Size, 10),
			it.ErrorMessage,
			dateStr,
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("failed to generate CSV: %w", err)
	}

	return buf.String(), nil
}
