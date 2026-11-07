package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSortPathsBySizeAscending(t *testing.T) {
	tempDir := t.TempDir()

	// Create files of varying sizes
	// large: 10000 bytes
	// small: 50 bytes
	// medium: 500 bytes
	// tiny1: 10 bytes (path "a_tiny.jpg")
	// tiny2: 10 bytes (path "b_tiny.jpg")
	largePath := filepath.Join(tempDir, "large.jpg")
	smallPath := filepath.Join(tempDir, "small.jpg")
	mediumPath := filepath.Join(tempDir, "medium.jpg")
	tiny1Path := filepath.Join(tempDir, "a_tiny.jpg")
	tiny2Path := filepath.Join(tempDir, "b_tiny.jpg")

	if err := os.WriteFile(largePath, make([]byte, 10000), 0644); err != nil {
		t.Fatalf("failed to create large file: %v", err)
	}
	if err := os.WriteFile(smallPath, make([]byte, 50), 0644); err != nil {
		t.Fatalf("failed to create small file: %v", err)
	}
	if err := os.WriteFile(mediumPath, make([]byte, 500), 0644); err != nil {
		t.Fatalf("failed to create medium file: %v", err)
	}
	if err := os.WriteFile(tiny1Path, make([]byte, 10), 0644); err != nil {
		t.Fatalf("failed to create tiny1 file: %v", err)
	}
	if err := os.WriteFile(tiny2Path, make([]byte, 10), 0644); err != nil {
		t.Fatalf("failed to create tiny2 file: %v", err)
	}

	// Intentionally scrambled order
	paths := []string{largePath, mediumPath, tiny2Path, smallPath, tiny1Path}

	sortPathsBySizeAscending(paths)

	expectedOrder := []string{tiny1Path, tiny2Path, smallPath, mediumPath, largePath}
	for i, p := range paths {
		if p != expectedOrder[i] {
			t.Errorf("at index %d: expected %s, got %s", i, filepath.Base(expectedOrder[i]), filepath.Base(p))
		}
	}
}

func TestSortUploadWorkItemsBySizeAscending(t *testing.T) {
	tempDir := t.TempDir()

	// Item 1: Small photo (100 bytes)
	p1 := filepath.Join(tempDir, "small.jpg")
	_ = os.WriteFile(p1, make([]byte, 100), 0644)
	item1 := UploadWorkItem{
		Kind:   UploadWorkSingle,
		Single: &SingleMedia{Path: p1},
	}

	// Item 2: Large video (50000 bytes)
	p2 := filepath.Join(tempDir, "video.mp4")
	_ = os.WriteFile(p2, make([]byte, 50000), 0644)
	item2 := UploadWorkItem{
		Kind:   UploadWorkSingle,
		Single: &SingleMedia{Path: p2},
	}

	// Item 3: Live Photo pair: photo (500 bytes) + video (1500 bytes) = 2000 bytes total
	lpPhoto := filepath.Join(tempDir, "live.heic")
	lpVideo := filepath.Join(tempDir, "live.mov")
	_ = os.WriteFile(lpPhoto, make([]byte, 500), 0644)
	_ = os.WriteFile(lpVideo, make([]byte, 1500), 0644)
	item3 := UploadWorkItem{
		Kind: UploadWorkLivePhoto,
		LivePhoto: &LivePhotoPair{
			ContentIdentifier: "test-id",
			PhotoPath:         lpPhoto,
			VideoPath:         lpVideo,
		},
	}

	items := []UploadWorkItem{item2, item3, item1}

	totalBytes := sortUploadWorkItemsBySizeAscending(items)

	// Expected total bytes: 100 + 50000 + (500 + 1500) = 52100
	if totalBytes != 52100 {
		t.Fatalf("expected totalBytes 52100, got %d", totalBytes)
	}

	// Expected sorted order by size ascending: item1 (100b), item3 (2000b), item2 (50000b)
	if items[0].Single == nil || items[0].Single.Path != p1 {
		t.Errorf("expected first item to be small.jpg, got %+v", items[0])
	}
	if items[1].LivePhoto == nil || items[1].LivePhoto.PhotoPath != lpPhoto {
		t.Errorf("expected second item to be live photo, got %+v", items[1])
	}
	if items[2].Single == nil || items[2].Single.Path != p2 {
		t.Errorf("expected third item to be video.mp4, got %+v", items[2])
	}
}

func TestFilterGooglePhotosFiles_SortsBySizeAscending(t *testing.T) {
	tempDir := t.TempDir()

	hugeVideo := filepath.Join(tempDir, "huge_video.mp4")
	mediumClip := filepath.Join(tempDir, "medium_clip.mp4")
	smallPhoto := filepath.Join(tempDir, "small_photo.jpg")

	_ = os.WriteFile(hugeVideo, make([]byte, 1000000), 0644)
	_ = os.WriteFile(mediumClip, make([]byte, 20000), 0644)
	_ = os.WriteFile(smallPhoto, make([]byte, 500), 0644)

	opts := UploadOptions{
		Recursive: true,
	}

	files, err := FilterGooglePhotosFiles([]string{tempDir}, opts)
	if err != nil {
		t.Fatalf("FilterGooglePhotosFiles failed: %v", err)
	}

	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	// Verify order: smallPhoto (500), mediumClip (20000), hugeVideo (1000000)
	if filepath.Base(files[0]) != "small_photo.jpg" {
		t.Errorf("expected first file to be small_photo.jpg, got %s", filepath.Base(files[0]))
	}
	if filepath.Base(files[1]) != "medium_clip.mp4" {
		t.Errorf("expected second file to be medium_clip.mp4, got %s", filepath.Base(files[1]))
	}
	if filepath.Base(files[2]) != "huge_video.mp4" {
		t.Errorf("expected third file to be huge_video.mp4, got %s", filepath.Base(files[2]))
	}
}

