package fstool

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

func TestReadFile(t *testing.T) {
	type cfg struct {
		workBaseDir   string
		allowedRoots  []string
		blockSymlinks bool
	}

	makeTool := func(t *testing.T, c cfg) *FSTool {
		t.Helper()
		opts := []FSToolOption{WithWorkBaseDir(c.workBaseDir), WithBlockSymlinks(c.blockSymlinks)}
		if c.allowedRoots != nil {
			opts = append(opts, WithAllowedRoots(c.allowedRoots))
		}
		return mustNewFSTool(t, opts...)
	}

	tests := []struct {
		name    string
		cfg     func(t *testing.T) cfg
		ctx     func(t *testing.T) context.Context
		args    func(t *testing.T, c cfg) ReadFileArgs
		wantErr func(error) bool

		wantKind     string // "text" | "file" | "image"
		wantText     string
		wantFileName string
		wantMIMEPref string
		wantBytes    []byte
	}{
		{
			name: "context_canceled",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			ctx: canceledContext,
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				return ReadFileArgs{Path: "whatever.txt"}
			},
			wantErr: wantErrIs(context.Canceled),
		},
		{
			name: "missing_path_errors",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				return ReadFileArgs{}
			},
			wantErr: wantErrAny,
		},
		{
			name: "nonexistent_file_errors_message",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				return ReadFileArgs{Path: filepath.Join(c.workBaseDir, "nope.txt")}
			},
			wantErr: wantErrContains("does not exist"),
		},
		{
			name: "read_text_default_encoding",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				mustWriteFile(t, filepath.Join(tmp, "file.txt"), []byte("hello world"))
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				return ReadFileArgs{Path: "file.txt"}
			},
			wantErr:  wantErrNone,
			wantKind: "text",
			wantText: "hello world",
		},
		{
			name: "read_text_encoding_trimmed_case_insensitive",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				mustWriteFile(t, filepath.Join(tmp, "file.txt"), []byte("hello world"))
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				return ReadFileArgs{Path: "file.txt", Encoding: "  TeXt "}
			},
			wantErr:  wantErrNone,
			wantKind: "text",
			wantText: "hello world",
		},
		{
			name: "read_binary_file_returns_file_union",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				mustWriteFile(t, filepath.Join(tmp, "file.bin"), []byte{0x00, 0x01, 0x02, 0x03})
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				return ReadFileArgs{Path: "file.bin", Encoding: "binary"}
			},
			wantErr:      wantErrNone,
			wantKind:     "file",
			wantFileName: "file.bin",
			wantMIMEPref: "application/",
			wantBytes:    []byte{0x00, 0x01, 0x02, 0x03},
		},
		{
			name: "read_png_as_binary_returns_image_union",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				mustWriteFile(t, filepath.Join(tmp, "image.png"), []byte{0x11, 0x22, 0x33})
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				return ReadFileArgs{Path: "image.png", Encoding: "binary"}
			},
			wantErr:      wantErrNone,
			wantKind:     "image",
			wantFileName: "image.png",
			wantMIMEPref: "image/",
			wantBytes:    []byte{0x11, 0x22, 0x33},
		},
		{
			name: "invalid_encoding_errors",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				mustWriteFile(t, filepath.Join(tmp, "file.txt"), []byte("x"))
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				return ReadFileArgs{Path: "file.txt", Encoding: "nope"}
			},
			wantErr: wantErrContains(`encoding must be "text" or "binary"`),
		},
		{
			name: "read_non_text_as_text_errors",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				mustWriteFile(t, filepath.Join(tmp, "file.bin"), []byte{0x00, 0x01, 0x02})
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				return ReadFileArgs{Path: "file.bin", Encoding: "text"}
			},
			wantErr: wantErrContains("cannot read non-text file"),
		},
		{
			name: "read_invalid_utf8_as_text_errors",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				mustWriteFile(t, filepath.Join(tmp, "bad.txt"), []byte{0xff, 0xfe})
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				return ReadFileArgs{Path: "bad.txt", Encoding: "text"}
			},
			wantErr: wantErrContains("not valid UTF-8"),
		},
		{
			name: "symlink_file_refused_when_blockSymlinks_true",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("symlink tests are unreliable on Windows CI")
				}
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp, blockSymlinks: true}
			},
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				target := filepath.Join(c.workBaseDir, "target.txt")
				mustWriteFile(t, target, []byte("ok"))
				link := filepath.Join(c.workBaseDir, "link.txt")
				mustSymlinkOrSkip(t, target, link)
				return ReadFileArgs{Path: "link.txt", Encoding: "text"}
			},
			wantErr: wantErrContains("symlink"),
		},
		{
			name: "allowedRoots_blocks_outside_file",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				root := t.TempDir()
				return cfg{workBaseDir: root, allowedRoots: []string{root}}
			},
			args: func(t *testing.T, c cfg) ReadFileArgs {
				t.Helper()
				outside := t.TempDir()
				p := filepath.Join(outside, "x.txt")
				mustWriteFile(t, p, []byte("x"))
				return ReadFileArgs{Path: p}
			},
			wantErr: wantErrContains("outside allowed roots"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.cfg(t)
			ft := makeTool(t, c)

			ctx := t.Context()
			if tt.ctx != nil {
				ctx = tt.ctx(t)
			}

			outs, err := ft.ReadFile(ctx, tt.args(t, c))
			if tt.wantErr == nil {
				tt.wantErr = wantErrNone
			}
			if !tt.wantErr(err) {
				t.Fatalf("err=%v did not match expectation", err)
			}
			if err != nil {
				return
			}

			if len(outs) != 1 {
				t.Fatalf("expected exactly 1 output, got %d", len(outs))
			}
			out := outs[0]
			if tt.wantKind != "" && string(out.Kind) != tt.wantKind {
				t.Fatalf("Kind=%q want=%q", string(out.Kind), tt.wantKind)
			}

			switch tt.wantKind {
			case "text":
				if out.TextItem == nil || out.FileItem != nil || out.ImageItem != nil {
					t.Fatalf("unexpected union shape for text: %#v", out)
				}
				if out.TextItem.Text != tt.wantText {
					t.Fatalf("Text=%q want=%q", out.TextItem.Text, tt.wantText)
				}
			case "file":
				if out.FileItem == nil || out.TextItem != nil || out.ImageItem != nil {
					t.Fatalf("unexpected union shape for file: %#v", out)
				}
				if tt.wantFileName != "" && out.FileItem.FileName != tt.wantFileName {
					t.Fatalf("FileName=%q want=%q", out.FileItem.FileName, tt.wantFileName)
				}
				if tt.wantMIMEPref != "" && !strings.HasPrefix(out.FileItem.FileMIME, tt.wantMIMEPref) {
					t.Fatalf("FileMIME=%q want prefix=%q", out.FileItem.FileMIME, tt.wantMIMEPref)
				}
				if tt.wantBytes != nil {
					raw := decodeBase64OrFail(t, out.FileItem.FileData)
					if !bytes.Equal(raw, tt.wantBytes) {
						t.Fatalf("decoded bytes=%v want=%v", raw, tt.wantBytes)
					}
				}
			case "image":
				if out.ImageItem == nil || out.TextItem != nil || out.FileItem != nil {
					t.Fatalf("unexpected union shape for image: %#v", out)
				}
				if tt.wantFileName != "" && out.ImageItem.ImageName != tt.wantFileName {
					t.Fatalf("ImageName=%q want=%q", out.ImageItem.ImageName, tt.wantFileName)
				}
				if tt.wantMIMEPref != "" && !strings.HasPrefix(out.ImageItem.ImageMIME, tt.wantMIMEPref) {
					t.Fatalf("ImageMIME=%q want prefix=%q", out.ImageItem.ImageMIME, tt.wantMIMEPref)
				}
				if tt.wantBytes != nil {
					raw := decodeBase64OrFail(t, out.ImageItem.ImageData)
					if !bytes.Equal(raw, tt.wantBytes) {
						t.Fatalf("decoded bytes=%v want=%v", raw, tt.wantBytes)
					}
				}
			default:
				t.Fatalf("unhandled wantKind=%q", tt.wantKind)
			}
		})
	}
}
