package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDatabaseInitAndMigrations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("OpenDatabase failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Verify schema_migrations table
	var version int
	err = db.QueryRow("SELECT MAX(version) FROM schema_migrations;").Scan(&version)
	if err != nil {
		t.Fatalf("failed to query schema_migrations: %v", err)
	}
	if version != 2 {
		t.Fatalf("expected migration version 2, got %d", version)
	}

	// Verify re-opening database succeeds idempotently
	db2, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("reopening database failed: %v", err)
	}
	_ = db2.Close()
}

func TestAutoBackupHistorySQLite(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "history.db")

	h := NewAutoBackupHistory(dbPath)
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

	// Check count
	if h.Count() != 1 {
		t.Fatalf("expected count 1, got %d", h.Count())
	}

	// Test re-open persistence
	_ = h.Close()
	h2 := NewAutoBackupHistory(dbPath)
	defer func() { _ = h2.Close() }()
	if !h2.IsUploaded(testFile, fi.Size(), fi.ModTime().Unix()) {
		t.Fatal("expected file to be uploaded in re-opened history")
	}
	if h2.Count() != 1 {
		t.Fatalf("expected count 1, got %d", h2.Count())
	}

	// Test case-insensitive lookup on Windows
	lowerPath := strings.ToLower(testFile)
	if !h2.IsUploaded(lowerPath, fi.Size(), fi.ModTime().Unix()) {
		t.Fatal("expected lowercase path lookup to succeed")
	}

	// Invalidate removes record
	h2.Invalidate(testFile)
	if h2.IsUploaded(testFile, fi.Size(), fi.ModTime().Unix()) {
		t.Fatal("expected file not uploaded after Invalidate")
	}
	if h2.Count() != 0 {
		t.Fatalf("expected count 0 after Invalidate, got %d", h2.Count())
	}
}

func TestAutoBackupHistoryConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "concurrent.db")
	h := NewAutoBackupHistory(dbPath)
	defer func() { _ = h.Close() }()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			path := filepath.Join(tempDir, fmt.Sprintf("file_%d.jpg", idx))
			h.MarkUploaded(path, int64(100+idx), int64(1000+idx))
			if !h.IsUploaded(path, int64(100+idx), int64(1000+idx)) {
				t.Errorf("file_%d should be uploaded", idx)
			}
		}(i)
	}
	wg.Wait()

	if h.Count() != 20 {
		t.Fatalf("expected count 20, got %d", h.Count())
	}
}

func TestMigrateJSONHistory(t *testing.T) {
	tempDir := t.TempDir()
	jsonPath := filepath.Join(tempDir, "autobackup_history.json")
	dbPath := filepath.Join(tempDir, "migrated.db")

	// 1. Create a synthetic legacy JSON file
	legacyRecords := map[string]BackupFileRecord{
		filepath.Join(tempDir, "photo1.jpg"): {Size: 1024, ModTime: 1700000001},
		filepath.Join(tempDir, "photo2.jpg"): {Size: 2048, ModTime: 1700000002},
	}
	jsonData, err := json.Marshal(legacyRecords)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonPath, jsonData, 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := OpenDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Run migration
	if err := MigrateJSONHistory(db, jsonPath); err != nil {
		t.Fatalf("MigrateJSONHistory failed: %v", err)
	}

	// Verify records exist in SQLite
	h := NewAutoBackupHistoryWithDB(db)
	if h.Count() != 2 {
		t.Fatalf("expected 2 migrated records, got %d", h.Count())
	}
	if !h.IsUploaded(filepath.Join(tempDir, "photo1.jpg"), 1024, 1700000001) {
		t.Fatal("expected photo1 to be uploaded in SQLite")
	}
	if !h.IsUploaded(filepath.Join(tempDir, "photo2.jpg"), 2048, 1700000002) {
		t.Fatal("expected photo2 to be uploaded in SQLite")
	}

	// Verify legacy JSON was archived as .bak
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatal("expected original jsonPath to be renamed")
	}
	if _, err := os.Stat(jsonPath + ".bak"); err != nil {
		t.Fatalf("expected .bak file to exist: %v", err)
	}

	// Running migration again with .bak should be idempotent and not fail
	if err := MigrateJSONHistory(db, jsonPath); err != nil {
		t.Fatalf("idempotent migration check failed: %v", err)
	}
	if h.Count() != 2 {
		t.Fatalf("expected 2 records after second migration, got %d", h.Count())
	}
}

