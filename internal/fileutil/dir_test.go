package fileutil

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestListDirectory(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "b.log", 1)
	mustWriteFile(t, root, "a.txt", 1)
	if err := os.Mkdir(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatalf("failed to mkdir subdir: %v", err)
	}

	tests := []struct {
		name           string
		setup          func(t *testing.T)
		dir            string
		pattern        string
		want           []string
		wantErrIs      error
		wantIsNotExist bool
	}{
		{
			name:    "no pattern returns all entries (already sorted by function)",
			dir:     root,
			pattern: "",
			want:    []string{"a.txt", "b.log", "subdir"},
		},
		{
			name:    "pattern filters entries",
			dir:     root,
			pattern: "*.txt",
			want:    []string{"a.txt"},
		},
		{
			name:      "invalid glob returns filepath.ErrBadPattern",
			dir:       root,
			pattern:   "[",
			wantErrIs: filepath.ErrBadPattern,
		},
		{
			name:           "non-existent directory returns not-exist",
			dir:            filepath.Join(root, "nope"),
			pattern:        "",
			wantIsNotExist: true,
		},
		{
			name:      "invalid path returns ErrInvalidPath",
			dir:       "a\x00b",
			pattern:   "",
			wantErrIs: ErrInvalidPath,
		},
		{
			name: "empty dir means dot (CWD)",
			setup: func(t *testing.T) {
				t.Helper()
				// Ensure deterministic: chdir into our temp dir for this subtest.
				t.Chdir(root)
			},
			dir:     "",
			pattern: "",
			want:    []string{"a.txt", "b.log", "subdir"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			got, err := ListDirectory(tc.dir, tc.pattern)

			if tc.wantErrIs != nil || tc.wantIsNotExist {
				if err == nil {
					t.Fatalf("expected error, got nil (got=%v)", got)
				}
				if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("error=%v; want errors.Is(_, %v)=true", err, tc.wantErrIs)
				}
				if tc.wantIsNotExist && !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("error=%v; want os.IsNotExist=true", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%#v want=%#v", got, tc.want)
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
				if tc.errIsNotExist && !errors.Is(err, fs.ErrNotExist) {
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

func TestCanonicalWorkdir_AndEnsureDirExists(t *testing.T) {
	td := t.TempDir()

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
	_, err = GetEffectiveWorkDir(f, nil)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected not-a-directory error, got: %v", err)
	}

	// NUL check.
	_, err = canonicalWorkdir("bad\x00path")
	if err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("expected NUL error, got: %v", err)
	}
}

func TestListDirectoryNormalized_SortsAndFiltersAndErrors(t *testing.T) {
	td := t.TempDir()

	// Create entries.
	if err := os.WriteFile(filepath.Join(td, "b.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(td, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Mkdir(filepath.Join(td, "dir1"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	cases := []struct {
		name          string
		dir           string
		pattern       string
		want          []string
		wantErrSubstr string
	}{
		{name: "empty_dir_invalid", dir: "   ", wantErrSubstr: "invalid"},
		{name: "nonexistent_dir_errors", dir: filepath.Join(td, "nope"), wantErrSubstr: "read dir error"},
		{name: "no_pattern_lists_all_sorted", dir: td, pattern: "", want: []string{"a.txt", "b.txt", "dir1"}},
		{name: "pattern_filters", dir: td, pattern: "*.txt", want: []string{"a.txt", "b.txt"}},
		{name: "invalid_glob_errors", dir: td, pattern: "[", wantErrSubstr: "syntax"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ListDirectoryNormalized(tc.dir, tc.pattern)
			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error")
				}
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErrSubstr)) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Fatalf("got %#v want %#v", got, tc.want)
			}
		})
	}

	_ = runtime.GOOS
}

func TestCanonicalizeAllowedRoots_ValidatesExistenceAndDirectories(t *testing.T) {
	td := t.TempDir()
	f := filepath.Join(td, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cases := []struct {
		name          string
		roots         []string
		wantLen       int
		wantErrSubstr string
	}{
		{name: "ignores_empty", roots: []string{"", "   ", td}, wantLen: 1},
		{name: "nonexistent_errors", roots: []string{filepath.Join(td, "nope")}, wantErrSubstr: "no such dir"},
		{name: "file_is_not_dir_errors", roots: []string{f}, wantErrSubstr: "not a directory"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalizeAllowedRoots(tc.roots)
			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error")
				}
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErrSubstr)) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.wantLen {
				t.Fatalf("len got=%d want=%d roots=%v", len(got), tc.wantLen, got)
			}
			for _, r := range got {
				if !filepath.IsAbs(r) {
					t.Fatalf("expected abs root, got %q", r)
				}
			}
		})
	}
}

func TestGetEffectiveWorkDir_EnforcesRootsAndExistence(t *testing.T) {
	td := t.TempDir()
	td2 := t.TempDir()

	f := filepath.Join(td, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cases := []struct {
		name          string
		input         string
		roots         []string
		wantErrSubstr string
		wantSameAs    string
	}{
		{name: "empty_errors", input: "   ", roots: nil, wantErrSubstr: "empty workdir"},
		{name: "nonexistent_errors", input: filepath.Join(td, "nope"), roots: nil, wantErrSubstr: "no such dir"},
		{name: "file_errors", input: f, roots: nil, wantErrSubstr: "not a directory"},
		{name: "outside_roots_errors", input: td2, roots: []string{td}, wantErrSubstr: "outside allowed roots"},
		{name: "within_roots_ok", input: td, roots: []string{td}, wantSameAs: td},
		{name: "no_roots_allows_any", input: td2, roots: nil, wantSameAs: td2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roots := tc.roots
			var err error
			if len(tc.roots) != 0 {
				roots, err = CanonicalizeAllowedRoots(roots)
				if err != nil {
					t.Fatalf("could not CanonicalizeAllowedRoots")
				}
			}
			got, err := GetEffectiveWorkDir(tc.input, roots)
			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error")
				}
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErrSubstr)) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantSameAs != "" {
				mustSameDir(t, tc.wantSameAs, got)
			}
		})
	}
}

func TestEnsurePathWithinAllowedRoots(t *testing.T) {
	td := t.TempDir()
	td2 := t.TempDir()

	cases := []struct {
		name          string
		p             string
		roots         []string
		wantErrSubstr string
	}{
		{name: "no_roots_allows", p: td2, roots: nil},
		{name: "within_root_ok", p: td, roots: []string{td}},
		{name: "outside_root_errors", p: td2, roots: []string{td}, wantErrSubstr: "outside allowed roots"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := EnsurePathWithinAllowedRoots(tc.p, tc.roots)
			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error")
				}
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErrSubstr)) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestIsPathWithinRoot(t *testing.T) {
	td := t.TempDir()
	sub := filepath.Join(td, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	cases := []struct {
		name          string
		root          string
		p             string
		want          bool
		wantErrSubstr string
	}{
		{name: "same_dir_true", root: td, p: td, want: true},
		{name: "child_true", root: td, p: sub, want: true},
		{name: "outside_false", root: sub, p: td, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsPathWithinRoot(tc.root, tc.p)
			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func mustSameDir(t *testing.T, a, b string) {
	t.Helper()
	sa, err := os.Stat(a)
	if err != nil {
		t.Fatalf("stat(%q): %v", a, err)
	}
	sb, err := os.Stat(b)
	if err != nil {
		t.Fatalf("stat(%q): %v", b, err)
	}
	if !os.SameFile(sa, sb) {
		t.Fatalf("expected same dir:\n  a=%q\n  b=%q", a, b)
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
