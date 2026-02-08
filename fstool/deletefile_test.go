package fstool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/flexigpt/llmtools-go/internal/fileutil"
	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

func TestDeleteFile(t *testing.T) {
	type tc struct {
		name string
		run  func(t *testing.T)
	}

	writeFile := func(t *testing.T, p string, data []byte) {
		t.Helper()
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatalf("WriteFile(%q): %v", p, err)
		}
	}

	tests := []tc{
		{
			name: "context_canceled_does_not_delete",
			run: func(t *testing.T) {
				t.Helper()
				tmp := t.TempDir()
				p := filepath.Join(tmp, "a.txt")
				writeFile(t, p, []byte("x"))

				ctx, cancel := context.WithCancel(t.Context())
				cancel()

				_, err := deleteFile(ctx, DeleteFileArgs{Path: p, TrashDir: filepath.Join(tmp, "trash")}, "", nil)
				if err == nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("expected context.Canceled, got: %v", err)
				}
				if _, statErr := os.Lstat(p); statErr != nil {
					t.Fatalf("expected original to remain, stat err=%v", statErr)
				}
			},
		},
		{
			name: "nonexistent_errors_isnotexist",
			run: func(t *testing.T) {
				t.Helper()
				tmp := t.TempDir()
				_, err := deleteFile(t.Context(), DeleteFileArgs{
					Path:     filepath.Join(tmp, "missing.txt"),
					TrashDir: filepath.Join(tmp, "trash"),
				}, "", nil)
				if err == nil {
					t.Fatalf("expected error")
				}
				if !os.IsNotExist(err) {
					t.Fatalf("expected IsNotExist, got: %T %v", err, err)
				}
			},
		},
		{
			name: "directory_errors",
			run: func(t *testing.T) {
				t.Helper()
				tmp := t.TempDir()
				_, err := deleteFile(t.Context(), DeleteFileArgs{
					Path:     tmp,
					TrashDir: filepath.Join(tmp, "trash"),
				}, "", nil)
				if err == nil {
					t.Fatalf("expected error")
				}
			},
		},
		{
			name: "moves_to_explicit_trashDir_trims_args",
			run: func(t *testing.T) {
				t.Helper()
				tmp := t.TempDir()
				p := filepath.Join(tmp, "a.txt")
				writeFile(t, p, []byte("hello"))

				trash := filepath.Join(tmp, "trash")
				out, err := deleteFile(t.Context(), DeleteFileArgs{
					Path:     "  " + p + "  ",
					TrashDir: "  " + trash + "  ",
				}, "", nil)
				if err != nil {
					t.Fatalf("deleteFile: %v", err)
				}
				if out == nil {
					t.Fatalf("expected non-nil out")
				}
				if gotDir := filepath.Clean(
					filepath.Dir(out.TrashedPath),
				); gotDir != filepath.Clean(
					fileutil.ApplyDarwinSystemRootAliases(trash),
				) {
					t.Fatalf("trashed dir=%q want=%q (out=%+v)", gotDir, trash, out)
				}
				if out.Method != DeleteFileMethodRename {
					t.Fatalf("method=%q want=%q", out.Method, DeleteFileMethodRename)
				}

				if _, statErr := os.Lstat(p); !os.IsNotExist(statErr) {
					t.Fatalf("expected original removed, stat err=%v", statErr)
				}
				b, rerr := os.ReadFile(out.TrashedPath)
				if rerr != nil {
					t.Fatalf("ReadFile trashed: %v", rerr)
				}
				if string(b) != "hello" {
					t.Fatalf("content mismatch: %q", string(b))
				}
			},
		},
		{
			name: "name_collision_gets_unique_name",
			run: func(t *testing.T) {
				t.Helper()
				tmp := t.TempDir()
				trash := filepath.Join(tmp, "trash")

				p := filepath.Join(tmp, "same.txt")
				writeFile(t, p, []byte("one"))
				out1, err := deleteFile(t.Context(), DeleteFileArgs{Path: p, TrashDir: trash}, "", nil)
				if err != nil {
					t.Fatalf("deleteFile #1: %v", err)
				}

				writeFile(t, p, []byte("two"))
				out2, err := deleteFile(t.Context(), DeleteFileArgs{Path: p, TrashDir: trash}, "", nil)
				if err != nil {
					t.Fatalf("deleteFile #2: %v", err)
				}

				if out1.TrashedPath == out2.TrashedPath {
					t.Fatalf("expected unique trashed paths, got same: %q", out1.TrashedPath)
				}

				b1, err := os.ReadFile(out1.TrashedPath)
				if err != nil {
					t.Fatalf("ReadFile #1 trashed: %v", err)
				}
				b2, err := os.ReadFile(out2.TrashedPath)
				if err != nil {
					t.Fatalf("ReadFile #2 trashed: %v", err)
				}
				if string(b1) != "one" || string(b2) != "two" {
					t.Fatalf("content mismatch: b1=%q b2=%q", string(b1), string(b2))
				}
			},
		},
		{
			name: "trashDir_is_file_errors_and_original_remains",
			run: func(t *testing.T) {
				t.Helper()
				tmp := t.TempDir()

				p := filepath.Join(tmp, "a.txt")
				writeFile(t, p, []byte("x"))

				trashAsFile := filepath.Join(tmp, "trash")
				writeFile(t, trashAsFile, []byte("not a dir"))

				_, err := deleteFile(t.Context(), DeleteFileArgs{Path: p, TrashDir: trashAsFile}, "", nil)
				if err == nil {
					t.Fatalf("expected error")
				}
				if _, statErr := os.Lstat(p); statErr != nil {
					t.Fatalf("expected original to remain, stat err=%v", statErr)
				}
			},
		},
		{
			name: "auto_uses_system_trash_when_set_non_windows",
			run: func(t *testing.T) {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("HOME/XDG behavior differs on Windows")
				}

				tmpHome := t.TempDir()
				t.Setenv("HOME", tmpHome)
				xdg := filepath.Join(tmpHome, "xdgdata")
				t.Setenv("XDG_DATA_HOME", xdg)

				tmp := t.TempDir()
				p := filepath.Join(tmp, "auto.txt")
				writeFile(t, p, []byte("x"))

				out, err := deleteFile(t.Context(), DeleteFileArgs{Path: p, TrashDir: "auto"}, "", nil)
				if err != nil {
					t.Fatalf("deleteFile(auto): %v", err)
				}

				var wantTrash string
				switch runtime.GOOS {
				case toolutil.GOOSDarwin:
					wantTrash = fileutil.ApplyDarwinSystemRootAliases(filepath.Join(tmpHome, ".Trash"))
				case toolutil.GOOSLinux,
					toolutil.GOOSFreebsd,
					toolutil.GOOSOpenbsd,
					toolutil.GOOSNetbsd,
					toolutil.GOOSDragonfly:
					wantTrash = filepath.Join(xdg, "Trash", "files")
				default:
					t.Skipf("system trash not defined for GOOS=%q in this test", runtime.GOOS)
				}

				if filepath.Clean(filepath.Dir(out.TrashedPath)) != filepath.Clean(wantTrash) {
					t.Fatalf("trashed dir=%q want=%q", filepath.Dir(out.TrashedPath), wantTrash)
				}
				if out.Method != DeleteFileMethodRename {
					t.Fatalf("method=%q want=%q", out.Method, DeleteFileMethodRename)
				}
			},
		},
		{
			name: "auto_falls_back_to_local_trash_when_system_trash_unusable",
			run: func(t *testing.T) {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("HOME/XDG behavior differs on Windows")
				}

				tmpHome := t.TempDir()
				t.Setenv("HOME", tmpHome)
				xdg := filepath.Join(tmpHome, "xdgdata")
				t.Setenv("XDG_DATA_HOME", xdg)

				// Break system trash by making it a file, so EnsureDirNoSymlink fails.
				switch runtime.GOOS {
				case toolutil.GOOSDarwin:
					writeFile(t, filepath.Join(tmpHome, ".Trash"), []byte("not a dir"))
				case toolutil.GOOSLinux,
					toolutil.GOOSFreebsd,
					toolutil.GOOSOpenbsd,
					toolutil.GOOSNetbsd,
					toolutil.GOOSDragonfly:
					if err := os.MkdirAll(filepath.Join(xdg, "Trash"), 0o755); err != nil {
						t.Fatalf("MkdirAll: %v", err)
					}
					writeFile(t, filepath.Join(xdg, "Trash", "files"), []byte("not a dir"))
				default:
					t.Skipf("system trash not defined for GOOS=%q in this test", runtime.GOOS)
				}

				tmp := t.TempDir()
				p := filepath.Join(tmp, "auto.txt")
				writeFile(t, p, []byte("x"))

				out, err := deleteFile(t.Context(), DeleteFileArgs{Path: p, TrashDir: "   "}, "", nil)
				if err != nil {
					t.Fatalf("deleteFile(auto whitespace): %v", err)
				}
				wantFallback := filepath.Join(filepath.Dir(p), ".trash")
				if filepath.Clean(
					filepath.Dir(out.TrashedPath),
				) != filepath.Clean(
					fileutil.ApplyDarwinSystemRootAliases(wantFallback),
				) {
					t.Fatalf("trashed dir=%q want fallback=%q", filepath.Dir(out.TrashedPath), wantFallback)
				}
			},
		},
		{
			name: "symlink_is_moved_as_symlink_best_effort",
			run: func(t *testing.T) {
				t.Helper()
				if runtime.GOOS == toolutil.GOOSWindows {
					t.Skip("symlink tests often require privileges on Windows")
				}
				tmp := t.TempDir()
				target := filepath.Join(tmp, "target.txt")
				writeFile(t, target, []byte("keep"))

				link := filepath.Join(tmp, "link.txt")
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("symlink not available: %v", err)
				}

				trash := filepath.Join(tmp, "trash")
				out, err := deleteFile(t.Context(), DeleteFileArgs{Path: link, TrashDir: trash}, "", nil)
				if err != nil {
					t.Fatalf("deleteFile: %v", err)
				}

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
					t.Fatalf("expected symlink in trash, got mode: %v", st.Mode())
				}
				gotTarget, err := os.Readlink(out.TrashedPath)
				if err != nil {
					t.Fatalf("Readlink trashed: %v", err)
				}
				if gotTarget != target {
					t.Fatalf("symlink target=%q want=%q", gotTarget, target)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t)
		})
	}
}
