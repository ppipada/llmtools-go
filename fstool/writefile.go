package fstool

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/flexigpt/llmtools-go/internal/fileutil"
	"github.com/flexigpt/llmtools-go/internal/toolutil"
	"github.com/flexigpt/llmtools-go/spec"
)

const writeFileFuncID spec.FuncID = "github.com/flexigpt/llmtools-go/fstool/writefile.WriteFile"

var writeFileTool = spec.Tool{
	SchemaVersion: spec.SchemaVersion,
	ID:            "019c04cf-72ec-7eed-ab2a-45e6fb9e1a86",
	Slug:          "writefile",
	Version:       "v1.0.0",
	DisplayName:   "Write file",
	Description:   "Write a file to disk. encoding=text writes UTF-8; binary expects base64 string as input and writes raw bytes.",
	Tags:          []string{"fs"},

	ArgSchema: spec.JSONSchema(`{
"$schema": "http://json-schema.org/draft-07/schema#",
"type": "object",
"properties": {
	"path": {
		"type": "string",
		"description": "Absolute path of the file to write."
	},
	"encoding": {
		"type": "string",
		"enum": ["text", "binary"],
		"description": "Write mode.",
		"default": "text"
	},
	"content": {
		"type": "string",
		"description": "If encoding=text, UTF-8 content. If encoding=binary, base64-encoded bytes."
	},
	"overwrite": {
		"type": "boolean",
		"description": "If false and the file exists, return an error.",
		"default": false
	},
	"createParents": {
		"type": "boolean",
		"description": "If true, create missing parent directories. Max new directories created is 8.",
		"default": false
	}
},
"required": ["path", "content"],
"additionalProperties": false
}`),

	GoImpl: spec.GoToolImpl{FuncID: writeFileFuncID},

	CreatedAt:  spec.SchemaStartTime,
	ModifiedAt: spec.SchemaStartTime,
}

func WriteFileTool() spec.Tool {
	return toolutil.CloneTool(writeFileTool)
}

type WriteFileArgs struct {
	Path          string `json:"path"`
	Encoding      string `json:"encoding,omitempty"` // "text"(default) | "binary"
	Content       string `json:"content"`
	Overwrite     bool   `json:"overwrite,omitempty"`
	CreateParents bool   `json:"createParents,omitempty"`
}

type WriteFileOut struct {
	Path         string `json:"path"`
	BytesWritten int64  `json:"bytesWritten"`
}

// Max raw bytes written to disk (text bytes or decoded binary bytes).
// This is a safety/abuse guard similar to ReadFile's cap.
const maxWriteBytes int64 = 16 * 1024 * 1024 // 16MB

func WriteFile(ctx context.Context, args WriteFileArgs) (*WriteFileOut, error) {
	return toolutil.WithRecoveryResp(func() (*WriteFileOut, error) {
		return writeFile(ctx, args)
	})
}

