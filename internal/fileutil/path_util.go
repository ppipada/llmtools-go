package fileutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EnsureDirNoSymlink creates missing directories one component at a time,
// refusing to traverse symlink components.
// "maxNewDirs: 0 => unlimited"; otherwise limits how many missing dirs it will create.
func EnsureDirNoSymlink(dir string, maxNewDirs int) (created int, err error) {
	d, err := NormalizePath(dir)
	if err != nil {
		return 0, err
	}
	if d == "." {
		// Current directory, nothing to create.
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

// VerifyDirNoSymlink ensures dir exists and is a directory, and none of its
// components are symlinks.
func VerifyDirNoSymlink(dir string) error {
	d, err := NormalizePath(dir)
	if err != nil {
		return err
	}
	if d == "." {
		return nil
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

	for _, part := range parts {
		if cur == "" {
			cur = part
		} else {
			cur = filepath.Join(cur, part)
		}
		st, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		if (st.Mode() & os.ModeSymlink) != 0 {
			return fmt.Errorf("refusing to traverse symlink path component: %s", cur)
		}
		if !st.IsDir() {
			return fmt.Errorf("path component is not a directory: %s", cur)
		}
	}

	return nil
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
	return filepath.Clean(p), nil
}

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
