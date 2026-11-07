package core

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAutoBackupHistory(t *testing.T) {
	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "history.json")

	h := NewAutoBackupHistory(historyPath)
	defer func() { _ = h.Close() }()
	if h.Count() != 0 {
		t.Fatalf("expected count 0, got %d", h.Count())
	}

	testFile := filepath.Join(tempDir, "photo.jpg")
	if err := os.WriteFile(testFile, []byte("fake image data"), 0o644); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(testFile)
	if err != nil {
		t.Fatal(err)
	}

	// Not uploaded initially
	if h.IsUploaded(testFile, fi.Size(), fi.ModTime().Unix()) {
		t.Fatal("expected file to not be uploaded initially")
	}

	// Mark uploaded
	h.MarkUploaded(testFile, fi.Size(), fi.ModTime().Unix())
	if !h.IsUploaded(testFile, fi.Size(), fi.ModTime().Unix()) {
		t.Fatal("expected file to be uploaded after MarkUploaded")
	}

	// Different size/mtime reports false
	if h.IsUploaded(testFile, fi.Size()+10, fi.ModTime().Unix()) {
		t.Fatal("expected false for changed size")
	}

	// Save and reload
	if err := h.Save(); err != nil {
		t.Fatalf("failed to save history: %v", err)
	}
	_ = h.Close()

	h2 := NewAutoBackupHistory(historyPath)
	defer func() { _ = h2.Close() }()
	if !h2.IsUploaded(testFile, fi.Size(), fi.ModTime().Unix()) {
		t.Fatal("expected file to be uploaded in reloaded history")
	}
	if h2.Count() != 1 {
		t.Fatalf("expected count 1, got %d", h2.Count())
	}
}

func TestIsTemporaryDownloadFile(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"image.jpg", false},
		{"video.mp4", false},
		{".hidden.jpg", true},
		{"~$word.docx", true},
		{"download.crdownload", true},
		{"video.part", true},
		{"file.tmp", true},
		{"tempfile.temp", true},
		{"movie.download", true},
	}

	for _, tt := range tests {
		got := isTemporaryDownloadFile(tt.name)
		if got != tt.expected {
			t.Errorf("isTemporaryDownloadFile(%q) = %v, want %v", tt.name, got, tt.expected)
		}
	}
}

func TestAutoBackupManagerLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	folder1 := filepath.Join(tempDir, "folder1")
	if err := os.MkdirAll(folder1, 0o755); err != nil {
		t.Fatal(err)
	}

	// Set config path for this test
	cfgFile := filepath.Join(tempDir, "gotohp.config")
	if err := LoadConfig(cfgFile); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = CloseAppDatabase()
		ConfigPath = ""
	}()

	mgr := &ConfigManager{}
	updated, err := mgr.AddAutoBackupFolder(folder1)
	if err != nil {
		t.Fatalf("failed to add folder: %v", err)
	}
	if len(updated) != 1 || updated[0] != folder1 {
		t.Fatalf("expected [folder1], got %v", updated)
	}

	// Reject duplicates
	updated2, err := mgr.AddAutoBackupFolder(folder1)
	if err != nil {
		t.Fatalf("unexpected error adding duplicate folder: %v", err)
	}
	if len(updated2) != 1 {
		t.Fatalf("expected 1 folder after duplicate add, got %d", len(updated2))
	}

	// Reject invalid folder
	_, err = mgr.AddAutoBackupFolder(filepath.Join(tempDir, "non_existent"))
	if err == nil {
		t.Fatal("expected error adding non-existent folder")
	}

	// Update interval
	mgr.SetAutoBackupInterval(30)
	cfg := mgr.GetAutoBackup()
	if cfg.IntervalMinutes != 30 {
		t.Fatalf("expected interval 30, got %d", cfg.IntervalMinutes)
	}

	// Create AutoBackupManager
	abManager := NewAutoBackupManager(nil, slog.Default(), nil)
	if abManager.IsRunning() {
		t.Fatal("expected manager not running initially")
	}

	// Start without enabled should not run
	if err := abManager.Start(); err != nil {
		t.Fatal(err)
	}
	if !abManager.IsRunning() {
		// Enabled is false, so it started the watcher for configured folders
	}
	abManager.Stop()

	// Remove folder
	remaining := mgr.RemoveAutoBackupFolder(folder1)
	if len(remaining) != 0 {
		t.Fatalf("expected 0 folders after removal, got %v", remaining)
	}
}