func writeFile(ctx context.Context, args WriteFileArgs) (*WriteFileOut, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p, err := fileutil.NormalizePath(strings.TrimSpace(args.Path))
	if err != nil {
		return nil, err
	}
	// Per schema: require absolute path.
	if !filepath.IsAbs(p) {
		return nil, errors.New("path must be absolute")
	}

	enc := fileutil.ReadEncoding(strings.ToLower(strings.TrimSpace(args.Encoding)))

	if enc == "" {
		enc = fileutil.ReadEncodingText
	}
	if enc != fileutil.ReadEncodingText && enc != fileutil.ReadEncodingBinary {
		return nil, errors.New(`encoding must be "text" or "binary"`)
	}

	// Decode/validate content.
	var data []byte
	switch enc {
	case fileutil.ReadEncodingText:
		// Content is required by schema, but empty string is a valid payload.
		if !utf8.ValidString(args.Content) {
			return nil, errors.New("content is not valid UTF-8")
		}
		data = []byte(args.Content)
	case fileutil.ReadEncodingBinary:
		b64 := strings.TrimSpace(args.Content)
		// Pre-check decoded size to avoid huge allocations.
		if int64(base64.StdEncoding.DecodedLen(len(b64))) > maxWriteBytes {
			return nil, fmt.Errorf("content too large (decoded > %d bytes)", maxWriteBytes)
		}
		decoded, derr := base64.StdEncoding.DecodeString(b64)
		if derr != nil {
			return nil, fmt.Errorf("invalid base64 content: %w", derr)
		}
		data = decoded
	}

	if int64(len(data)) > maxWriteBytes {
		return nil, fmt.Errorf("content too large (%d bytes; max %d)", len(data), maxWriteBytes)
	}

	parent := filepath.Dir(p)
	if parent == "" || parent == "." {
		// With absolute paths this should not happen, but keep it defensive.
		return nil, fileutil.ErrInvalidPath
	}

	if args.CreateParents {
		_, err := fileutil.EnsureDirNoSymlink(parent, 8 /*max new dirs*/)
		if err != nil {
			return nil, err
		}

	} else {
		if err := fileutil.VerifyDirNoSymlink(parent); err != nil {
			return nil, err
		}
	}

	// Validate existing destination if present.
	if st, err := os.Lstat(p); err == nil {
		if st.IsDir() {
			return nil, fmt.Errorf("path is a directory, not a file: %s", p)
		}
		// Refuse special files (device nodes, pipes, sockets, etc.)
		if !st.Mode().IsRegular() && (st.Mode()&os.ModeSymlink) == 0 {
			return nil, fmt.Errorf("refusing to write to non-regular file: %s", p)
		}
		if !args.Overwrite {
			return nil, fmt.Errorf("file already exists and overwrite=false: %s", p)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// Atomic write: write temp file in same directory, then commit.
	tmp, err := os.CreateTemp(parent, ".writefile-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()

	cleanupTmp := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	// Best-effort; may be unsupported on some filesystems/platforms.
	_ = tmp.Chmod(0o600)

	if err := ctx.Err(); err != nil {
		cleanupTmp()
		return nil, err
	}

	n, err := tmp.Write(data)
	if err != nil {
		cleanupTmp()
		return nil, err
	}
	if n != len(data) {
		cleanupTmp()
		return nil, ioErrShortWrite(n, len(data))
	}

	// Best-effort durability.
	if err := tmp.Sync(); err != nil {
		cleanupTmp()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanupTmp()
		return nil, err
	}

	// Allow cancellation before the commit step (rename/link).
	if err := ctx.Err(); err != nil {
		cleanupTmp()
		return nil, err
	}

	// Commit temp file into place.
	if !args.Overwrite {
		// On unix, Rename would overwrite; Link is atomic and won't overwrite.
		// On windows, Rename fails if destination exists, so it's fine to use Rename.
		if runtime.GOOS != toolutil.GOOSWindows {
			if err := os.Link(tmpName, p); err != nil {
				// If it exists (race), treat as "overwrite=false" violation.
				if errors.Is(err, os.ErrExist) {
					cleanupTmp()
					return nil, fmt.Errorf("file already exists and overwrite=false: %s", p)
				}
				// Some FS may not support hardlinks. Reserve the destination name
				// with O_EXCL, then rename over the placeholder (prevents clobbering).
				ph, perr := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
				if perr != nil {
					cleanupTmp()
					if errors.Is(perr, os.ErrExist) {
						return nil, fmt.Errorf("file already exists and overwrite=false: %s", p)
					}
					return nil, perr
				}
				_ = ph.Close()
				if err2 := os.Rename(tmpName, p); err2 != nil {
					// If rename fails, remove placeholder and temp.
					_ = os.Remove(p)
					cleanupTmp()
					return nil, err2
				}
			} else {
				// Link succeeded: remove temp link-name, destination remains.
				_ = os.Remove(tmpName)
			}
		} else {
			if err := os.Rename(tmpName, p); err != nil {
				cleanupTmp()
				return nil, err
			}
		}
	} else {
		if runtime.GOOS == toolutil.GOOSWindows {
			// Windows won't rename over an existing destination. Handle races by:
			// rename -> if dest exists now, remove -> retry rename once.
			if err := os.Rename(tmpName, p); err != nil {
				if st, stErr := os.Lstat(p); stErr == nil {
					if st.IsDir() {
						cleanupTmp()
						return nil, fmt.Errorf("path is a directory, not a file: %s", p)
					}
					if rmErr := os.Remove(p); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
						cleanupTmp()
						return nil, rmErr
					}
					if err2 := os.Rename(tmpName, p); err2 != nil {
						cleanupTmp()
						return nil, err2
					}
				} else {
					cleanupTmp()
					return nil, err
				}
			}
		} else {
			if err := os.Rename(tmpName, p); err != nil {
				cleanupTmp()
				return nil, err
			}
		}
	}

	// Ensure final mode is 0600 (best-effort; Windows ignores).
	_ = os.Chmod(p, 0o600)

	return &WriteFileOut{
		Path:         p,
		BytesWritten: int64(len(data)),
	}, nil
}

func ioErrShortWrite(got, want int) error {
	return fmt.Errorf("short write: wrote %d bytes, expected %d", got, want)
}
