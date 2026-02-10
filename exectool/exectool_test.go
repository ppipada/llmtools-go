package exectool

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestNewExecTool_InitPathPolicy_DefaultsAndOptions(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	td := t.TempDir()
	td2 := t.TempDir()

	cases := []struct {
		name           string
		opts           []ExecToolOption
		wantBaseEquals string // if non-empty, require exact match (SameFile)
		wantRootsLen   int
		wantErrSubstr  string
	}{
		{
			name:           "defaults_to_cwd_when_no_roots_and_no_base",
			opts:           nil,
			wantBaseEquals: cwd,
			wantRootsLen:   0,
		},
		{
			name:           "defaults_to_first_allowed_root_when_base_blank",
			opts:           []ExecToolOption{WithAllowedRoots([]string{td, td2})},
			wantBaseEquals: td,
			wantRootsLen:   2,
		},
		{
			name:           "explicit_base_is_used_when_valid",
			opts:           []ExecToolOption{WithWorkBaseDir(td)},
			wantBaseEquals: td,
			wantRootsLen:   0,
		},
		{
			name:          "invalid_allowed_root_errors",
			opts:          []ExecToolOption{WithAllowedRoots([]string{filepath.Join(td, "does-not-exist")})},
			wantErrSubstr: "invalid allowed root",
		},
		{
			name: "nil_option_is_ignored",
			opts: []ExecToolOption{nil, WithWorkBaseDir(td)},
			// "base" should become td.
			wantBaseEquals: td,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			et, err := NewExecTool(tc.opts...)
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
				t.Fatalf("NewExecTool: %v", err)
			}

			if tc.wantRootsLen != 0 && len(et.AllowedRoots()) != tc.wantRootsLen {
				t.Fatalf(
					"AllowedRoots len got=%d want=%d roots=%v",
					len(et.AllowedRoots()),
					tc.wantRootsLen,
					et.AllowedRoots(),
				)
			}
			if tc.wantRootsLen == 0 && len(et.AllowedRoots()) != 0 {
				t.Fatalf("expected no allowed roots, got %v", et.AllowedRoots())
			}

			if tc.wantBaseEquals != "" {
				mustSameDir(t, tc.wantBaseEquals, et.WorkBaseDir())
			}
		})
	}
}

func TestExecTool_AllowedRoots_ReturnsClone(t *testing.T) {
	td := t.TempDir()
	td2 := t.TempDir()

	cases := []struct {
		name string
		opts []ExecToolOption
	}{
		{name: "with_roots", opts: []ExecToolOption{WithAllowedRoots([]string{td, td2})}},
		{name: "no_roots", opts: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			et, err := NewExecTool(tc.opts...)
			if err != nil {
				t.Fatalf("NewExecTool: %v", err)
			}

			r1 := et.AllowedRoots()
			if len(r1) > 0 {
				r1[0] = "mutated"
			}
			r2 := et.AllowedRoots()

			// If roots existed, internal should not be mutated.
			if len(r2) > 0 && r2[0] == "mutated" {
				t.Fatalf("AllowedRoots returned slice appears to alias internal storage")
			}
		})
	}
}

