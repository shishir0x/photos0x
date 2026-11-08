package core

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionHistoryStore(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "sessions_test.db")

	store := NewSessionHistoryStore(dbPath)
	defer func() { _ = store.Close() }()

	// 1. Initial list empty
	list, err := store.ListSessions(10)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(list))
	}

	// 2. Record a session with items
	sessID := "test-session-1"
	items := []UploadSessionItem{
		{
			Path:      "C:\\photos\\img1.jpg",
			FileName:  "img1.jpg",
			Status:    "success",
			MediaKey:  "AF1QipKey1",
			Size:      1024,
			CreatedAt: 1000,
		},
		{
			Path:         "C:\\photos\\img2.jpg",
			FileName:     "img2.jpg",
			Status:       "fail",
			ErrorMessage: "Network timeout",
			Size:         2048,
			CreatedAt:    1001,
		},
	}
	session := UploadSession{
		ID:           sessID,
		StartedAt:    1000,
		FinishedAt:   1005,
		Account:      "user@example.com",
		AlbumName:    "Vacation",
		TotalFiles:   2,
		SuccessCount: 1,
		FailCount:    1,
		SkippedCount: 0,
		TotalBytes:   3072,
	}

	if err := store.RecordSession(session, items); err != nil {
		t.Fatalf("RecordSession failed: %v", err)
	}

	// 3. List sessions
	sessions, err := store.ListSessions(10)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d (err: %v)", len(sessions), err)
	}
	if sessions[0].ID != sessID || sessions[0].AlbumName != "Vacation" || sessions[0].SuccessCount != 1 {
		t.Fatalf("unexpected session in list: %+v", sessions[0])
	}

	// 4. Get session details with items
	details, err := store.GetSessionDetails(sessID)
	if err != nil || details == nil {
		t.Fatalf("GetSessionDetails failed: %v", err)
	}
	if len(details.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(details.Items))
	}
	if details.Items[0].FileName != "img1.jpg" || details.Items[1].Status != "fail" {
		t.Fatalf("unexpected items: %+v", details.Items)
	}

	// 5. Export session CSV
	csvText, err := store.ExportSessionCSV(sessID)
	if err != nil {
		t.Fatalf("ExportSessionCSV failed: %v", err)
	}
	if !strings.Contains(csvText, "img1.jpg") || !strings.Contains(csvText, "Network timeout") {
		t.Fatalf("CSV content missing expected data:\n%s", csvText)
	}

	// 6. Clear sessions
	if err := store.ClearSessions(); err != nil {
		t.Fatalf("ClearSessions failed: %v", err)
	}
	sessionsAfterClear, err := store.ListSessions(10)
	if err != nil || len(sessionsAfterClear) != 0 {
		t.Fatalf("expected 0 sessions after clear, got %d", len(sessionsAfterClear))
	}
}

