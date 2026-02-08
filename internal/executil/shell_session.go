package executil

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

type envEntry struct {
	key string
	val string
}
type ShellSession struct {
	id      string
	workdir string
	env     map[string]string
	mu      sync.RWMutex
	closed  bool
}

func (sess *ShellSession) GetID() string {
	return sess.id
}

func (sess *ShellSession) SetWorkDir(workdir string) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.workdir = workdir
}

func (sess *ShellSession) AddToEnv(additionalEnv map[string]string) error {
	if len(additionalEnv) == 0 {
		return nil
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return errors.New("session is closed")
	}

	if sess.env == nil {
		sess.env = map[string]string{}
	}
	if runtime.GOOS == toolutil.GOOSWindows {
		// Rebuild session env to canonical keys to avoid case-insensitive duplicates
		// causing nondeterministic behavior.
		canon := make(map[string]string, len(sess.env)+len(additionalEnv))
		for k, v := range sess.env {
			kk := strings.ToUpper(strings.TrimSpace(k))
			if kk == "" {
				continue
			}
			canon[kk] = v
		}
		for k, v := range additionalEnv {
			kk := strings.ToUpper(strings.TrimSpace(k))
			if kk == "" {
				continue
			}
			canon[kk] = v
		}
		sess.env = canon
	} else {
		maps.Copy(sess.env, additionalEnv)
	}

	return nil
}

func (sess *ShellSession) GetEffectiveEnv(overrides map[string]string) ([]string, error) {
	// Start with current process env.
	envMap := map[string]envEntry{}

	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			kk := strings.TrimSpace(k)
			if kk == "" || strings.ContainsRune(kk, '\x00') || strings.ContainsRune(v, '\x00') ||
				strings.Contains(kk, "=") {
				continue
			}
			ck := canonicalEnvKey(kk)
			envMap[ck] = envEntry{key: kk, val: v}
		}
	}

	// Then session env.
	if sess != nil {
		sess.mu.RLock()
		closed := sess.closed
		snap := maps.Clone(sess.env)
		sess.mu.RUnlock()
		if closed {
			return nil, errors.New("session is closed")
		}
		for k, v := range snap {
			if err := validateEnvKV(k, v); err != nil {
				return nil, fmt.Errorf("invalid session env %q: %w", k, err)
			}
			kk := strings.TrimSpace(k)
			ck := canonicalEnvKey(kk)
			envMap[ck] = envEntry{key: kk, val: v}
		}
	}

	// Then per-call overrides.
	if len(overrides) != 0 {
		for k, v := range overrides {
			// Overrides are validated by validateEnvMap in ShellCommand; still normalize key.
			kk := strings.TrimSpace(k)
			if kk == "" {
				continue
			}
			ck := canonicalEnvKey(kk)
			envMap[ck] = envEntry{key: kk, val: v}
		}
	}

	// Stable ordering (deterministic across runs).
	entries := make([]envEntry, 0, len(envMap))
	for _, e := range envMap {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		// Compare canonical keys for stable ordering across Windows/unix.
		return canonicalEnvKey(entries[i].key) < canonicalEnvKey(entries[j].key)
	})
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.key+"="+e.val)
	}
	return out, nil
}

func (sess *ShellSession) GetEffectiveWorkdir(arg string, allowedRoots []string) (string, error) {
	if strings.TrimSpace(arg) != "" {
		p, err := canonicalWorkdir(arg)
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
	if sess != nil {
		sess.mu.RLock()
		wd := sess.workdir
		closed := sess.closed
		sess.mu.RUnlock()
		if closed {
			return "", errors.New("session is closed")
		}
		if strings.TrimSpace(wd) != "" {
			p, err := canonicalWorkdir(wd)
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
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if err := ensureWorkdirAllowed(cwd, allowedRoots); err != nil {
		return "", err
	}
	return cwd, nil
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

func ValidateEnvMap(m map[string]string) error {
	for k, v := range m {
		if err := validateEnvKV(k, v); err != nil {
			return fmt.Errorf("env %q: %w", k, err)
		}
	}
	return nil
}

func validateEnvKV(k, v string) error {
	kk := strings.TrimSpace(k)
	if kk == "" {
		return errors.New("empty name")
	}
	if strings.ContainsRune(kk, '\x00') || strings.ContainsRune(v, '\x00') {
		return errors.New("contains NUL byte")
	}
	if strings.Contains(kk, "=") {
		return errors.New("name contains '='")
	}
	return nil
}

func canonicalEnvKey(k string) string {
	if runtime.GOOS == toolutil.GOOSWindows {
		return strings.ToUpper(k)
	}
	return k
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
