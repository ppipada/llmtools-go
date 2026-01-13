package image

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectImage(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "img.png")

	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := range 6 {
		for x := range 8 {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}

	f, err := os.Create(imgPath)
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatalf("encode png: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close image file: %v", err)
	}

	out, err := InspectImage(context.Background(), InspectImageArgs{Path: imgPath})
	if err != nil {
		t.Fatalf("InspectImage error: %v", err)
	}
	if !out.Exists {
		t.Fatalf("expected image to exist")
	}
	if out.Width != 8 || out.Height != 6 {
		t.Fatalf("unexpected dimensions: %+v", out)
	}
	if out.Format != "png" {
		t.Fatalf("expected png format, got %q", out.Format)
	}
	if out.SizeBytes <= 0 {
		t.Fatalf("expected positive SizeBytes, got %d", out.SizeBytes)
	}
	if out.ModTime == nil {
		t.Fatalf("expected ModTime to be set")
	}

	textPath := filepath.Join(tmpDir, "notimg.txt")
	if err := os.WriteFile(textPath, []byte("plain"), 0o600); err != nil {
		t.Fatalf("write text: %v", err)
	}
	if _, err := InspectImage(context.Background(), InspectImageArgs{Path: textPath}); err == nil {
		t.Fatalf("expected error for non-image file")
	}
	if _, err := InspectImage(context.Background(), InspectImageArgs{Path: tmpDir}); err == nil {
		t.Fatalf("expected error for directory path")
	}
	if _, err := InspectImage(context.Background(), InspectImageArgs{}); err == nil {
		t.Fatalf("expected error for empty path")
	}
}