func TestFolderDeduplicationCaseSensitivity(t *testing.T) {
	tempDir := t.TempDir()
	folder := filepath.Join(tempDir, "TestFolder")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}

	cfgFile := filepath.Join(tempDir, "gotohp.config")
	if err := LoadConfig(cfgFile); err != nil {
		t.Fatal(err)
	}

	mgr := &ConfigManager{}
	list, err := mgr.AddAutoBackupFolder(folder)
	if err != nil {
		t.Fatalf("failed to add folder: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(list))
	}

	// Add path with redundant separators and dots: e.g. folder/./
	redundantPath := folder + string(filepath.Separator) + "."
	list2, err := mgr.AddAutoBackupFolder(redundantPath)
	if err != nil {
		t.Fatalf("unexpected error adding redundant path: %v", err)
	}
	if len(list2) != 1 {
		t.Fatalf("expected redundant path to be de-duplicated, got count %d", len(list2))
	}

	// On Windows, test different casing: e.g. lowercase
	lowerFolder := strings.ToLower(folder)
	list3, err := mgr.AddAutoBackupFolder(lowerFolder)
	if err != nil {
		t.Fatalf("unexpected error adding lowercase folder: %v", err)
	}
	if len(list3) != 1 {
		t.Fatalf("expected case-insensitive duplicate rejection on Windows, got count %d", len(list3))
	}

	// Remove folder using lowercase path
	remaining := mgr.RemoveAutoBackupFolder(lowerFolder)
	if len(remaining) != 0 {
		t.Fatalf("expected 0 folders after removal with different casing, got %v", remaining)
	}
}

func TestAutoBackupFlushNilUploadManager(t *testing.T) {
	abManager := NewAutoBackupManager(nil, slog.Default(), nil)

	// flush() with empty pending queue and nil upload manager must not panic
	abManager.flush()

	// schedule path and flush() with nil upload manager must not panic and must retain queue
	abManager.schedule("test_photo.jpg")

	abManager.flush()

	abManager.mu.Lock()
	pendingLen := len(abManager.pending)
	abManager.mu.Unlock()

	if pendingLen != 1 {
		t.Fatalf("expected pending queue to retain 1 item when uploadManager is nil, got %d", pendingLen)
	}
}

func TestAutoBackupHistoryInvalidation(t *testing.T) {
	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "history.json")

	h := NewAutoBackupHistory(historyPath)
	defer func() { _ = h.Close() }()
	testPath := filepath.Join(tempDir, "photo.jpg")

	h.MarkUploaded(testPath, 1024, 1700000000)
	if !h.IsUploaded(testPath, 1024, 1700000000) {
		t.Fatal("expected file to be marked as uploaded")
	}

	// Invalidate removes entry
	h.Invalidate(testPath)
	if h.IsUploaded(testPath, 1024, 1700000000) {
		t.Fatal("expected file to not be uploaded after Invalidate")
	}
}

