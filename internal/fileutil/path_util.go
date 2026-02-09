package fileutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

var errPathMustBeAbsolute = errors.New("path must be absolute")

var errWindowsDriveRelativePath = errors.New(
	"windows drive-relative paths like `C:foo` are not supported; use `C:\\foo` or a relative path without a drive letter",
)

// InitPathPolicy canonicalizes allowedRoots and computes an effective workBaseDir.
//
// Behavior:
//   - allowedRoots == nil/empty => allow all (canonRoots will be nil/empty)
//   - workBaseDir blank:
//   - if allowedRoots is set => defaults to the first allowed root (more deterministic/sandbox-friendly)
//   - else => defaults to current process working directory
//   - returned effectiveBase is canonicalized and guaranteed to exist and (if roots set) be within roots
func InitPathPolicy(workBaseDir string, allowedRoots []string) (effectiveBase string, canonRoots []string, err error) {
	canonRoots, err = CanonicalizeAllowedRoots(allowedRoots)
	if err != nil {
		return "", nil, err
	}

	base := strings.TrimSpace(workBaseDir)
	if base == "" {
		if len(canonRoots) > 0 {
			// If a sandbox is configured, default base to the sandbox root.
			// This avoids surprising "cwd outside allowed roots" failures and is deterministic.
			base = canonRoots[0]
		} else {
			cwd, e := os.Getwd()
			if e != nil {
				return "", nil, e
			}
			base = cwd
		}
	}

	effectiveBase, err = GetEffectiveWorkDir(base, canonRoots)
	if err != nil {
		return "", nil, err
	}
	return effectiveBase, canonRoots, nil
}

// ResolvePath resolves an input path (absolute or relative) to an absolute path:
//   - relative paths resolve against workBaseDir
//   - enforces allowedRoots (if set)
//   - normalizes OS-specific separators and cleans path
//   - applies macOS root-level compatibility symlink aliases (e.g. /var -> /private/var)
func ResolvePath(workBaseDir string, allowedRoots []string, inputPath, defaultIfEmpty string) (string, error) {
	s := strings.TrimSpace(inputPath)
	if s == "" {
		s = strings.TrimSpace(defaultIfEmpty)
	}

	norm, err := NormalizePath(s)
	if err != nil {
		return "", err
	}

	// Harden Windows: reject ambiguous "C:foo" drive-relative paths.
	if runtime.GOOS == toolutil.GOOSWindows {
		if vol := filepath.VolumeName(norm); vol != "" && !filepath.IsAbs(norm) {
			return "", errWindowsDriveRelativePath
		}
	}

	// Resolve relative against base.
	if !filepath.IsAbs(norm) {
		// IMPORTANT: if workBaseDir is empty, do not silently resolve relative paths
		// against whatever filepath.Abs uses (process CWD) without making it explicit.
		// This also keeps behavior consistent with tool constructors that default baseDir to CWD.
		if strings.TrimSpace(workBaseDir) == "" {
			cwd, e := os.Getwd()
			if e != nil {
				return "", e
			}
			workBaseDir = cwd
		}
		norm = filepath.Join(workBaseDir, norm)
	}

	abs, err := filepath.Abs(norm)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)

	// Keep macOS paths coherent with CanonicalizeAllowedRoots/GetEffectiveWorkDir behavior.
	abs = ApplyDarwinSystemRootAliases(abs)

	if err := EnsurePathWithinAllowedRoots(abs, allowedRoots); err != nil {
		return "", fmt.Errorf("path %q is outside allowed roots %q", abs, allowedRoots)
	}
	return abs, nil
}

// EnsureDirNoSymlink creates missing directories one component at a time,
// refusing to traverse symlink components.
// "maxNewDirs: 0 => unlimited"; otherwise limits how many missing dirs it will create.
func EnsureDirNoSymlink(dir string, maxNewDirs int) (created int, err error) {
	return walkDirNoSymlink(dir, true, maxNewDirs)
}

// VerifyDirNoSymlink ensures dir exists and is a directory, and none of its
// components are symlinks.
func VerifyDirNoSymlink(dir string) error {
	_, err := walkDirNoSymlink(dir, false, 0)
	return err
}

func walkDirNoSymlink(dir string, createMissing bool, maxNewDirs int) (created int, err error) {
	d, err := NormalizePath(dir)
	if err != nil {
		return 0, err
	}
	if d == "." {
		return 0, nil
	}

	vol := filepath.VolumeName(d)
	rest := d[len(vol):]

	sep := string(os.PathSeparator)
	rest = strings.TrimLeft(rest, sep)

	parts := []string{}
	if rest != "" {
		for p := range strings.SplitSeq(rest, sep) {
			if p == "" || p == "." {
				continue
			}
			parts = append(parts, p)
		}
	}

	cur := ""
	if vol != "" {
		if filepath.IsAbs(d) {
			cur = vol + sep
		} else {
			cur = vol
		}
	} else if filepath.IsAbs(d) {
		cur = sep
	}

	created = 0
	for _, part := range parts {
		if cur == "" {
			cur = part
		} else {
			cur = filepath.Join(cur, part)
		}

		st, err := os.Lstat(cur)
		if err == nil {
			if (st.Mode() & os.ModeSymlink) != 0 {
				if resolved, ok, aerr := allowDarwinSystemSymlink(cur); aerr != nil {
					return created, aerr
				} else if ok {
					// Treat the system symlink as allowed, and continue traversal
					// from its resolved real directory.
					cur = resolved
					continue
				}
				return created, fmt.Errorf("refusing to traverse symlink path component: %s", cur)
			}
			if !st.IsDir() {
				return created, fmt.Errorf("path component is not a directory: %s", cur)
			}
			continue
		}

		if !errors.Is(err, os.ErrNotExist) {
			return created, err
		}
		if !createMissing {
			return created, err
		}

		if maxNewDirs > 0 && created >= maxNewDirs {
			return created, fmt.Errorf("too many parent directories to create (max %d)", maxNewDirs)
		}
		if err := os.Mkdir(cur, 0o755); err != nil {
			return created, err
		}
		created++
	}

	return created, nil
}

func UniquePathInDir(dir, base string) (string, error) {
	// First try the plain name.
	p := filepath.Join(dir, base)
	if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
		return p, nil
	}

	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	// Try a few times; collisions are extremely unlikely with time+random suffix.
	for range 12 {
		sfx, err := randomHex(6) // 12 hex chars
		if err != nil {
			return "", err
		}
		ts := time.Now().UTC().Format("20060102T150405.000000000Z")
		name := fmt.Sprintf("%s.%s.%s%s", stem, ts, sfx, ext)
		candidate := filepath.Join(dir, name)
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not allocate unique trash name for %q", base)
}

// NormalizeAbsPath normalizes the input path (trim, NUL reject, Clean) and
// requires it to be absolute.
//
// Tools that require absolute paths should use this helper to keep behavior
// consistent across the toolset.
func NormalizeAbsPath(p string) (string, error) {
	norm, err := NormalizePath(p)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(norm) {
		return "", errPathMustBeAbsolute
	}
	return norm, nil
}

// NormalizePath:
// - trims
// - rejects empty and NUL byte
// - filepath.Clean.
func NormalizePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", ErrInvalidPath
	}
	if strings.ContainsRune(p, 0) {
		return "", ErrInvalidPath
	}
	p = filepath.FromSlash(p)
	return filepath.Clean(p), nil
}

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
