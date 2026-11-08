package core

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var supportedThumbnailExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
}

// GenerateThumbnail produces a data URL (data:image/jpeg;base64,...) for image files resized
// to fit within maxDim x maxDim pixels. Returns empty string if unsupported or on error.
func GenerateThumbnail(filePath string, maxDim int) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	if !supportedThumbnailExts[ext] {
		return ""
	}

	f, err := os.Open(filePath)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	// Quick config check to avoid loading huge images into memory
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 16000 || cfg.Height > 16000 {
		return ""
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ""
	}

	img, _, err := image.Decode(f)
	if err != nil {
		return ""
	}

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return ""
	}

	if maxDim <= 0 {
		maxDim = 40
	}

	var targetW, targetH int
	if w > h {
		targetW = maxDim
		targetH = max(1, (h*maxDim)/w)
	} else {
		targetH = maxDim
		targetW = max(1, (w*maxDim)/h)
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	for y := 0; y < targetH; y++ {
		srcY := bounds.Min.Y + (y * h / targetH)
		for x := 0; x < targetW; x++ {
			srcX := bounds.Min.X + (x * w / targetW)
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 65}); err != nil {
		return ""
	}

	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

