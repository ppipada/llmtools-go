package fileutil

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

func TestListDirectory_BasicAndPattern(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "a.txt", 1)
	mustWriteFile(t, root, "b.log", 1)
	if err := os.Mkdir(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatalf("failed to mkdir subdir: %v", err)
	}

	type testCase struct {
		name    string
		dir     string
		pattern string
		want    []string
	}

	tests := []testCase{
		{
			name:    "NoPattern_AllEntries",
			dir:     root,
			pattern: "",
			want:    []string{"a.txt", "b.log", "subdir"},
		},
		{
			name:    "Pattern_TxtOnly",
			dir:     root,
			pattern: "*.txt",
			want:    []string{"a.txt"},
		},
		{
			name:    "Pattern_Invalid_NoError_EmptyResult",
			dir:     root,
			pattern: "[",        // invalid glob; implementation ignores Match error
			want:    []string{}, // invalid pattern yields no matches, no error
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ListDirectory(tc.dir, tc.pattern)
			if err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			sort.Strings(got)
			sort.Strings(tc.want)
			if len(got) != len(tc.want) {
				t.Fatalf("expected %d entries, got %d (%v)", len(tc.want), len(got), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("expected got[%d]=%q, got %q", i, tc.want[i], got[i])
				}
			}
		})
	}
}

func TestListDirectory_DefaultPathDot(t *testing.T) {
	// Not parallel: this test changes the working directory globally.
	tmp := t.TempDir()
	t.Chdir(tmp)

	// Create some entries in the current directory.
	mustWriteFile(t, tmp, "x.txt", 1)
	if err := os.Mkdir(filepath.Join(tmp, "dir"), 0o755); err != nil {
		t.Fatalf("failed to mkdir: %v", err)
	}

	got, err := ListDirectory("", "")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// We only check that the created entries are present; there might be others.
	wantSet := map[string]bool{"x.txt": true, "dir": true}
	for name := range wantSet {
		found := slices.Contains(got, name)
		if !found {
			t.Errorf("expected to find %q in ListDirectory output, got %v", name, got)
		}
	}
}

func mustWriteFile(t *testing.T, dir, name string, size int) string {
	t.Helper()
	full := filepath.Join(dir, name)
	data := bytes.Repeat([]byte("x"), size)
	if err := os.WriteFile(full, data, 0o600); err != nil {
		t.Fatalf("failed to write file %q: %v", full, err)
	}
	return full
}