func TestExecTool_SetAllowedRoots_StateUnchangedOnError(t *testing.T) {
	td := t.TempDir()
	outside := t.TempDir()

	et, err := NewExecTool(WithWorkBaseDir(outside))
	if err != nil {
		t.Fatalf("NewExecTool: %v", err)
	}

	beforeBase := et.WorkBaseDir()
	beforeRoots := et.AllowedRoots()

	cases := []struct {
		name          string
		roots         []string
		wantErrSubstr string
	}{
		{
			name:          "base_outside_new_roots_errors",
			roots:         []string{td},
			wantErrSubstr: "outside allowed roots",
		},
		{
			name:  "empty_roots_allows_all",
			roots: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := et.SetAllowedRoots(tc.roots)
			if tc.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error")
				}
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErrSubstr)) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErrSubstr)
				}
				// Ensure state unchanged.
				if et.WorkBaseDir() != beforeBase {
					t.Fatalf("workBaseDir changed on error: got %q want %q", et.WorkBaseDir(), beforeBase)
				}
				gotRoots := et.AllowedRoots()
				if len(gotRoots) != len(beforeRoots) {
					t.Fatalf("allowedRoots changed on error: got %v want %v", gotRoots, beforeRoots)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestExecTool_SetWorkBaseDir_EmptyDefaultsToRootOrCWD(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	td := t.TempDir()

	cases := []struct {
		name           string
		opts           []ExecToolOption
		setBase        string
		wantBaseEquals string
	}{
		{
			name:           "no_roots_empty_base_defaults_to_cwd",
			opts:           nil,
			setBase:        "   ",
			wantBaseEquals: cwd,
		},
		{
			name:           "with_roots_empty_base_defaults_to_first_root",
			opts:           []ExecToolOption{WithAllowedRoots([]string{td})},
			setBase:        "",
			wantBaseEquals: td,
		},
		{
			name:           "explicit_base_relative_is_allowed_and_canonicalized",
			opts:           nil,
			setBase:        ".",
			wantBaseEquals: cwd,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			et, err := NewExecTool(tc.opts...)
			if err != nil {
				t.Fatalf("NewExecTool: %v", err)
			}
			if err := et.SetWorkBaseDir(tc.setBase); err != nil {
				t.Fatalf("SetWorkBaseDir: %v", err)
			}
			mustSameDir(t, tc.wantBaseEquals, et.WorkBaseDir())
		})
	}
}

func TestExecTool_WithBlockedCommands_RejectsInvalid(t *testing.T) {
	cases := []struct {
		name          string
		cmds          []string
		wantErrSubstr string
	}{
		{
			name:          "rejects_whitespace_command_line",
			cmds:          []string{"rm -rf /"},
			wantErrSubstr: "whitespace",
		},
		{
			name:          "rejects_nul",
			cmds:          []string{"rm\x00"},
			wantErrSubstr: "nul",
		},
		{
			name: "allows_empty_entries_and_trims",
			cmds: []string{"", "   ", " ECHO "},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			et, err := NewExecTool()
			if err != nil {
				t.Fatalf("NewExecTool: %v", err)
			}
			opt := WithBlockedCommands(tc.cmds)
			err = opt(et)
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

			_, _, _, blocked, _ := et.snapshot()
			if _, ok := blocked["echo"]; len(tc.cmds) > 0 &&
				strings.Contains(strings.ToLower(strings.Join(tc.cmds, ",")), "echo") &&
				!ok {
				// On Windows, NormalizeBlockedCommand lowercases and basenames; "echo" should still appear.
				t.Fatalf("expected echo to be present in blocked list after WithBlockedCommands")
			}
		})
	}
}

func TestExecTool_ConcurrentGetSet_NoPanics(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrency stress in -short")
	}

	td := t.TempDir()
	td2 := t.TempDir()

	et, err := NewExecTool(WithAllowedRoots([]string{td, td2}))
	if err != nil {
		t.Fatalf("NewExecTool: %v", err)
	}

	cases := []struct {
		name     string
		workers  int
		iters    int
		setBases []string
	}{
		{
			name:     "concurrent_readers_writers",
			workers:  8,
			iters:    200,
			setBases: []string{"", td, td2, "   "},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var wg sync.WaitGroup
			wg.Add(tc.workers)

			for w := 0; w < tc.workers; w++ {
				go func() {
					defer wg.Done()
					for i := 0; i < tc.iters; i++ {
						// Mix getters.
						_ = et.WorkBaseDir()
						_ = et.AllowedRoots()

						// Mix setters (best-effort).
						base := tc.setBases[(w+i)%len(tc.setBases)]
						_ = et.SetWorkBaseDir(base)

						// Occasionally attempt to set roots (sometimes invalid).
						if i%25 == 0 {
							roots := [][]string{
								{td},
								{td2},
								{td, td2},
								{t.TempDir()}, // likely invalid w.r.t base -> should error; ignore
							}
							_ = et.SetAllowedRoots(roots[(w+i/25)%len(roots)])
						}
					}
				}()
			}
			wg.Wait()

			// Final invariant: work base dir must exist.
			if st, err := os.Stat(et.WorkBaseDir()); err != nil || !st.IsDir() {
				t.Fatalf("final WorkBaseDir invalid: %q statErr=%v", et.WorkBaseDir(), err)
			}
		})
	}

	// Additional small platform sanity checks.
	_ = runtime.GOOS
}
