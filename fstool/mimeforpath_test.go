package fstool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

func TestMIMEForPath(t *testing.T) {
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

	pngHeader := []byte{
		0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
		0x00, 0x00, 0x00, 0x0D, 'I', 'H', 'D', 'R',
	}

	tests := []struct {
		name    string
		cfg     func(t *testing.T) cfg
		ctx     func(t *testing.T) context.Context
		args    func(t *testing.T, c cfg) MIMEForPathArgs
		wantErr func(error) bool

		wantMethod MIMEDetectMethod
		wantMIME   string
		wantMode   MIMEMode
		wantExt    string
		wantNorm   string
	}{
		{
			name: "context_canceled",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			ctx: canceledContext,
			args: func(t *testing.T, c cfg) MIMEForPathArgs {
				t.Helper()
				return MIMEForPathArgs{Path: "whatever.txt"}
			},
			wantErr: wantErrIs(context.Canceled),
		},
		{
			name: "invalid_path_empty",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) MIMEForPathArgs {
				t.Helper()
				return MIMEForPathArgs{Path: "   "}
			},
			wantErr: wantErrContains("invalid path"),
		},
		{
			name: "nonexistent_known_extension_uses_extension_no_io",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) MIMEForPathArgs {
				t.Helper()
				return MIMEForPathArgs{Path: filepath.Join(c.workBaseDir, "missing.PDF")}
			},
			wantErr:    wantErrNone,
			wantMethod: MIMEDetectMethodExtension,
			wantMIME:   "application/pdf",
			wantMode:   MIMEModeDocument,
			wantExt:    ".PDF",
			wantNorm:   ".pdf",
		},
		{
			name: "nonexistent_unknown_extension_errors_isnotexist",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) MIMEForPathArgs {
				t.Helper()
				return MIMEForPathArgs{Path: filepath.Join(c.workBaseDir, "missing.unknownext")}
			},
			wantErr: func(err error) bool { return err != nil && os.IsNotExist(err) },
		},
		{
			name: "existing_no_extension_sniffs_text",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				mustWriteFile(t, filepath.Join(tmp, "noext"), []byte("hello\n"))
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) MIMEForPathArgs {
				t.Helper()
				return MIMEForPathArgs{Path: filepath.Join(c.workBaseDir, "noext")}
			},
			wantErr:    wantErrNone,
			wantMethod: MIMEDetectMethodSniff,
			wantMIME:   "text/plain; charset=utf-8",
			wantMode:   MIMEModeText,
			wantExt:    "",
			wantNorm:   "",
		},
		{
			name: "existing_unknown_extension_sniffs_png",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				mustWriteFile(t, filepath.Join(tmp, "x.bin"), pngHeader)
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) MIMEForPathArgs {
				t.Helper()
				return MIMEForPathArgs{Path: filepath.Join(c.workBaseDir, "x.bin")}
			},
			wantErr:    wantErrNone,
			wantMethod: MIMEDetectMethodSniff,
			wantMIME:   "image/png",
			wantMode:   MIMEModeImage,
			wantExt:    ".bin",
			wantNorm:   ".bin",
		},
		{
			name: "directory_path_errors",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) MIMEForPathArgs {
				t.Helper()
				return MIMEForPathArgs{Path: c.workBaseDir}
			},
			wantErr: wantErrAny,
		},
		{
			name: "allowedRoots_blocks_outside_path",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				root := t.TempDir()
				return cfg{workBaseDir: root, allowedRoots: []string{root}}
			},
			args: func(t *testing.T, c cfg) MIMEForPathArgs {
				t.Helper()
				outside := t.TempDir()
				return MIMEForPathArgs{Path: filepath.Join(outside, "x.txt")}
			},
			wantErr: wantErrContains("outside allowed roots"),
		},
		{
			name: "symlink_sniff_allowed_when_blockSymlinks_false",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("symlink tests are unreliable on Windows CI")
				}
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp, blockSymlinks: false}
			},
			args: func(t *testing.T, c cfg) MIMEForPathArgs {
				t.Helper()
				target := filepath.Join(c.workBaseDir, "target.unknownext")
				mustWriteFile(t, target, []byte("hello\n"))
				link := filepath.Join(c.workBaseDir, "link.unknownext")
				mustSymlinkOrSkip(t, target, link)
				return MIMEForPathArgs{Path: link}
			},
			wantErr:    wantErrNone,
			wantMethod: MIMEDetectMethodSniff,
			wantMode:   MIMEModeText,
			wantExt:    ".unknownext",
			wantNorm:   ".unknownext",
		},
		{
			name: "symlink_sniff_refused_when_blockSymlinks_true",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("symlink tests are unreliable on Windows CI")
				}
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp, blockSymlinks: true}
			},
			args: func(t *testing.T, c cfg) MIMEForPathArgs {
				t.Helper()
				target := filepath.Join(c.workBaseDir, "target.unknownext")
				mustWriteFile(t, target, []byte("hello\n"))
				link := filepath.Join(c.workBaseDir, "link.unknownext")
				mustSymlinkOrSkip(t, target, link)
				return MIMEForPathArgs{Path: link}
			},
			wantErr: wantErrContains("symlink"),
		},
		{
			name: "windows_drive_relative_path_rejected",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) MIMEForPathArgs {
				t.Helper()
				if runtime.GOOS != toolutil.GOOSWindows {
					t.Skip("windows-only behavior")
				}
				return MIMEForPathArgs{Path: `C:drive-relative.txt`}
			},
			wantErr: wantErrContains("drive-relative"),
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

			out, err := ft.MIMEForPath(ctx, tt.args(t, c))
			if tt.wantErr == nil {
				tt.wantErr = wantErrNone
			}
			if !tt.wantErr(err) {
				t.Fatalf("err=%v did not match expectation", err)
			}
			if err != nil {
				return
			}
			if out == nil {
				t.Fatalf("expected non-nil out")
			}

			if tt.wantMethod != "" && out.Method != tt.wantMethod {
				t.Fatalf("Method=%q want=%q", out.Method, tt.wantMethod)
			}
			if tt.wantMIME != "" && out.MIMEType != tt.wantMIME {
				t.Fatalf("MIMEType=%q want=%q", out.MIMEType, tt.wantMIME)
			}
			if tt.wantMode != "" && out.Mode != tt.wantMode {
				t.Fatalf("Mode=%q want=%q", out.Mode, tt.wantMode)
			}
			if out.Extension != tt.wantExt {
				t.Fatalf("Extension=%q want=%q", out.Extension, tt.wantExt)
			}
			if out.NormalizedExtension != tt.wantNorm {
				t.Fatalf("NormalizedExtension=%q want=%q", out.NormalizedExtension, tt.wantNorm)
			}
		})
	}
}
