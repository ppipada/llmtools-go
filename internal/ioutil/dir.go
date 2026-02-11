package ioutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ListDirectoryNormalized lists entries in a directory that is assumed to be already normalized.
// It does not normalize or resolve relative paths; callers must do that.
func ListDirectoryNormalized(dir, pattern string) ([]string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, ErrInvalidPath
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir error %w", err)
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

func UniquePathInDir(dir, base string) (string, error) {
	dir = strings.TrimSpace(dir)
	base = strings.TrimSpace(base)
	if dir == "" || base == "" {
		return "", ErrInvalidPath
	}
	if strings.ContainsRune(dir, 0) || strings.ContainsRune(base, 0) {
		return "", ErrInvalidPath
	}

	// Ensure dir exists and is a directory.
	if st, err := os.Stat(dir); err != nil {
		return "", err
	} else if !st.IsDir() {
		return "", fmt.Errorf("directory: %s, err: %w", dir, ErrInvalidDir)
	}

	// Base must be a single filename, not a path.
	if base == "." || base == ".." || filepath.Base(base) != base || filepath.VolumeName(base) != "" {
		return "", ErrInvalidPath
	}

	// First try the plain name.
	p := filepath.Join(dir, base)
	_, err := os.Lstat(p)

	if err != nil && errors.Is(err, os.ErrNotExist) {
		return p, nil
	} else if err != nil {
		return "", err
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
		if _, err := os.Lstat(candidate); err == nil {
			continue
		} else if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else {
			return "", err
		}
	}
	return "", fmt.Errorf("could not allocate unique trash name for %q", base)
}

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
