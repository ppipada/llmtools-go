package fstool

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

func TestWriteFile(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	t.Run("context_canceled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := WriteFile(ctx, WriteFileArgs{
			Path:    filepath.Join(tmp, "a.txt"),
			Content: "x",
		})
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("rejects_relative_path", func(t *testing.T) {
		t.Parallel()
		_, err := WriteFile(t.Context(), WriteFileArgs{
			Path:    "relative.txt",
			Content: "hello",
		})
		if err == nil || !strings.Contains(err.Error(), "absolute") {
			t.Fatalf("expected absolute path error, got: %v", err)
		}
	})

	t.Run("writes_text_default_encoding", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(tmp, "text.txt")

		out, err := WriteFile(t.Context(), WriteFileArgs{
			Path:    p,
			Content: "hello",
		})
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if out.Path != p || out.BytesWritten != 5 {
			t.Fatalf("unexpected out: %+v", out)
		}

		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(b) != "hello" {
			t.Fatalf("content mismatch: %q", string(b))
		}

		if runtime.GOOS != toolutil.GOOSWindows {
			st, _ := os.Stat(p)
			if st.Mode().Perm() != 0o600 {
				t.Fatalf("expected perms 0600, got %o", st.Mode().Perm())
			}
		}
	})

	t.Run("overwrite_false_errors_if_exists", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(tmp, "exists.txt")

		if _, err := WriteFile(t.Context(), WriteFileArgs{Path: p, Content: "a"}); err != nil {
			t.Fatalf("seed write: %v", err)
		}
		_, err := WriteFile(t.Context(), WriteFileArgs{Path: p, Content: "b", Overwrite: false})
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("overwrite_true_replaces_content", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(tmp, "ow.txt")

		if _, err := WriteFile(t.Context(), WriteFileArgs{Path: p, Content: "a"}); err != nil {
			t.Fatalf("seed write: %v", err)
		}
		_, err := WriteFile(t.Context(), WriteFileArgs{Path: p, Content: "bb", Overwrite: true})
		if err != nil {
			t.Fatalf("overwrite: %v", err)
		}

		b, _ := os.ReadFile(p)
		if string(b) != "bb" {
			t.Fatalf("content mismatch: %q", string(b))
		}
	})

	t.Run("writes_binary_base64", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(tmp, "bin.dat")
		raw := []byte{0x00, 0x01, 0x02, 0xff}
		b64 := base64.StdEncoding.EncodeToString(raw)

		out, err := WriteFile(t.Context(), WriteFileArgs{
			Path:     p,
			Encoding: "binary",
			Content:  b64,
		})
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if out.BytesWritten != int64(len(raw)) {
			t.Fatalf("unexpected out: %+v", out)
		}

		got, _ := os.ReadFile(p)
		if !bytes.Equal(got, raw) {
			t.Fatalf("binary mismatch: got=%v want=%v", got, raw)
		}
	})

	t.Run("invalid_base64_errors", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(tmp, "badb64.dat")

		_, err := WriteFile(t.Context(), WriteFileArgs{
			Path:     p,
			Encoding: "binary",
			Content:  "!!!",
		})
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("createParents_false_missing_parent_errors", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(tmp, "nope", "a.txt")

		_, err := WriteFile(t.Context(), WriteFileArgs{
			Path:          p,
			Content:       "x",
			CreateParents: false,
		})
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("createParents_true_creates_up_to_8", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(tmp, "a", "b", "c", "d.txt")

		_, err := WriteFile(t.Context(), WriteFileArgs{
			Path:          p,
			Content:       "ok",
			CreateParents: true,
		})
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	})

	t.Run("createParents_true_depth_limit_exceeded", func(t *testing.T) {
		t.Parallel()
		// 9 missing directories under tmp.
		p := filepath.Join(tmp, "1", "2", "3", "4", "5", "6", "7", "8", "9", "f.txt")

		_, err := WriteFile(t.Context(), WriteFileArgs{
			Path:          p,
			Content:       "x",
			CreateParents: true,
		})
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("refuses_directory_target", func(t *testing.T) {
		t.Parallel()
		_, err := WriteFile(t.Context(), WriteFileArgs{
			Path:    tmp,
			Content: "x",
		})
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("refuses_invalid_utf8_text", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(tmp, "badutf8.txt")
		s := string([]byte{0xff, 0xfe})

		_, err := WriteFile(t.Context(), WriteFileArgs{
			Path:     p,
			Encoding: "text",
			Content:  s,
		})
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("symlink_component_rejected", func(t *testing.T) {
		t.Parallel()

		realDir := filepath.Join(tmp, "real")
		if err := os.MkdirAll(realDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		linkDir := filepath.Join(tmp, "link")

		if err := os.Symlink(realDir, linkDir); err != nil {
			// Windows often needs admin/dev-mode.
			t.Skipf("symlink not supported: %v", err)
		}

		p := filepath.Join(linkDir, "child.txt")
		_, err := WriteFile(t.Context(), WriteFileArgs{
			Path:          p,
			Content:       "x",
			CreateParents: false,
		})
		if err == nil {
			t.Fatalf("expected error")
		}
	})
}
