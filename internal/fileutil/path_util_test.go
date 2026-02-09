package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

func TestNormalizeAbsPath(t *testing.T) {
	absBase := filepath.Join(t.TempDir(), "x", "..", "y")
	tests := []struct {
		name      string
		in        string
		want      string
		wantErrIs error
	}{
		{
			name: "absolute path is cleaned",
			in:   "  " + absBase + "  ",
			want: filepath.Clean(absBase),
		},
		{
			name:      "relative path rejected",
			in:        "a/../b",
			wantErrIs: errPathMustBeAbsolute,
		},
		{
			name:      "empty path rejected",
			in:        "",
			wantErrIs: ErrInvalidPath,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeAbsPath(tc.in)
			if tc.wantErrIs != nil {
				if err == nil {
					t.Fatalf("expected error, got nil (got=%q)", got)
				}
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("error=%v; want errors.Is(_, %v)=true", err, tc.wantErrIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	sep := string(os.PathSeparator)
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", true},
		{"   ", "", true},
		{"a" + sep + ".." + sep + "b", filepath.Clean("a" + sep + ".." + sep + "b"), false},
		{"  .  ", ".", false},
		{"a\u0000b", "", true},
	}

	for _, tc := range tests {
		t.Run("in="+tc.in, func(t *testing.T) {
			got, err := NormalizePath(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (got=%q)", got)
				}
				if !errors.Is(err, ErrInvalidPath) {
					t.Fatalf("expected ErrInvalidPath, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("NormalizePath(%q)=%q want=%q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEnsureDirNoSymlink(t *testing.T) {
	root := t.TempDir()

	// Pre-create a dir to test "existing doesn't count".
	if err := os.Mkdir(filepath.Join(root, "already"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tests := []struct {
		name        string
		dir         string
		maxNewDirs  int
		setup       func(t *testing.T)
		wantCreated int
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty path invalid",
			dir:         "",
			maxNewDirs:  0,
			wantCreated: 0,
			wantErr:     true,
		},
		{
			name:        "dot does nothing",
			dir:         ".",
			maxNewDirs:  0,
			wantCreated: 0,
		},
		{
			name:        "creates nested directories",
			dir:         filepath.Join(root, "a", "b", "c"),
			maxNewDirs:  0,
			wantCreated: 3,
		},
		{
			name:        "existing parent does not count as created",
			dir:         filepath.Join(root, "already", "child"),
			maxNewDirs:  0,
			wantCreated: 1,
		},
		{
			name:        "maxNewDirs enforced",
			dir:         filepath.Join(root, "x", "y", "z"),
			maxNewDirs:  2,
			wantCreated: 2,
			wantErr:     true,
			errContains: "too many parent directories",
		},
		{
			name: "component exists as file => error",
			dir:  filepath.Join(root, "filecomp", "child"),
			setup: func(t *testing.T) {
				t.Helper()
				mustWriteBytes(t, filepath.Join(root, "filecomp"), []byte("x"))
			},
			wantCreated: 0,
			wantErr:     true,
			errContains: "not a directory",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			created, err := EnsureDirNoSymlink(tc.dir, tc.maxNewDirs)
			if created != tc.wantCreated {
				t.Fatalf("created=%d want=%d (err=%v)", created, tc.wantCreated, err)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if st, statErr := os.Stat(tc.dir); statErr != nil || !st.IsDir() {
				t.Fatalf("expected dir to exist: statErr=%v isDir=%v", statErr, st != nil && st.IsDir())
			}
		})
	}

	t.Run("refuses symlink component (if supported)", func(t *testing.T) {
		if runtime.GOOS == toolutil.GOOSWindows {
			t.Skip("symlink tests skipped on Windows")
		}
		real1 := filepath.Join(root, "real1")
		if err := os.Mkdir(real1, 0o755); err != nil {
			t.Fatalf("mkdir real: %v", err)
		}
		link := filepath.Join(root, "link")
		mustSymlinkOrSkip(t, real1, link)

		created, err := EnsureDirNoSymlink(filepath.Join(link, "child"), 0)
		if err == nil {
			t.Fatalf("expected error, got nil (created=%d)", created)
		}
		if !strings.Contains(err.Error(), "symlink path component") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestVerifyDirNoSymlink_AllowsDarwinSystemSymlinks(t *testing.T) {
	if runtime.GOOS != toolutil.GOOSDarwin {
		t.Skip("darwin-only")
	}

	if err := VerifyDirNoSymlink(t.TempDir()); err != nil {
		t.Fatalf("VerifyDirNoSymlink(os.TempDir()) unexpected error: %v", err)
	}
}

func TestVerifyDirNoSymlink(t *testing.T) {
	root := t.TempDir()

	okDir := filepath.Join(root, "ok", "nested")
	if err := os.MkdirAll(okDir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}

	tests := []struct {
		name        string
		dir         string
		setup       func(t *testing.T)
		wantErr     bool
		errContains string
	}{
		{
			name:    "dot is ok",
			dir:     ".",
			wantErr: false,
		},
		{
			name:    "existing nested dir ok",
			dir:     okDir,
			wantErr: false,
		},
		{
			name:    "missing dir errors",
			dir:     filepath.Join(root, "nope"),
			wantErr: true,
		},
		{
			name: "file component errors",
			dir:  filepath.Join(root, "file", "child"),
			setup: func(t *testing.T) {
				t.Helper()
				mustWriteBytes(t, filepath.Join(root, "file"), []byte("x"))
			},
			wantErr:     true,
			errContains: "not a directory",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t)
			}
			err := VerifyDirNoSymlink(tc.dir)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}

	t.Run("symlink component errors (if supported)", func(t *testing.T) {
		if runtime.GOOS == toolutil.GOOSWindows {
			t.Skip("symlink tests skipped on Windows")
		}

		real2 := filepath.Join(root, "real2")
		if err := os.Mkdir(real2, 0o755); err != nil {
			t.Fatalf("mkdir real: %v", err)
		}
		link := filepath.Join(root, "link2")
		mustSymlinkOrSkip(t, real2, link)

		err := VerifyDirNoSymlink(filepath.Join(link, "child"))
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "symlink path component") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestUniquePathInDir(t *testing.T) {
	root := t.TempDir()

	t.Run("returns base name when available", func(t *testing.T) {
		p, err := UniquePathInDir(root, "a.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p != filepath.Join(root, "a.txt") {
			t.Fatalf("got=%q want=%q", p, filepath.Join(root, "a.txt"))
		}
	})

	t.Run("generates unique name when collision", func(t *testing.T) {
		// Create colliding name.
		mustWriteBytes(t, filepath.Join(root, "a.txt"), []byte("x"))

		p, err := UniquePathInDir(root, "a.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if filepath.Dir(p) != root {
			t.Fatalf("expected same dir %q, got %q", root, filepath.Dir(p))
		}
		if p == filepath.Join(root, "a.txt") {
			t.Fatalf("expected unique path, got original: %q", p)
		}
		if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected candidate not to exist yet, stat err=%v", err)
		}

		// Validate shape: a.<timestamp>.<hex>.txt.
		base := filepath.Base(p)
		re := regexp.MustCompile(`^a\.\d{8}T\d{6}\.\d{9}Z\.[0-9a-f]{12}\.txt$`)
		if !re.MatchString(base) {
			t.Fatalf("unexpected unique filename shape: %q", base)
		}
	})
}

func TestNormalizePath_AndNormalizeAbsPath(t *testing.T) {
	td := t.TempDir()
	rel := "."
	abs := td

	cases := []struct {
		name          string
		fn            string // "norm" | "abs"
		in            string
		wantAbs       bool
		wantErrIsInv  bool
		wantErrSubstr string
	}{
		{name: "normalize_trims_and_cleans", fn: "norm", in: "  " + rel + "  ", wantAbs: false},
		{name: "normalize_rejects_empty", fn: "norm", in: "   ", wantErrIsInv: true},
		{name: "normalize_rejects_nul", fn: "norm", in: "a\x00b", wantErrIsInv: true},
		{name: "normalize_abs_requires_abs", fn: "abs", in: rel, wantErrSubstr: "absolute"},
		{name: "normalize_abs_accepts_abs", fn: "abs", in: abs, wantAbs: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			var err error
			switch tc.fn {
			case "norm":
				got, err = NormalizePath(tc.in)
			case "abs":
				got, err = NormalizeAbsPath(tc.in)
			default:
				t.Fatalf("bad fn")
			}

			if tc.wantErrIsInv || tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error")
				}
				if tc.wantErrIsInv && !errors.Is(err, ErrInvalidPath) {
					t.Fatalf("expected ErrInvalidPath, got %v", err)
				}
				if tc.wantErrSubstr != "" &&
					!strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErrSubstr)) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSubstr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantAbs && !filepath.IsAbs(got) {
				t.Fatalf("expected abs, got %q", got)
			}
		})
	}
}

func TestInitPathPolicy_DefaultsAndRoots(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	td := t.TempDir()
	td2 := t.TempDir()

	cases := []struct {
		name          string
		base          string
		roots         []string
		wantBaseSame  string
		wantRootsLen  int
		wantErrSubstr string
	}{
		{name: "no_roots_base_blank_defaults_cwd", base: "", roots: nil, wantBaseSame: cwd, wantRootsLen: 0},
		{
			name:         "with_roots_base_blank_defaults_first_root",
			base:         "",
			roots:        []string{td, td2},
			wantBaseSame: td,
			wantRootsLen: 2,
		},
		{name: "explicit_base_must_exist", base: filepath.Join(td, "nope"), roots: nil, wantErrSubstr: "no such dir"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotBase, gotRoots, err := InitPathPolicy(tc.base, tc.roots)
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
			if tc.wantRootsLen != len(gotRoots) {
				t.Fatalf("roots len got=%d want=%d roots=%v", len(gotRoots), tc.wantRootsLen, gotRoots)
			}
			if tc.wantBaseSame != "" {
				mustSameDir(t, tc.wantBaseSame, gotBase)
			}
		})
	}
}

func TestResolvePath_RelativeAbsoluteAllowedRootsAndDefaults(t *testing.T) {
	td := t.TempDir()
	sub := filepath.Join(td, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	outside := t.TempDir()

	cases := []struct {
		name          string
		base          string
		roots         []string
		input         string
		def           string
		wantSameAs    string
		wantErrSubstr string
	}{
		{name: "empty_input_uses_default", base: td, roots: []string{td}, input: "", def: sub, wantSameAs: sub},
		{name: "relative_resolves_against_base", base: td, roots: []string{td}, input: "sub", def: "", wantSameAs: sub},
		{name: "absolute_within_roots_ok", base: td, roots: []string{td}, input: sub, def: "", wantSameAs: sub},
		{
			name:          "absolute_outside_roots_errors",
			base:          td,
			roots:         []string{td},
			input:         outside,
			def:           "",
			wantErrSubstr: "outside allowed roots",
		},
		{name: "blank_base_uses_cwd_for_relative", base: "", roots: nil, input: ".", def: "", wantSameAs: mustGetwd(t)},
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

			got, err := ResolvePath(tc.base, roots, tc.input, tc.def)
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

func TestResolvePath_WindowsDriveRelativeRejected(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantErr     bool
		wantSubstr  string
		windowsOnly bool
	}{
		{
			name:        "drive_relative_rejected",
			input:       `C:foo`,
			wantErr:     true,
			wantSubstr:  "drive-relative",
			windowsOnly: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.windowsOnly && runtime.GOOS != toolutil.GOOSWindows {
				t.Skip("windows-only")
			}
			_, err := ResolvePath("", nil, tc.input, "")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				if tc.wantSubstr != "" &&
					!strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantSubstr)) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestEnsureDirNoSymlink_CreatesAndLimits(t *testing.T) {
	td := t.TempDir()
	target := filepath.Join(td, "a", "b", "c")

	cases := []struct {
		name          string
		dir           string
		maxNewDirs    int
		wantCreated   int
		wantErrSubstr string
	}{
		{name: "dot_noop", dir: ".", maxNewDirs: 0, wantCreated: 0},
		{name: "creates_all_missing_unlimited", dir: target, maxNewDirs: 0, wantCreated: 3},
		{
			name:          "limit_too_small_errors",
			dir:           filepath.Join(td, "x", "y", "z"),
			maxNewDirs:    2,
			wantErrSubstr: "too many parent directories",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			created, err := EnsureDirNoSymlink(tc.dir, tc.maxNewDirs)
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
			if created != tc.wantCreated {
				t.Fatalf("created got %d want %d", created, tc.wantCreated)
			}
			if tc.dir != "." {
				if st, serr := os.Stat(tc.dir); serr != nil || !st.IsDir() {
					t.Fatalf("expected dir exists: statErr=%v", serr)
				}
			}
		})
	}
}

func TestVerifyDirNoSymlink_RejectsSymlinkComponent(t *testing.T) {
	td := t.TempDir()
	realDir := filepath.Join(td, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	link := filepath.Join(td, "link")

	// Create a symlink; skip if not supported.
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlink not supported/allowed on this platform: %v", err)
	}

	cases := []struct {
		name          string
		dir           string
		wantErrSubstr string
	}{
		{name: "symlink_component_rejected", dir: filepath.Join(link, "child"), wantErrSubstr: "symlink"},
	}

	// Make child dir under real dir so lstat traversal sees the symlink component first.
	if err := os.MkdirAll(filepath.Join(realDir, "child"), 0o755); err != nil {
		t.Fatalf("MkdirAll child: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyDirNoSymlink(tc.dir)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErrSubstr)) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSubstr)
			}
		})
	}
}

func TestUniquePathInDir_BasicAndCollision(t *testing.T) {
	td := t.TempDir()

	cases := []struct {
		name string
		base string
		pre  bool // pre-create base to force unique name generation
	}{
		{name: "no_collision_returns_plain", base: "x.txt", pre: false},
		{name: "collision_generates_unique", base: "x.txt", pre: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			basePath := filepath.Join(td, tc.base)
			if tc.pre {
				if err := os.WriteFile(basePath, []byte("x"), 0o600); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}

			got, err := UniquePathInDir(td, tc.base)
			if err != nil {
				t.Fatalf("UniquePathInDir: %v", err)
			}
			if filepath.Dir(got) != td {
				t.Fatalf("expected same dir %q, got %q", td, filepath.Dir(got))
			}
			if !strings.HasSuffix(got, filepath.Ext(tc.base)) {
				t.Fatalf("expected same ext %q got %q", filepath.Ext(tc.base), got)
			}

			if !tc.pre {
				if got != basePath {
					t.Fatalf("expected %q got %q", basePath, got)
				}
			} else {
				if got == basePath {
					t.Fatalf("expected unique path different from base")
				}
				if _, statErr := os.Stat(got); !errors.Is(statErr, os.ErrNotExist) {
					// It should not exist yet.
					t.Fatalf("expected unique path not to exist yet; statErr=%v", statErr)
				}
			}
		})
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	return cwd
}

func mustSymlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		// Often EPERM in CI environments.
		t.Skipf("symlink not supported/allowed: %v", err)
	}
}
