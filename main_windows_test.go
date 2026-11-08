//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetWebviewUserDataPath(t *testing.T) {
	path := getWebviewUserDataPath()
	if path == "" {
		t.Fatal("expected non-empty webview user data path")
	}

	// Verify the path exists and is a directory
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("path does not exist: %v", err)
	}
	if !fi.IsDir() {
		t.Fatalf("path is not a directory: %s", path)
	}

	// Verify we can write to it
	testFile := filepath.Join(path, ".test_write_perm")
	if err := os.WriteFile(testFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("cannot write into webview user data path: %v", err)
	}
	_ = os.Remove(testFile)
}