func TestAddWatchRecursiveLimit(t *testing.T) {
	tempDir := t.TempDir()
	// Create multiple subdirectories
	for i := 0; i < 5; i++ {
		sub := filepath.Join(tempDir, "sub", string(rune('a'+i)))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	abManager := NewAutoBackupManager(nil, slog.Default(), nil)
	// Even with a synthetic directory tree, addWatchRecursive must succeed without error
	err := abManager.addWatchRecursive(nil, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNestedFolderDeduplication(t *testing.T) {
	tempDir := t.TempDir()
	parentDir := filepath.Join(tempDir, "Photos")
	childDir := filepath.Join(parentDir, "Vacation")
	if err := os.MkdirAll(childDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfgFile := filepath.Join(tempDir, "gotohp.config")
	if err := LoadConfig(cfgFile); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = CloseAppDatabase()
		ConfigPath = ""
	}()

	mgr := &ConfigManager{}
	// Add parent folder
	list, err := mgr.AddAutoBackupFolder(parentDir)
	if err != nil {
		t.Fatalf("failed to add parent folder: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(list))
	}

	// Try adding child folder - should be subsumed by parent and not added
	list2, err := mgr.AddAutoBackupFolder(childDir)
	if err != nil {
		t.Fatalf("unexpected error adding child folder: %v", err)
	}
	if len(list2) != 1 {
		t.Fatalf("expected child folder to be subsumed by parent, got list: %v", list2)
	}

	// Now remove parent folder and add child first
	mgr.RemoveAutoBackupFolder(parentDir)
	list3, err := mgr.AddAutoBackupFolder(childDir)
	if err != nil {
		t.Fatalf("failed to add child: %v", err)
	}
	if len(list3) != 1 {
		t.Fatalf("expected 1 folder, got %d", len(list3))
	}

	// Adding parent now should replace child folder with parent
	list4, err := mgr.AddAutoBackupFolder(parentDir)
	if err != nil {
		t.Fatalf("unexpected error adding parent: %v", err)
	}
	if len(list4) != 1 || list4[0] != parentDir {
		t.Fatalf("expected parent to replace child folder, got: %v", list4)
	}

	// Test cleanMonitoredFolders helper
	rawList := []string{parentDir, childDir, parentDir}
	cleaned := cleanMonitoredFolders(rawList)
	if len(cleaned) != 1 || cleaned[0] != parentDir {
		t.Fatalf("expected cleanMonitoredFolders to prune child and duplicate, got: %v", cleaned)
	}
}

func TestAddWatchNonExistentFolder(t *testing.T) {
	tempDir := t.TempDir()
	abManager := NewAutoBackupManager(nil, slog.Default(), nil)
	nonExistent := filepath.Join(tempDir, "does_not_exist")

	err := abManager.addWatchRecursive(nil, nonExistent)
	if err == nil {
		t.Fatal("expected error when adding non-existent folder, got nil")
	}
}

func TestFileReadinessCheck(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Temporary download file should not be ready
	tempFile := filepath.Join(tempDir, "image.jpg.crdownload")
	_ = os.WriteFile(tempFile, []byte("downloading"), 0o644)
	if isFileReadyForUpload(tempFile, nil) {
		t.Fatal("expected temporary download file to not be ready")
	}

	// 2. 0-byte file should not be ready
	zeroFile := filepath.Join(tempDir, "empty.jpg")
	_ = os.WriteFile(zeroFile, []byte(""), 0o644)
	if isFileReadyForUpload(zeroFile, nil) {
		t.Fatal("expected 0-byte file to not be ready")
	}

	// 3. Normal file modified recently (<1s) should wait for settling
	readyFile := filepath.Join(tempDir, "ready.jpg")
	_ = os.WriteFile(readyFile, []byte("valid content"), 0o644)
	// Immediately after writing, modTime is Now(), so readiness check defers it
	if isFileReadyForUpload(readyFile, nil) {
		t.Fatal("expected brand new file with recent modtime to defer upload for settling")
	}

	// 4. File with older modTime should be ready
	pastTime := time.Now().Add(-5 * time.Second)
	_ = os.Chtimes(readyFile, pastTime, pastTime)
	info, _ := os.Stat(readyFile)
	if !isFileReadyForUpload(readyFile, info) {
		t.Fatal("expected settled file to be ready for upload")
	}
}

func TestAutoBackupFailureBackoff(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "broken.jpg")
	if err := os.WriteFile(testFile, []byte("broken content"), 0o644); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(testFile)
	if err != nil {
		t.Fatal(err)
	}

	abManager := NewAutoBackupManager(nil, slog.Default(), nil)

	// Initially not failing
	if abManager.shouldSkipFailure(testFile, fi.Size(), fi.ModTime().Unix()) {
		t.Fatal("expected new file to not be skipped")
	}

	// Record a failure
	abManager.recordFailure(testFile, errors.New("simulated upload error"))
	abs, _ := filepath.Abs(testFile)
	abManager.mu.Lock()
	rec := abManager.failures[abs]
	abManager.mu.Unlock()

	if rec == nil || rec.consecutiveFailures != 1 {
		t.Fatalf("expected 1 failure recorded, got %v", rec)
	}

	// Now shouldSkipFailure should be true (in backoff)
	if !abManager.shouldSkipFailure(testFile, fi.Size(), fi.ModTime().Unix()) {
		t.Fatal("expected failed file to be skipped during backoff window")
	}

	// If file was edited (different modTime or size), backoff should reset
	if abManager.shouldSkipFailure(testFile, fi.Size()+50, fi.ModTime().Unix()) {
		t.Fatal("expected modified file size to reset failure backoff")
	}

	// Clear failure
	abManager.clearFailure(testFile)
	if abManager.shouldSkipFailure(testFile, fi.Size(), fi.ModTime().Unix()) {
		t.Fatal("expected cleared failure to not be skipped")
	}
}

func TestAutoBackupMaxFailureQuarantine(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "looping_video.mp4")
	if err := os.WriteFile(testFile, []byte("fake video content"), 0o644); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(testFile)
	if err != nil {
		t.Fatal(err)
	}

	abManager := NewAutoBackupManager(nil, slog.Default(), nil)

	// Simulate 10 consecutive failures
	for i := 0; i < 10; i++ {
		abManager.recordFailure(testFile, errors.New("scotty stream error"))
	}

	abs, _ := filepath.Abs(testFile)
	abManager.mu.Lock()
	rec := abManager.failures[abs]
	// Force nextRetry to be in the past so backoff window is elapsed
	rec.nextRetry = time.Now().Add(-1 * time.Hour)
	abManager.mu.Unlock()

	// Should still be skipped because max consecutive failures (quarantine) is reached
	if !abManager.shouldSkipFailure(testFile, fi.Size(), fi.ModTime().Unix()) {
		t.Fatal("expected file with >=10 failures to be quarantined even after backoff elapsed")
	}

	// But if file size changes, quarantine must reset
	if abManager.shouldSkipFailure(testFile, fi.Size()+100, fi.ModTime().Unix()) {
		t.Fatal("expected modified file size to break quarantine")
	}
}


