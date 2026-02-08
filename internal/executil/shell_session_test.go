package executil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalWorkdir_AndEnsureDirExists(t *testing.T) {
	td := t.TempDir()
	id := newSessionID()
	s := &ShellSession{
		id:      id,
		workdir: "",
		env:     map[string]string{},
	}

	got, err := canonicalWorkdir(td)
	if err != nil {
		t.Fatalf("canonicalWorkdir error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected abs path, got: %q", got)
	}
	if err := ensureDirExists(got); err != nil {
		t.Fatalf("ensureDirExists error: %v", err)
	}

	// Not a directory.
	f := filepath.Join(td, "f")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, err = s.GetEffectiveWorkdir(f, nil)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected not-a-directory error, got: %v", err)
	}

	// NUL check.
	_, err = canonicalWorkdir("bad\x00path")
	if err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("expected NUL error, got: %v", err)
	}
}

func TestValidateEnvMap(t *testing.T) {
	if err := ValidateEnvMap(map[string]string{"OK": "1"}); err != nil {
		t.Fatalf("expected ok, got: %v", err)
	}

	if err := ValidateEnvMap(map[string]string{"": "1"}); err == nil {
		t.Fatalf("expected error for empty key")
	}

	if err := ValidateEnvMap(map[string]string{"A=B": "1"}); err == nil {
		t.Fatalf("expected error for key containing '='")
	}

	if err := ValidateEnvMap(map[string]string{"A\x00": "1"}); err == nil {
		t.Fatalf("expected error for NUL in key")
	}

	if err := ValidateEnvMap(map[string]string{"A": "1\x00"}); err == nil {
		t.Fatalf("expected error for NUL in value")
	}
}

func TestEffectiveEnv_OrderIsDeterministicNonDecreasingByCanonicalKey(t *testing.T) {
	// We can't control all of os.Environ(), but we can assert monotonic ordering.
	t.Setenv("ZZZ_TEST_ENV", "1")
	t.Setenv("AAA_TEST_ENV", "2")
	id := newSessionID()
	s := &ShellSession{
		id:      id,
		workdir: "",
		env:     map[string]string{},
	}

	env, err := s.GetEffectiveEnv(map[string]string{"MMM_TEST_ENV": "3"})
	if err != nil {
		t.Fatalf("effectiveEnv error: %v", err)
	}
	var prev string
	for i, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			t.Fatalf("bad env entry %q", kv)
		}
		ck := canonicalEnvKey(k)
		if i > 0 && ck < prev {
			t.Fatalf("env not sorted at %d: %q < %q", i, ck, prev)
		}
		prev = ck
	}
}

func TestCappedWriter_TruncatesAndCounts(t *testing.T) {
	w := newCappedWriter(1024)

	_, _ = w.Write([]byte(strings.Repeat("a", 600)))
	_, _ = w.Write([]byte(strings.Repeat("b", 600))) // total 1200 > 1024

	if w.TotalBytes() != 1200 {
		t.Fatalf("expected totalBytes 1200, got %d", w.TotalBytes())
	}
	if !w.Truncated() {
		t.Fatalf("expected truncated=true")
	}
	if got := len(w.Bytes()); got != 1024 {
		t.Fatalf("expected stored bytes len 1024, got %d", got)
	}
	b := w.Bytes()
	if len(b) == 0 || b[len(b)-1] != 'b' {
		t.Fatalf("expected tail capture ending with 'b'")
	}
}

func TestSafeUTF8_ReplacesInvalid(t *testing.T) {
	s := safeUTF8([]byte{0xff, 0xfe, 'a'})
	if !strings.Contains(s, "\uFFFD") {
		t.Fatalf("expected replacement char in %q", s)
	}
	if !strings.Contains(s, "a") {
		t.Fatalf("expected 'a' preserved in %q", s)
	}
}
