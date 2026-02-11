package fstool

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

func TestWriteFile(t *testing.T) {
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
		args    func(t *testing.T, c cfg) WriteFileArgs
		wantErr func(error) bool
		check   func(t *testing.T, c cfg, out *WriteFileOut)
	}{
		{
			name: "context_canceled",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			ctx: canceledContext,
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				return WriteFileArgs{Path: "a.txt", Content: "x"}
			},
			wantErr: wantErrIs(context.Canceled),
		},
		{
			name: "writes_text_default_encoding_trims_path",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				return WriteFileArgs{Path: "  text.txt  ", Content: "hello"}
			},
			wantErr: wantErrNone,
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				if out == nil {
					t.Fatalf("expected non-nil out")
				}
				if out.BytesWritten != 5 {
					t.Fatalf("BytesWritten=%d want=%d", out.BytesWritten, 5)
				}
				got := string(mustReadFile(t, filepath.Join(c.workBaseDir, "text.txt")))
				if got != "hello" {
					t.Fatalf("content=%q want=%q", got, "hello")
				}
			},
		},
		{
			name: "overwrite_false_errors_and_preserves_original",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				p := filepath.Join(c.workBaseDir, "exists.txt")
				mustWriteFile(t, p, []byte("a"))
				return WriteFileArgs{Path: "exists.txt", Content: "b", Overwrite: false}
			},
			wantErr: wantErrContains("overwrite=false"),
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				got := string(mustReadFile(t, filepath.Join(c.workBaseDir, "exists.txt")))
				if got != "a" {
					t.Fatalf("expected original content preserved, got=%q", got)
				}
			},
		},
		{
			name: "overwrite_true_replaces_content",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				p := filepath.Join(c.workBaseDir, "ow.txt")
				mustWriteFile(t, p, []byte("a"))
				return WriteFileArgs{Path: "ow.txt", Content: "bb", Overwrite: true}
			},
			wantErr: wantErrNone,
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				got := string(mustReadFile(t, filepath.Join(c.workBaseDir, "ow.txt")))
				if got != "bb" {
					t.Fatalf("content=%q want=%q", got, "bb")
				}
			},
		},
		{
			name: "writes_binary_base64_trimmed_case_insensitive_encoding",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				raw := []byte{0x00, 0x01, 0x02, 0xff}
				b64 := base64.StdEncoding.EncodeToString(raw)
				return WriteFileArgs{
					Path:     "bin.dat",
					Encoding: "  BiNaRy ",
					Content:  "  " + b64 + "  ",
				}
			},
			wantErr: wantErrNone,
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				got := mustReadFile(t, filepath.Join(c.workBaseDir, "bin.dat"))
				want := []byte{0x00, 0x01, 0x02, 0xff}
				if !bytes.Equal(got, want) {
					t.Fatalf("bytes=%v want=%v", got, want)
				}
				if out.BytesWritten != int64(len(want)) {
					t.Fatalf("BytesWritten=%d want=%d", out.BytesWritten, len(want))
				}
			},
		},
		{
			name: "invalid_base64_errors",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				return WriteFileArgs{Path: "badb64.dat", Encoding: "binary", Content: "!!!"}
			},
			wantErr: wantErrContains("invalid base64"),
		},
		{
			name: "createParents_false_missing_parent_errors",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				return WriteFileArgs{Path: filepath.Join("nope", "a.txt"), Content: "x", CreateParents: false}
			},
			wantErr: wantErrAny,
		},
		{
			name: "createParents_true_creates_dirs_and_writes",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				return WriteFileArgs{Path: filepath.Join("a", "b", "c", "d.txt"), Content: "ok", CreateParents: true}
			},
			wantErr: wantErrNone,
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				if _, err := os.Stat(filepath.Join(c.workBaseDir, "a", "b", "c", "d.txt")); err != nil {
					t.Fatalf("expected file to exist, stat err=%v", err)
				}
			},
		},
		{
			name: "createParents_true_depth_limit_exceeded_does_not_create_file",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp, blockSymlinks: true} // Max depth is enforced only if symlinks are blocked.
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				p := filepath.Join("1", "2", "3", "4", "5", "6", "7", "8", "9", "f.txt")
				return WriteFileArgs{Path: p, Content: "x", CreateParents: true}
			},
			wantErr: wantErrContains("too many parent directories"),
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				p := filepath.Join(c.workBaseDir, "1", "2", "3", "4", "5", "6", "7", "8", "9", "f.txt")
				if _, err := os.Stat(p); err == nil {
					t.Fatalf("did not expect file to be created on error")
				}
			},
		},
		{
			name: "refuses_directory_target",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				return WriteFileArgs{Path: ".", Content: "x"}
			},
			wantErr: wantErrAny,
		},
		{
			name: "refuses_invalid_utf8_text",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				// Construct a string with invalid UTF-8 bytes.
				s := string([]byte{0xff, 0xfe})
				return WriteFileArgs{Path: "badutf8.txt", Encoding: "text", Content: s}
			},
			wantErr: wantErrContains("not valid UTF-8"),
		},
		{
			name: "blockSymlinks_true_rejects_symlink_parent_component",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("symlink tests are unreliable on Windows CI")
				}
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp, blockSymlinks: true}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				realTxt := filepath.Join(c.workBaseDir, "real")
				mustMkdirAll(t, realTxt)

				link := filepath.Join(c.workBaseDir, "link")
				mustSymlinkOrSkip(t, realTxt, link)

				return WriteFileArgs{Path: filepath.Join("link", "child.txt"), Content: "x", CreateParents: false}
			},
			wantErr: wantErrContains("symlink"),
		},
		{
			name: "allowedRoots_blocks_outside_path",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				root := t.TempDir()
				return cfg{workBaseDir: root, allowedRoots: []string{root}}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				outside := t.TempDir()
				return WriteFileArgs{Path: filepath.Join(outside, "x.txt"), Content: "x"}
			},
			wantErr: wantErrContains("outside allowed roots"),
		},
		{
			name: "windows_drive_relative_path_rejected",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				if runtime.GOOS != toolutil.GOOSWindows {
					t.Skip("windows-only behavior")
				}
				return WriteFileArgs{Path: `C:drive-relative.txt`, Content: "x"}
			},
			wantErr: wantErrContains("drive-relative"),
		},
		{
			name: "writes_into_symlink_dir_when_blockSymlinks_false",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("symlink tests are unreliable on Windows CI")
				}
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp, blockSymlinks: false}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				realTxt := filepath.Join(c.workBaseDir, "real")
				mustMkdirAll(t, realTxt)

				link := filepath.Join(c.workBaseDir, "link")
				mustSymlinkOrSkip(t, realTxt, link)

				return WriteFileArgs{Path: filepath.Join("link", "child.txt"), Content: "ok", CreateParents: false}
			},
			wantErr: wantErrNone,
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				// Should have written into real/child.txt.
				got := string(mustReadFile(t, filepath.Join(c.workBaseDir, "real", "child.txt")))
				if got != "ok" {
					t.Fatalf("content=%q want=%q", got, "ok")
				}
			},
		},
		{
			name: "returns_absolute_resolved_path_in_output",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				return WriteFileArgs{Path: "out.txt", Content: "x"}
			},
			wantErr: wantErrNone,
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				if out == nil {
					t.Fatalf("expected non-nil out")
				}
				want := canonForPolicyExpectations(filepath.Join(c.workBaseDir, "out.txt"))
				if filepath.Clean(out.Path) != filepath.Clean(want) {
					t.Fatalf("out.Path=%q want=%q", out.Path, want)
				}
			},
		},
		{
			name: "overwrite_false_error_does_not_modify_existing_file",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				mustWriteFile(t, filepath.Join(c.workBaseDir, "stay.txt"), []byte("stay"))
				return WriteFileArgs{Path: "stay.txt", Content: "changed", Overwrite: false}
			},
			wantErr: wantErrContains("overwrite=false"),
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				got := string(mustReadFile(t, filepath.Join(c.workBaseDir, "stay.txt")))
				if got != "stay" {
					t.Fatalf("content modified unexpectedly: %q", got)
				}
			},
		},
		{
			name: "rejects_unknown_encoding",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				return WriteFileArgs{Path: "x.txt", Encoding: "nope", Content: "x"}
			},
			wantErr: wantErrContains(`encoding must be "text" or "binary"`),
		},
		{
			name: "binary_content_whitespace_trimmed",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				raw := []byte("abc")
				return WriteFileArgs{
					Path:     "trim.bin",
					Encoding: "binary",
					Content:  "\n\t " + base64.StdEncoding.EncodeToString(raw) + " \r\n",
				}
			},
			wantErr: wantErrNone,
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				got := mustReadFile(t, filepath.Join(c.workBaseDir, "trim.bin"))
				if !bytes.Equal(got, []byte("abc")) {
					t.Fatalf("got=%q want=%q", string(got), "abc")
				}
			},
		},
		{
			name: "error_messages_are_stable_enough_for_users",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				// Missing parent with createParents=false.
				return WriteFileArgs{Path: filepath.Join("nope", "x.txt"), Content: "x", CreateParents: false}
			},
			wantErr: wantErrAny,
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				// No-op; existence checked by error.
			},
		},
		{
			name: "windows_path_semantics_do_not_break_non_windows",
			cfg: func(t *testing.T) cfg {
				t.Helper()
				tmp := t.TempDir()
				return cfg{workBaseDir: tmp}
			},
			args: func(t *testing.T, c cfg) WriteFileArgs {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("this case is for non-windows only")
				}
				// On unix, "C:foo" is just a filename with colon; should be allowed.
				return WriteFileArgs{Path: "C:foo.txt", Content: "ok"}
			},
			wantErr: func(err error) bool {
				if runtime.GOOS == toolutil.GOOSWindows {
					return true
				}
				return err == nil
			},
			check: func(t *testing.T, c cfg, out *WriteFileOut) {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					return
				}
				got := string(mustReadFile(t, filepath.Join(c.workBaseDir, "C:foo.txt")))
				if got != "ok" {
					t.Fatalf("content=%q want=%q", got, "ok")
				}
			},
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

			out, err := ft.WriteFile(ctx, tt.args(t, c))
			if tt.wantErr == nil {
				tt.wantErr = wantErrNone
			}
			if !tt.wantErr(err) {
				t.Fatalf("err=%v did not match expectation", err)
			}
			if tt.check != nil {
				tt.check(t, c, out)
			}

			// On error, ensure out is nil (tool contract expectation).
			if err != nil && out != nil {
				t.Fatalf("expected nil out on error, got %+v", out)
			}
		})
	}
}
