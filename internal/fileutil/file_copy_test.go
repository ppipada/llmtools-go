package fileutil

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

func TestCopyFileCtx(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	content := []byte("hello copy\n")
	mustWriteBytes(t, src, content)

	written, err := CopyFileCtx(t.Context(), src, dst, 0o640)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != int64(len(content)) {
		t.Fatalf("written=%d want=%d", written, len(content))
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("dst content=%q want=%q", string(got), string(content))
	}

	if runtime.GOOS != toolutil.GOOSWindows {
		st, err := os.Stat(dst)
		if err != nil {
			t.Fatalf("stat dst: %v", err)
		}
		if st.Mode().Perm() != 0o640 {
			t.Fatalf("perm=%o want=%o", st.Mode().Perm(), 0o640)
		}
	}
}

func TestCopyFileCtx_Errors(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "src.txt")
	mustWriteBytes(t, src, []byte("hello"))

	tests := []struct {
		name     string
		ctx      context.Context
		dstSetup func(t *testing.T, dst string)
		dst      string
		wantErr  bool
	}{
		{
			name:    "canceled before start",
			ctx:     canceledContext(t.Context()),
			dst:     filepath.Join(dir, "dst1.txt"),
			wantErr: true,
		},
		{
			name: "dst exists => O_EXCL error",
			ctx:  t.Context(),
			dstSetup: func(t *testing.T, dst string) {
				t.Helper()
				mustWriteBytes(t, dst, []byte("already"))
			},
			dst:     filepath.Join(dir, "dst2.txt"),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.dstSetup != nil {
				tc.dstSetup(t, tc.dst)
			}
			_, err := CopyFileCtx(tc.ctx, src, tc.dst, 0o600)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
		})
	}
}

func TestCopyFileToExistingCtx(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")

	mustWriteBytes(t, src, []byte("NEW"))
	mustWriteBytes(t, dst, []byte("OLD-TO-BE-TRUNCATED"))

	written, err := CopyFileToExistingCtx(t.Context(), src, dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != 3 {
		t.Fatalf("written=%d want=3", written)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != "NEW" {
		t.Fatalf("dst content=%q want=%q", string(got), "NEW")
	}
}

func TestCopyFileToExistingCtx_Errors(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "src.txt")
	mustWriteBytes(t, src, []byte("hello"))

	tests := []struct {
		name            string
		ctx             context.Context
		dst             string
		dstSetup        func(t *testing.T, dst string)
		wantErrContains string
	}{
		{
			name: "canceled before start",
			ctx:  canceledContext(t.Context()),
			dst:  filepath.Join(dir, "dst1.txt"),
		},
		{
			name: "dst missing => error",
			ctx:  t.Context(),
			dst:  filepath.Join(dir, "missing.txt"),
		},
		{
			name: "dst is directory => not regular file",
			ctx:  t.Context(),
			dst:  filepath.Join(dir, "adir"),
			dstSetup: func(t *testing.T, dst string) {
				t.Helper()
				if err := os.Mkdir(dst, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			},
			wantErrContains: "not a regular file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.dstSetup != nil {
				tc.dstSetup(t, tc.dst)
			}
			_, err := CopyFileToExistingCtx(tc.ctx, src, tc.dst)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if tc.wantErrContains != "" && !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrContains)
			}
		})
	}
}

func canceledContext(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	return ctx
}
