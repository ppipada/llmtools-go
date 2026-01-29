package fileutil

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
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

func TestListDirectory_Additional(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "a.txt", 1)
	mustWriteFile(t, root, "b.log", 1)
	if err := os.Mkdir(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	tests := []struct {
		name          string
		dir           string
		pattern       string
		want          []string
		wantErr       bool
		errContains   string
		errIsNotExist bool
	}{
		{
			name:          "nonexistent dir returns error",
			dir:           filepath.Join(root, "nope"),
			pattern:       "",
			wantErr:       true,
			errIsNotExist: true,
		},
		{
			name:        "invalid glob pattern returns error",
			dir:         root,
			pattern:     "[",
			wantErr:     true,
			errContains: "syntax error in pattern",
		},
		{
			name:    "pattern matches none returns empty slice",
			dir:     root,
			pattern: "*.nope",
			want:    []string{},
		},
		{
			name:    "pattern can match directories too",
			dir:     root,
			pattern: "sub*",
			want:    []string{"subdir"},
		},
		{
			name:    "basic no pattern includes files and dirs",
			dir:     root,
			pattern: "",
			want:    []string{"a.txt", "b.log", "subdir"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ListDirectory(tc.dir, tc.pattern)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (got=%v)", got)
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errContains)
				}
				if tc.errIsNotExist && !os.IsNotExist(err) {
					t.Fatalf("expected not-exist error, got: %v", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			sort.Strings(got)
			sort.Strings(tc.want)
			if len(got) != len(tc.want) {
				t.Fatalf("len(got)=%d want=%d got=%v want=%v", len(got), len(tc.want), got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got[%d]=%q want=%q got=%v", i, got[i], tc.want[i], got)
				}
			}
		})
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
