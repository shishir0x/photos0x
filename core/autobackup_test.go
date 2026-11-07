package core

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutoBackupHistory(t *testing.T) {
	h := NewAutoBackupHistory(":memory:")
	defer func() { _ = h.Close() }()
	if h.Count() != 0 {
		t.Fatalf("expected count 0, got %d", h.Count())
	}

	tempDir := t.TempDir()
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
	_ = h.MarkUploaded(testFile, fi.Size(), fi.ModTime().Unix())
	if !h.IsUploaded(testFile, fi.Size(), fi.ModTime().Unix()) {
		t.Fatal("expected file to be uploaded after MarkUploaded")
	}

	// Different size/mtime reports false
	if h.IsUploaded(testFile, fi.Size()+10, fi.ModTime().Unix()) {
		t.Fatal("expected false for changed size")
	}

	if h.Count() != 1 {
		t.Fatalf("expected count 1, got %d", h.Count())
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

	// Create AutoBackupManager
	abManager := NewAutoBackupManager(nil, slog.Default(), nil)
	if abManager.IsRunning() {
		t.Fatal("expected manager not running initially")
	}

	// Start without enabled should start periodic scanner
	if err := abManager.Start(); err != nil {
		t.Fatal(err)
	}
	if !abManager.IsRunning() {
		t.Fatal("expected manager to be running after Start()")
	}

	// Calling Start() again is idempotent
	if err := abManager.Start(); err != nil {
		t.Fatal(err)
	}

	abManager.Stop()
	if abManager.IsRunning() {
		t.Fatal("expected manager to not be running after Stop()")
	}

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
	defer func() {
		_ = CloseAppDatabase()
		ConfigPath = ""
	}()

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

func TestAutoBackupScanFolders(t *testing.T) {
	tempDir := t.TempDir()
	folder := filepath.Join(tempDir, "scan_test")
	if err := os.MkdirAll(folder, 0o755); err != nil {
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

	f1 := filepath.Join(folder, "photo1.jpg")
	f2 := filepath.Join(folder, "photo2.jpg")
	_ = os.WriteFile(f1, []byte("content1"), 0o644)
	_ = os.WriteFile(f2, []byte("content2"), 0o644)

	abManager := NewAutoBackupManager(nil, slog.Default(), nil)
	// scanFolders with nil uploadManager must not panic
	abManager.running = true
	abManager.scanFolders([]string{folder})

	// Mark f1 as uploaded
	info1, _ := os.Stat(f1)
	_ = abManager.history.MarkUploaded(f1, info1.Size(), info1.ModTime().Unix())

	if !abManager.history.IsUploaded(f1, info1.Size(), info1.ModTime().Unix()) {
		t.Fatal("expected f1 to be marked as uploaded")
	}

	info2, _ := os.Stat(f2)
	if abManager.history.IsUploaded(f2, info2.Size(), info2.ModTime().Unix()) {
		t.Fatal("expected f2 to not be uploaded yet")
	}
}

func TestAutoBackupHistoryInvalidation(t *testing.T) {
	h := NewAutoBackupHistory(":memory:")
	defer func() { _ = h.Close() }()
	testPath := "C:/photos/photo.jpg"

	_ = h.MarkUploaded(testPath, 1024, 1700000000)
	if !h.IsUploaded(testPath, 1024, 1700000000) {
		t.Fatal("expected file to be marked as uploaded")
	}

	// Invalidate removes entry
	_ = h.Invalidate(testPath)
	if h.IsUploaded(testPath, 1024, 1700000000) {
		t.Fatal("expected file to not be uploaded after Invalidate")
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
