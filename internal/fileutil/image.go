package fileutil

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ImageInfo struct {
	PathInfo

	Width    int      `json:"width,omitempty"`
	Height   int      `json:"height,omitempty"`
	Format   string   `json:"format,omitempty"`   // e.g. "jpeg", "png"
	MIMEType MIMEType `json:"mimeType,omitempty"` // e.g. "image/jpeg"
}

// ImageData holds metadata (and optionally content) for an image file.
type ImageData struct {
	ImageInfo

	Base64Data string `json:"base64Data,omitempty"` // optional, if requested
}

// ReadImage inspects an image file and returns its intrinsic metadata.
// If includeBase64 is true, Base64Data will contain the base64-encoded file
// contents. If the file does not exist, Exists == false and err == nil.
// Returns an error if the path is empty, a directory, or not a supported image.
func ReadImage(
	path string,
	includeBase64Data bool,
	maxBytes int64,
) (*ImageData, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrInvalidPath
	}

	out := &ImageData{}
	p, err := NormalizePath(path)
	if err != nil {
		return nil, err
	}
	out.Path = p

	// Refuse symlink traversal in parent directories (best-effort hardening).
	// Preserve current semantics: if parent doesn't exist, treat as "file doesn't exist".
	parent := filepath.Dir(p)
	if parent != "" && parent != "." {
		if err := VerifyDirNoSymlink(parent); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				out.Exists = false
				return out, nil
			}
			return nil, err
		}
	}

	st, err := os.Lstat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			out.Exists = false
			return out, nil
		}
		return nil, err
	}
	out.Exists = true
	out.Name = st.Name()
	out.IsDir = st.IsDir()
	out.Size = st.Size()
	mt := st.ModTime().UTC()
	out.ModTime = &mt

	if (st.Mode() & os.ModeSymlink) != 0 {
		return nil, fmt.Errorf("refusing to operate on symlink file: %s", p)
	}

	if out.IsDir {
		return nil, errors.New("path points to a directory, expected file")
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("expected regular file: %s", p)
	}

	// We need to decode the image config; if includeBase64 is true, we can
	// read the whole file once and reuse that data for both config and base64.
	if includeBase64Data {
		if maxBytes > 0 && out.Size > maxBytes {
			return nil, fmt.Errorf(
				"file %q exceeds maximum allowed size (%d bytes): %w",
				out.Path,
				maxBytes,
				ErrFileExceedsMaxSize,
			)
		}

		f, err := os.Open(out.Path)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		r := io.Reader(f)
		if maxBytes > 0 {
			r = io.LimitReader(f, maxBytes+1)
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		if maxBytes > 0 && int64(len(data)) > maxBytes {
			return nil, fmt.Errorf(
				"file %q exceeds maximum allowed size (%d bytes): %w",
				out.Path,
				maxBytes,
				ErrFileExceedsMaxSize,
			)
		}

		reader := bytes.NewReader(data)
		err = decodeImageConfig(out, reader)
		if err != nil {
			return nil, err
		}
		out.Base64Data = base64.StdEncoding.EncodeToString(data)
		return out, nil
	}

	// No base64 requested: just open and decode config.
	f, err := os.Open(out.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := io.Reader(f)
	if maxBytes > 0 {
		// Config decode should only need headers, but keep it bounded anyway.
		r = io.LimitReader(f, maxBytes)
	}
	err = decodeImageConfig(out, r)
	if err != nil {
		return nil, err
	}

	return out, nil
}

func decodeImageConfig(info *ImageData, reader io.Reader) error {
	cfg, fmtName, err := image.DecodeConfig(reader)
	if err != nil {
		return err
	}

	info.Width = cfg.Width
	info.Height = cfg.Height
	info.Format = fmtName
	m, err := MIMEFromExtensionString(fmtName)
	if err != nil {
		return fmt.Errorf("unsupported image format %q: %w", fmtName, err)
	}
	if GetModeForMIME(m) != ExtensionModeImage {
		return fmt.Errorf("unsupported image MIME type %q", m)
	}
	info.MIMEType = m
	return nil
}
