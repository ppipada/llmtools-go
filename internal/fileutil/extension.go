package fileutil

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrInvalidPath      = errors.New("invalid path")
	ErrUnknownExtension = errors.New("unknown extension")
)

type ExtensionMode string

const (
	ExtensionModeText     ExtensionMode = "text"
	ExtensionModeImage    ExtensionMode = "image"
	ExtensionModeDocument ExtensionMode = "document"
	ExtensionModeDefault  ExtensionMode = "default"
)

type FileExt string

const (
	ExtPDF FileExt = ".pdf"
)

type MIMEType string

const (
	MIMEEmpty                  MIMEType = ""
	MIMEApplicationOctetStream MIMEType = "application/octet-stream"

	MIMETextPlain MIMEType = "text/plain; charset=utf-8"

	MIMEApplicationJSON MIMEType = "application/json"
	MIMEApplicationXML  MIMEType = "application/xml"
	MIMEApplicationYAML MIMEType = "application/x-yaml"
	MIMEApplicationTOML MIMEType = "application/toml"
	MIMEApplicationSQL  MIMEType = "application/sql"
	MIMEApplicationJS   MIMEType = "application/javascript"

	MIMEImageJPEG MIMEType = "image/jpeg"
	MIMEImagePNG  MIMEType = "image/png"
	MIMEImageGIF  MIMEType = "image/gif"
	MIMEImageWEBP MIMEType = "image/webp"

	MIMEApplicationPDF MIMEType = "application/pdf"
)

// MIMEForLocalFile returns a best-effort MIME type and a coarse "mode" (text/image/document/default).
//
// Behavior is intentionally simple:
//   - First try extension-based detection (stdlib + minimal overrides).
//   - If extension detection is unknown/generic, sniff the file bytes.
//   - Sniffing uses DetectContentType + a small "isProbablyTextSample" heuristic.
func MIMEForLocalFile(path string) (mimeType MIMEType, mode ExtensionMode, err error) {
	if strings.TrimSpace(path) == "" {
		return MIMEEmpty, ExtensionModeDefault, ErrInvalidPath
	}

	ext := filepath.Ext(path)
	if ext != "" {
		mt, e := MIMEFromExtensionString(ext)
		if e == nil && mt != MIMEEmpty && baseMIME(mt) != string(MIMEApplicationOctetStream) {
			return mt, modeForMIME(mt), nil
		}
		// Unknown or generic => sniff.
	}

	return SniffFileMIME(path)
}

// MIMEFromExtensionString returns a best-known MIME for the given extension string.
// Accepts "png" as well as ".png" (useful because image.DecodeConfig returns "png").
//
// If the extension cannot be resolved, returns application/octet-stream and ErrUnknownExtension.
func MIMEFromExtensionString(ext string) (MIMEType, error) {
	if strings.TrimSpace(ext) == "" {
		return MIMEEmpty, ErrInvalidPath
	}
	e := normalizeExt(ext)

	// Minimal explicit mapping for types you actually use/validate.
	switch e {
	case ".pdf":
		return MIMEApplicationPDF, nil
	case ".png":
		return MIMEImagePNG, nil
	case ".jpg", ".jpeg":
		return MIMEImageJPEG, nil
	case ".gif":
		return MIMEImageGIF, nil
	case ".webp":
		return MIMEImageWEBP, nil
	case ".json":
		return MIMEApplicationJSON, nil
	case ".yaml", ".yml":
		return MIMEApplicationYAML, nil
	case ".toml":
		return MIMEApplicationTOML, nil
	case ".sql":
		return MIMEApplicationSQL, nil
	case ".js":
		return MIMEApplicationJS, nil
	case ".xml":
		return MIMEApplicationXML, nil
	}

	// Fall back to stdlib mapping.
	if t := mime.TypeByExtension(e); t != "" {
		return MIMEType(t), nil
	}

	return MIMEApplicationOctetStream, ErrUnknownExtension
}

// SniffFileMIME inspects initial bytes of a file and returns a best-effort
// MIME type and mode. It will return an error if the file can't be opened/read.
func SniffFileMIME(path string) (mimeType MIMEType, mode ExtensionMode, err error) {
	if strings.TrimSpace(path) == "" {
		return MIMEEmpty, ExtensionModeDefault, ErrInvalidPath
	}

	f, err := os.Open(path)
	if err != nil {
		return MIMEEmpty, ExtensionModeDefault, err
	}
	defer f.Close()

	buf := make([]byte, 4096)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return MIMEEmpty, ExtensionModeDefault, err
	}
	sample := buf[:n]
	if len(sample) == 0 {
		// Empty file: treat as text.
		return MIMETextPlain, ExtensionModeText, nil
	}

	mt := MIMEType(http.DetectContentType(sample))
	m := modeForMIME(mt)

	// If DetectContentType is generic, try to classify text via heuristic.
	if baseMIME(mt) == string(MIMEApplicationOctetStream) || mt == MIMEEmpty {
		if isProbablyTextSample(sample) {
			return MIMETextPlain, ExtensionModeText, nil
		}
		return MIMEApplicationOctetStream, ExtensionModeDefault, nil
	}

	// If DetectContentType says "text/plain" but the sample is clearly binary,
	// downgrade to default/octet-stream.
	if m == ExtensionModeText && !isProbablyTextSample(sample) {
		return MIMEApplicationOctetStream, ExtensionModeDefault, nil
	}

	return mt, m, nil
}

func modeForMIME(mt MIMEType) ExtensionMode {
	base := baseMIME(mt)

	switch {
	case strings.HasPrefix(base, "text/"):
		return ExtensionModeText
	case strings.HasPrefix(base, "image/"):
		return ExtensionModeImage
	}

	// Treat common "application/* but still text-like" as text.
	switch base {
	case "application/json",
		"application/xml",
		"application/x-yaml",
		"application/yaml",
		"application/toml",
		"application/sql",
		"application/javascript":
		return ExtensionModeText
	}

	// Structured suffixes like application/vnd.foo+json.
	if strings.HasSuffix(base, "+json") || strings.HasSuffix(base, "+xml") {
		return ExtensionModeText
	}

	if base == "application/pdf" {
		return ExtensionModeDocument
	}

	return ExtensionModeDefault
}

func baseMIME(mt MIMEType) string {
	s := strings.TrimSpace(strings.ToLower(string(mt)))
	if s == "" {
		return ""
	}
	// Drop parameters like "; charset=utf-8".
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// isProbablyTextSample returns true if the byte sample looks like text.
// Heuristic: disallow NULs and too many control bytes (except \t, \n, \r).
func isProbablyTextSample(p []byte) bool {
	if len(p) == 0 {
		return true
	}
	nulCount := 0
	controlCount := 0
	for _, b := range p {
		if b == 0 {
			nulCount++
			continue
		}
		if b < 32 && b != 9 && b != 10 && b != 13 {
			controlCount++
		}
	}
	if nulCount > 0 {
		return false
	}
	// If >10% bytes are control chars, assume binary.
	return controlCount*10 <= len(p)
}

// normalizeExt lowercases and ensures a leading '.' for an extension.
func normalizeExt(ext string) string {
	e := strings.TrimSpace(ext)
	if e == "" {
		return ""
	}
	if !strings.HasPrefix(e, ".") {
		e = "." + e
	}
	return strings.ToLower(e)
}
