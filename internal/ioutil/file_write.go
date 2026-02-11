package ioutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/flexigpt/llmtools-go/internal/fspolicy"
)

// WriteFileAtomicBytesResolved is like WriteFileAtomicBytes but assumes dst is already an absolute,
// policy-resolved path (i.e. returned from p.ResolvePath).
//
// This avoids re-resolving (and re-checking allowed roots) at higher layers.
func WriteFileAtomicBytesResolved(
	p fspolicy.FSPolicy,
	dst string,
	data []byte,
	perm fs.FileMode,
	overwrite bool,
) error {
	dst = strings.TrimSpace(dst)
	if dst == "" || strings.ContainsRune(dst, 0) {
		return ErrInvalidPath
	}
	if !filepath.IsAbs(dst) {
		return fmt.Errorf("path must be absolute: %s", dst)
	}
	dst = filepath.Clean(filepath.FromSlash(dst))
	return writeFileAtomicBytesResolved(p, dst, data, perm, overwrite, false)
}

// WriteFileAtomicBytesWithParents is a policy-aware convenience wrapper that:
//   - resolves path once via policy
//   - either verifies parent exists (createParents=false) or creates it (createParents=true)
//   - then performs the atomic write
//
// It returns the resolved absolute destination path (even on many error paths) so callers can use it in
// messages/outputs.
func WriteFileAtomicBytesWithParents(
	p fspolicy.FSPolicy,
	path string,
	data []byte,
	perm fs.FileMode,
	overwrite bool,
	createParents bool,
	maxNewDirs int,
) (dst string, err error) {
	dst, err = p.ResolvePath(path, "")
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(dst)

	if createParents {
		if _, err := p.EnsureDirResolved(parent, maxNewDirs); err != nil {
			return dst, err
		}
	} else {
		if err := p.VerifyDirResolved(parent); err != nil {
			return dst, err
		}
	}
	return dst, writeFileAtomicBytesResolved(p, dst, data, perm, overwrite, true)
}

func writeFileAtomicBytesResolved(
	p fspolicy.FSPolicy,
	dst string,
	data []byte,
	perm fs.FileMode,
	overwrite bool,
	parentAlreadyChecked bool,
) error {
	parent := filepath.Dir(dst)

	if p.BlockSymlinks() && !parentAlreadyChecked {
		if err := p.VerifyDirResolved(parent); err != nil {
			return err
		}
	}

	// Validate destination type if it already exists (race-hardened).
	if st, err := os.Lstat(dst); err == nil {
		if st.IsDir() {
			return fmt.Errorf("path is a directory, not a file: %s", dst)
		}
		if (st.Mode() & os.ModeSymlink) != 0 {
			if p.BlockSymlinks() {
				return fmt.Errorf(
					"%w: refusing to write to symlink destination: %s",
					fspolicy.ErrSymlinkDisallowed,
					dst,
				)
			}
			// Even if symlinks are allowed, writing to an existing symlink destination is ambiguous across platforms.
			return fmt.Errorf("refusing to write to symlink destination: %s", dst)
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("refusing to write to non-regular file: %s", dst)
		}
		if !overwrite {
			return fmt.Errorf("file already exists: %w", os.ErrExist)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	tmp, err := os.CreateTemp(parent, ".tmp-llmtools-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	cleanup := func(retErr error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return retErr
	}

	_ = tmp.Chmod(perm)

	n, err := tmp.Write(data)
	if err != nil {
		return cleanup(err)
	}
	if n != len(data) {
		return cleanup(fmt.Errorf("short write: wrote %d bytes, expected %d", n, len(data)))
	}
	if err := tmp.Sync(); err != nil {
		return cleanup(err)
	}
	if err := tmp.Close(); err != nil {
		return cleanup(err)
	}

	if err := commitAtomicTempFile(tmpName, dst, parent, perm, overwrite); err != nil {
		return cleanup(err)
	}

	// TmpName may or may not exist depending on commit strategy; remove is best-effort.
	_ = os.Remove(tmpName)
	return nil
}
