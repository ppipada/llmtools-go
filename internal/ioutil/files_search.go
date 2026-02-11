package ioutil

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"unicode/utf8"

	"github.com/flexigpt/llmtools-go/internal/fspolicy"
)

var errSearchLimitReached = errors.New("search limit reached")

// SearchFiles walks root (default ".") recursively and returns up to maxResults files
// whose *path* or UTF-8 text content match the regexp pattern.
// If maxResults <= 0, it is treated as "no limit".
//
// FSPolicy enforcement:
//   - root is resolved via policy (base dir + allowed roots)
//   - if policy.BlockSymlinks == true: symlink entries are skipped
//
// - if allowedRoots is set: each file considered is policy-checked (ResolvePath) to avoid symlink/junction sandbox
// escapes.
func SearchFiles(
	ctx context.Context,
	p fspolicy.FSPolicy,
	root, pattern string,
	maxResults int,
) (matchedFiles []string, reachedLimit bool, err error) {
	reachedLimit = false

	if pattern == "" {
		return nil, reachedLimit, errors.New("pattern is required")
	}
	// Still walk an absolute, policy-resolved root for hardening.
	rootArg := root
	if rootArg == "" {
		rootArg = "."
	}

	rootAbs, err := p.ResolvePath(rootArg, ".")
	if err != nil {
		return nil, reachedLimit, err
	}
	// Enforce BlockSymlinks semantics for the root itself (and ensure it exists/is a dir).
	if err := p.VerifyDirResolved(rootAbs); err != nil {
		return nil, reachedLimit, err
	}

	// If symlinks are allowed and the root itself is a symlink-to-dir, WalkDir won't recurse
	// (because it Lstats the root). Walk the resolved target instead, while preserving
	// returned path formatting based on rootArg/rootReturn.
	walkRoot := rootAbs
	if !p.BlockSymlinks() {
		if st, lerr := os.Lstat(rootAbs); lerr == nil && (st.Mode()&os.ModeSymlink) != 0 {
			if resolved, rerr := filepath.EvalSymlinks(rootAbs); rerr == nil && resolved != "" {
				walkRoot = filepath.Clean(resolved)
			}
		}
	}

	// This is the "old-style" root prefix as it would have been passed to WalkDir.
	// Used only to format/match returned paths, not for IO.
	rootReturn := filepath.Clean(rootArg)

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, reachedLimit, err
	}

	limit := maxResults
	if limit <= 0 {
		limit = int(^uint(0) >> 1) // effectively “infinite”
	}

	var matches []string

	walkFn := func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}

		if len(matches) >= limit {
			reachedLimit = true
			return errSearchLimitReached
		}

		// Symlink hardening: skip symlink entries entirely.
		if p.BlockSymlinks() {
			if (d.Type() & os.ModeSymlink) != 0 {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}

		// Defense-in-depth: if sandbox roots are set, policy-check each file path
		// (this catches symlink/junction escapes even when BlockSymlinks==false).
		if p.HasAllowedRoots() && !d.IsDir() {
			if _, rerr := p.ResolvePath(path, ""); rerr != nil {
				return nil //nolint:nilerr // Skip out-of-policy entries.
			}
		}

		if d.IsDir() {
			return nil
		}

		// Match/return using the older path shape:
		// - if caller used "" or ".", return paths like "a/b.txt"
		// - if caller used "some/root", return "some/root/a/b.txt"
		// - if caller used an absolute root, return absolute paths (as WalkDir would).
		displayPath := path
		if rel, rerr := filepath.Rel(walkRoot, path); rerr == nil {
			if rootReturn == "." {
				displayPath = rel
			} else {
				displayPath = filepath.Join(rootReturn, rel)
			}
		}

		// Path match first.
		if re.MatchString(displayPath) {
			matches = append(matches, displayPath)
		} else {
			// Check file content only for reasonably small files.
			if info, _ := d.Info(); info != nil && info.Size() < 1*1024*1024 { // 1 MB guard
				if data, rerr := os.ReadFile(path); rerr == nil {
					sample := data[:min(len(data), 4096)]
					if !isProbablyTextSample(sample) || !utf8.Valid(data) {
						return nil
					}
					if re.Match(data) {
						matches = append(matches, displayPath)
					}
				}
			}
		}

		if len(matches) >= limit {
			reachedLimit = true
			return errSearchLimitReached
		}
		return nil
	}

	err = filepath.WalkDir(walkRoot, walkFn)
	if err != nil && !errors.Is(err, errSearchLimitReached) {
		return nil, reachedLimit, err
	}

	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, reachedLimit, nil
}
