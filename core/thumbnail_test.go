package core

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateThumbnail(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Create a valid test PNG image
	imgPath := filepath.Join(tempDir, "test.png")
	img := image.NewRGBA(image.Rect(0, 0, 100, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	// Generate thumbnail
	thumb := GenerateThumbnail(imgPath, 40)
	if thumb == "" {
		t.Fatal("expected non-empty thumbnail for valid PNG")
	}
	if !strings.HasPrefix(thumb, "data:image/jpeg;base64,") {
		t.Fatalf("expected data URL prefix, got %s", thumb[:min(30, len(thumb))])
	}

	// 2. Unsupported file extension
	txtPath := filepath.Join(tempDir, "test.txt")
	_ = os.WriteFile(txtPath, []byte("not an image"), 0o644)
	if thumbTxt := GenerateThumbnail(txtPath, 40); thumbTxt != "" {
		t.Fatalf("expected empty thumbnail for text file, got %s", thumbTxt)
	}

	// 3. Non-existent file
	if thumbMissing := GenerateThumbnail(filepath.Join(tempDir, "missing.png"), 40); thumbMissing != "" {
		t.Fatalf("expected empty thumbnail for missing file, got %s", thumbMissing)
	}
}

