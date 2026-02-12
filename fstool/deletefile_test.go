package fstool

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

func TestDeleteFile(t *testing.T) {
	type policyCfg struct {
		name          string
		allowedRoots  []string
		workBaseDir   string
		blockSymlinks bool
	}

	makeTool := func(t *testing.T, cfg policyCfg) *FSTool {
		t.Helper()
		opts := []FSToolOption{
			WithBlockSymlinks(cfg.blockSymlinks),
		}
		if cfg.workBaseDir != "" {
			opts = append(opts, WithWorkBaseDir(cfg.workBaseDir))
		}
		if cfg.allowedRoots != nil {
			opts = append(opts, WithAllowedRoots(cfg.allowedRoots))
		}
		return mustNewFSTool(t, opts...)
	}

	systemTrashExpected := func(t *testing.T, home string) (string, bool) {
		t.Helper()
		switch runtime.GOOS {
		case toolutil.GOOSDarwin:
			return filepath.Join(home, ".Trash"), true
		case toolutil.GOOSLinux, "freebsd", "openbsd", "netbsd", "dragonfly":
			xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
			if xdg != "" {
				return filepath.Join(xdg, "Trash", "files"), true
			}
			return filepath.Join(home, ".local", "share", "Trash", "files"), true
		default:
			return "", false
		}
	}

	tests := []struct {
		name    string
		cfg     func(t *testing.T) policyCfg
		ctx     func(t *testing.T) context.Context
		setup   func(t *testing.T, cfg policyCfg) (src string, args DeleteFileArgs, check func(t *testing.T, out *DeleteFileOut))
		wantErr func(error) bool
	}{
		{
			name: "context_canceled_does_not_delete",
			cfg: func(t *testing.T) policyCfg {
				t.Helper()
				tmp := t.TempDir()
				return policyCfg{name: "default", workBaseDir: tmp}
			},
			ctx: canceledContext,
			setup: func(t *testing.T, cfg policyCfg) (string, DeleteFileArgs, func(t *testing.T, out *DeleteFileOut)) {
				t.Helper()
				src := filepath.Join(cfg.workBaseDir, "a.txt")
				mustWriteFile(t, src, []byte("x"))
				trash := filepath.Join(cfg.workBaseDir, "trash")
				return src, DeleteFileArgs{Path: src, TrashDir: trash}, func(t *testing.T, out *DeleteFileOut) {
					t.Helper()
					if _, err := os.Lstat(src); err != nil {
						t.Fatalf("expected original to remain, stat err=%v", err)
					}
				}
			},
			wantErr: wantErrIs(context.Canceled),
		},
		{
			name: "nonexistent_preserves_isnotexist",
			cfg: func(t *testing.T) policyCfg {
				t.Helper()
				tmp := t.TempDir()
				return policyCfg{workBaseDir: tmp}
			},
			setup: func(t *testing.T, cfg policyCfg) (string, DeleteFileArgs, func(t *testing.T, out *DeleteFileOut)) {
				t.Helper()
				src := filepath.Join(cfg.workBaseDir, "missing.txt")
				trash := filepath.Join(cfg.workBaseDir, "trash")
				return src, DeleteFileArgs{Path: src, TrashDir: trash}, nil
			},
			wantErr: func(err error) bool { return err != nil && os.IsNotExist(err) },
		},
		{
			name: "directory_errors",
			cfg: func(t *testing.T) policyCfg {
				t.Helper()
				tmp := t.TempDir()
				return policyCfg{workBaseDir: tmp}
			},
			setup: func(t *testing.T, cfg policyCfg) (string, DeleteFileArgs, func(t *testing.T, out *DeleteFileOut)) {
				t.Helper()
				trash := filepath.Join(cfg.workBaseDir, "trash")
				return cfg.workBaseDir, DeleteFileArgs{Path: cfg.workBaseDir, TrashDir: trash}, nil
			},
			wantErr: wantErrAny,
		},
		{
			name: "explicit_trashdir_trims_args_and_moves",
			cfg: func(t *testing.T) policyCfg {
				t.Helper()
				tmp := t.TempDir()
				return policyCfg{workBaseDir: tmp}
			},
			setup: func(t *testing.T, cfg policyCfg) (string, DeleteFileArgs, func(t *testing.T, out *DeleteFileOut)) {
				t.Helper()
				src := filepath.Join(cfg.workBaseDir, "a.txt")
				mustWriteFile(t, src, []byte("hello"))
				trash := filepath.Join(cfg.workBaseDir, "trash")
				args := DeleteFileArgs{
					Path:     "  " + src + "  ",
					TrashDir: "  " + trash + "  ",
				}
				return src, args, func(t *testing.T, out *DeleteFileOut) {
					t.Helper()
					if out == nil {
						t.Fatalf("expected non-nil out")
					}
					gotDir := filepath.Clean(filepath.Dir(out.TrashedPath))
					gotDir = evalTestSymlinksBestEffort(gotDir)
					wantDir := canonForPolicyExpectations(trash)
					if gotDir != filepath.Clean(wantDir) {
						t.Fatalf("trashed dir=%q want=%q (out=%+v)", gotDir, wantDir, out)
					}
					if _, err := os.Lstat(src); !os.IsNotExist(err) {
						t.Fatalf("expected original removed, stat err=%v", err)
					}
					if got := string(mustReadFile(t, out.TrashedPath)); got != "hello" {
						t.Fatalf("trashed content=%q want=%q", got, "hello")
					}
				}
			},
			wantErr: wantErrNone,
		},
		{
			name: "name_collision_gets_unique_name",
			cfg: func(t *testing.T) policyCfg {
				t.Helper()
				tmp := t.TempDir()
				return policyCfg{workBaseDir: tmp}
			},
			setup: func(t *testing.T, cfg policyCfg) (string, DeleteFileArgs, func(t *testing.T, out *DeleteFileOut)) {
				t.Helper()
				trash := filepath.Join(cfg.workBaseDir, "trash")
				src := filepath.Join(cfg.workBaseDir, "same.txt")

				mustWriteFile(t, src, []byte("one"))
				ft := makeTool(t, cfg)
				out1, err := ft.DeleteFile(t.Context(), DeleteFileArgs{Path: src, TrashDir: trash})
				if err != nil {
					t.Fatalf("seed delete #1: %v", err)
				}

				mustWriteFile(t, src, []byte("two"))
				out2, err := ft.DeleteFile(t.Context(), DeleteFileArgs{Path: src, TrashDir: trash})
				if err != nil {
					t.Fatalf("seed delete #2: %v", err)
				}

				if out1.TrashedPath == out2.TrashedPath {
					t.Fatalf("expected unique trashed paths, got same: %q", out1.TrashedPath)
				}
				if string(mustReadFile(t, out1.TrashedPath)) != "one" {
					t.Fatalf("content mismatch for out1")
				}
				if string(mustReadFile(t, out2.TrashedPath)) != "two" {
					t.Fatalf("content mismatch for out2")
				}

				// Return a no-op test case since we've already asserted.
				return "skip", DeleteFileArgs{Path: src, TrashDir: trash}, func(t *testing.T, out *DeleteFileOut) {
					t.Helper()
				}
			},
			wantErr: wantErrNone,
		},
		{
			name: "trashdir_is_file_errors_and_original_remains",
			cfg: func(t *testing.T) policyCfg {
				t.Helper()
				tmp := t.TempDir()
				return policyCfg{workBaseDir: tmp}
			},
			setup: func(t *testing.T, cfg policyCfg) (string, DeleteFileArgs, func(t *testing.T, out *DeleteFileOut)) {
				t.Helper()
				src := filepath.Join(cfg.workBaseDir, "a.txt")
				mustWriteFile(t, src, []byte("x"))

				trashAsFile := filepath.Join(cfg.workBaseDir, "trash")
				mustWriteFile(t, trashAsFile, []byte("not a dir"))

				return src, DeleteFileArgs{Path: src, TrashDir: trashAsFile}, func(t *testing.T, out *DeleteFileOut) {
					t.Helper()
					if _, err := os.Lstat(src); err != nil {
						t.Fatalf("expected original to remain, stat err=%v", err)
					}
				}
			},
			wantErr: wantErrAny,
		},
		{
			name: "auto_uses_system_trash_or_falls_back_to_local",
			cfg: func(t *testing.T) policyCfg {
				t.Helper()
				tmp := t.TempDir()
				return policyCfg{workBaseDir: tmp}
			},
			setup: func(t *testing.T, cfg policyCfg) (string, DeleteFileArgs, func(t *testing.T, out *DeleteFileOut)) {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("system trash detection is not defined in this tool for Windows; auto uses local fallback")
				}

				tmpHome := t.TempDir()
				t.Setenv("HOME", tmpHome)

				// Prefer XDG on linux/bsd.
				xdg := filepath.Join(tmpHome, "xdgdata")
				t.Setenv("XDG_DATA_HOME", xdg)

				src := filepath.Join(cfg.workBaseDir, "auto.txt")
				mustWriteFile(t, src, []byte("x"))

				wantTrash, ok := systemTrashExpected(t, tmpHome)
				if !ok {
					t.Skipf("system trash not defined for GOOS=%q in this test", runtime.GOOS)
				}

				return src, DeleteFileArgs{Path: src, TrashDir: "auto"}, func(t *testing.T, out *DeleteFileOut) {
					t.Helper()
					if out == nil {
						t.Fatalf("expected non-nil out")
					}
					gotDir := filepath.Clean(filepath.Dir(out.TrashedPath))
					wantDir := filepath.Clean(canonForPolicyExpectations(wantTrash))
					if gotDir != wantDir {
						t.Fatalf("trashed dir=%q want=%q (trash=%q)", gotDir, wantDir, wantTrash)
					}
				}
			},
			wantErr: wantErrNone,
		},
		{
			name: "auto_falls_back_to_local_when_system_trash_unusable",
			cfg: func(t *testing.T) policyCfg {
				t.Helper()
				tmp := t.TempDir()
				return policyCfg{workBaseDir: tmp}
			},
			setup: func(t *testing.T, cfg policyCfg) (string, DeleteFileArgs, func(t *testing.T, out *DeleteFileOut)) {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("HOME/XDG behavior differs on Windows")
				}

				tmpHome := t.TempDir()
				t.Setenv("HOME", tmpHome)
				xdg := filepath.Join(tmpHome, "xdgdata")
				t.Setenv("XDG_DATA_HOME", xdg)

				// Break system trash by making it a file, so EnsureDir fails.
				switch runtime.GOOS {
				case toolutil.GOOSDarwin:
					mustWriteFile(t, filepath.Join(tmpHome, ".Trash"), []byte("not a dir"))
				case toolutil.GOOSLinux, "freebsd", "openbsd", "netbsd", "dragonfly":
					mustMkdirAll(t, filepath.Join(xdg, "Trash"))
					mustWriteFile(t, filepath.Join(xdg, "Trash", "files"), []byte("not a dir"))
				default:
					t.Skipf("system trash not defined for GOOS=%q in this test", runtime.GOOS)
				}

				src := filepath.Join(cfg.workBaseDir, "auto.txt")
				mustWriteFile(t, src, []byte("x"))

				return src, DeleteFileArgs{Path: src, TrashDir: "   "}, func(t *testing.T, out *DeleteFileOut) {
					t.Helper()
					if out == nil {
						t.Fatalf("expected non-nil out")
					}
					wantFallback := filepath.Join(filepath.Dir(canonForPolicyExpectations(src)), ".trash")
					gotDir := filepath.Clean(filepath.Dir(out.TrashedPath))
					if gotDir != filepath.Clean(wantFallback) {
						t.Fatalf("trashed dir=%q want fallback=%q", gotDir, wantFallback)
					}
				}
			},
			wantErr: wantErrNone,
		},
		{
			name: "allowedRoots_blocks_outside_path",
			cfg: func(t *testing.T) policyCfg {
				t.Helper()
				root := t.TempDir()
				outside := t.TempDir()
				_ = outside
				return policyCfg{
					workBaseDir:  root,
					allowedRoots: []string{root},
				}
			},
			setup: func(t *testing.T, cfg policyCfg) (string, DeleteFileArgs, func(t *testing.T, out *DeleteFileOut)) {
				t.Helper()
				outside := t.TempDir()
				src := filepath.Join(outside, "x.txt")
				mustWriteFile(t, src, []byte("x"))
				return src, DeleteFileArgs{Path: src, TrashDir: filepath.Join(cfg.workBaseDir, "trash")}, nil
			},
			wantErr: wantErrContains("outside allowed roots"),
		},
		{
			name: "symlink_file_allowed_when_blockSymlinks_false",
			cfg: func(t *testing.T) policyCfg {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("symlink tests are unreliable on Windows CI")
				}
				tmp := t.TempDir()
				return policyCfg{workBaseDir: tmp, blockSymlinks: false}
			},
			setup: func(t *testing.T, cfg policyCfg) (string, DeleteFileArgs, func(t *testing.T, out *DeleteFileOut)) {
				t.Helper()
				target := filepath.Join(cfg.workBaseDir, "target.txt")
				mustWriteFile(t, target, []byte("keep"))

				link := filepath.Join(cfg.workBaseDir, "link.txt")
				mustSymlinkOrSkip(t, target, link)

				trash := filepath.Join(cfg.workBaseDir, "trash")
				return link, DeleteFileArgs{Path: link, TrashDir: trash}, func(t *testing.T, out *DeleteFileOut) {
					t.Helper()
					t.Helper()
					if _, err := os.Stat(target); err != nil {
						t.Fatalf("target missing: %v", err)
					}
					if _, err := os.Lstat(link); !os.IsNotExist(err) {
						t.Fatalf("expected original link removed, stat err=%v", err)
					}
					st, err := os.Lstat(out.TrashedPath)
					if err != nil {
						t.Fatalf("Lstat trashed: %v", err)
					}
					if (st.Mode() & os.ModeSymlink) == 0 {
						t.Fatalf("expected symlink in trash, got mode=%v", st.Mode())
					}
				}
			},
			wantErr: wantErrNone,
		},
		{
			name: "symlink_file_refused_when_blockSymlinks_true",
			cfg: func(t *testing.T) policyCfg {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("symlink tests are unreliable on Windows CI")
				}
				tmp := t.TempDir()
				return policyCfg{workBaseDir: tmp, blockSymlinks: true}
			},
			setup: func(t *testing.T, cfg policyCfg) (string, DeleteFileArgs, func(t *testing.T, out *DeleteFileOut)) {
				t.Helper()
				target := filepath.Join(cfg.workBaseDir, "target.txt")
				mustWriteFile(t, target, []byte("keep"))

				link := filepath.Join(cfg.workBaseDir, "link.txt")
				mustSymlinkOrSkip(t, target, link)

				trash := filepath.Join(cfg.workBaseDir, "trash")
				return link, DeleteFileArgs{Path: link, TrashDir: trash}, func(t *testing.T, out *DeleteFileOut) {
					t.Helper()
					t.Helper()
					if _, err := os.Lstat(link); err != nil {
						t.Fatalf("expected original link to remain, stat err=%v", err)
					}
				}
			},
			wantErr: wantErrContains("symlink"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg(t)
			ft := makeTool(t, cfg)

			ctx := t.Context()
			if tt.ctx != nil {
				ctx = tt.ctx(t)
			}

			src, args, check := tt.setup(t, cfg)
			if src == "skip" {
				return
			}

			out, err := ft.DeleteFile(ctx, args)
			if tt.wantErr == nil {
				tt.wantErr = wantErrNone
			}
			if !tt.wantErr(err) {
				t.Fatalf("err=%v did not match expectation", err)
			}
			if err == nil && check != nil {
				check(t, out)
			}
			if err != nil && check != nil {
				check(t, nil)
			}
		})
	}
}
