package fileutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListDirectory lists files/dirs in path (default "."), pattern is an optional
// glob filter (filepath.Match).
func ListDirectory(path, pattern string) ([]string, error) {
	dir := path
	if dir == "" {
		dir = "."
	}
	var err error
	dir, err = NormalizePath(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if pattern != "" {
			matched, matchErr := filepath.Match(pattern, name)
			if matchErr != nil {
				return nil, matchErr
			}
			if !matched {
				continue
			}
		}
		out = append(out, name)
	}
	sort.Strings(out)

	return out, nil
}

func CanonicalizeAllowedRoots(roots []string) ([]string, error) {
	var out []string
	for _, r := range roots {
		if strings.TrimSpace(r) == "" {
			continue
		}
		cr, err := canonicalWorkdir(r)
		if err != nil {
			return nil, fmt.Errorf("invalid allowed root %q: %w", r, err)
		}
		if err := ensureDirExists(cr); err != nil {
			return nil, fmt.Errorf("invalid allowed root %q: %w", r, err)
		}
		out = append(out, cr)
	}
	return out, nil
}

func GetEffectiveWorkDir(inputWorkDir string, allowedRoots []string) (string, error) {
	if strings.TrimSpace(inputWorkDir) == "" {
		return "", errors.New("empty workdir received")
	}
	p, err := canonicalWorkdir(inputWorkDir)
	if err != nil {
		return "", err
	}
	if err := ensureDirExists(p); err != nil {
		return "", err
	}
	if err := ensureWorkdirAllowed(p, allowedRoots); err != nil {
		return "", err
	}
	return p, nil
}

func ensureDirExists(p string) error {
	st, err := os.Stat(p)
	if err != nil {
		return errors.Join(err, errors.New("no such dir"))
	}
	if !st.IsDir() {
		return fmt.Errorf("workdir is not a directory: %s", p)
	}
	return nil
}

func canonicalWorkdir(p string) (string, error) {
	if strings.ContainsRune(p, '\x00') {
		return "", errors.New("workdir contains NUL byte")
	}
	cleaned := filepath.Clean(p)
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", err
	}
	// Best-effort: resolve symlinks to avoid platform-dependent aliases
	// (e.g. macOS /var -> /private/var) and to harden allowed-root checks.
	// If resolution fails (odd FS / permissions), keep the absolute path.
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil && resolved != "" {
		abs = resolved
	}
	return abs, nil
}

func ensureWorkdirAllowed(p string, roots []string) error {
	if len(roots) == 0 {
		return nil
	}
	for _, r := range roots {
		ok, err := pathWithinRoot(r, p)
		if err != nil {
			continue
		}
		if ok {
			return nil
		}
	}
	return fmt.Errorf("workdir %q is outside allowed roots", p)
}

func pathWithinRoot(root, p string) (bool, error) {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false, err
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return true, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, nil
	}
	return true, nil
}