func TestMigrateMalformedJSONPreservation(t *testing.T) {
	tempDir := t.TempDir()
	jsonPath := filepath.Join(tempDir, "corrupted.json")
	if err := os.WriteFile(jsonPath, []byte("{invalid json data"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := OpenDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	err = MigrateJSONHistory(db, jsonPath)
	if err == nil {
		t.Fatal("expected error for malformed json")
	}

	// Corrupted file must be preserved and NOT deleted or renamed
	if _, statErr := os.Stat(jsonPath); statErr != nil {
		t.Fatal("expected corrupted json file to be preserved untouched")
	}
}

func TestMigrateEmptyAndMissingJSON(t *testing.T) {
	tempDir := t.TempDir()
	db, err := OpenDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// 1. Missing JSON file
	missingPath := filepath.Join(tempDir, "non_existent.json")
	if err := MigrateJSONHistory(db, missingPath); err != nil {
		t.Fatalf("missing json migration failed: %v", err)
	}

	// 2. Empty JSON file (0 bytes)
	emptyFilePath := filepath.Join(tempDir, "empty.json")
	if err := os.WriteFile(emptyFilePath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateJSONHistory(db, emptyFilePath); err != nil {
		t.Fatalf("empty file migration failed: %v", err)
	}
	if _, err := os.Stat(emptyFilePath + ".bak"); err != nil {
		t.Fatalf("expected empty file to be renamed to .bak: %v", err)
	}

	// 3. Empty JSON object ("{}")
	emptyObjPath := filepath.Join(tempDir, "empty_obj.json")
	if err := os.WriteFile(emptyObjPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := MigrateJSONHistory(db, emptyObjPath); err != nil {
		t.Fatalf("empty object migration failed: %v", err)
	}
	if _, err := os.Stat(emptyObjPath + ".bak"); err != nil {
		t.Fatalf("expected empty object to be renamed to .bak: %v", err)
	}
}

func TestMigrateDuplicateNormalizedPaths(t *testing.T) {
	tempDir := t.TempDir()
	db, err := OpenDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	jsonPath := filepath.Join(tempDir, "dups.json")
	p1 := filepath.Join(tempDir, "TestPic.jpg")
	p2 := strings.ToLower(p1)

	// Construct JSON with distinct keys that normalize to the same path
	content := fmt.Sprintf(`{
		%q: {"size": 500, "modTime": 100},
		%q: {"size": 600, "modTime": 200}
	}`, p1, p2)

	if err := os.WriteFile(jsonPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateJSONHistory(db, jsonPath); err != nil {
		t.Fatalf("migration with duplicate normalized paths failed: %v", err)
	}

	h := NewAutoBackupHistoryWithDB(db)
	// Should contain 1 record without crashing
	if h.Count() != 1 {
		t.Fatalf("expected 1 record after duplicate resolution, got %d", h.Count())
	}
}

func TestMigratePreservesExistingSQLiteRecord(t *testing.T) {
	tempDir := t.TempDir()
	db, err := OpenDatabase(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	h := NewAutoBackupHistoryWithDB(db)
	testPath := filepath.Join(tempDir, "existing.jpg")
	// Pre-populate SQLite with valid record
	h.MarkUploaded(testPath, 9999, 2000)

	// Legacy JSON with older/different size for the same file
	jsonPath := filepath.Join(tempDir, "stale.json")
	staleRecords := map[string]BackupFileRecord{
		testPath: {Size: 1111, ModTime: 1000},
	}
	jsonData, _ := json.Marshal(staleRecords)
	_ = os.WriteFile(jsonPath, jsonData, 0o644)

	if err := MigrateJSONHistory(db, jsonPath); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Existing SQLite record must remain untouched
	if !h.IsUploaded(testPath, 9999, 2000) {
		t.Fatal("expected original SQLite record to be preserved")
	}
	if h.IsUploaded(testPath, 1111, 1000) {
		t.Fatal("stale JSON record should NOT have overwritten existing SQLite record")
	}
}

