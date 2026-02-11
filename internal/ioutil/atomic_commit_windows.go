//go:build windows

package ioutil

import (
	"fmt"
	"io/fs"
	"os"
	"time"
)

func commitAtomicTempFile(tmpName, dst, parent string, perm fs.FileMode, overwrite bool) error {
	_ = parent // windows: directory sync is skipped/no-op.

	if !overwrite {
		// Windows rename won't overwrite. This satisfies overwrite=false.
		if err := os.Rename(tmpName, dst); err != nil {
			if _, stErr := os.Lstat(dst); stErr == nil {
				return fmt.Errorf("file already exists: %w", os.ErrExist)
			}
			return err
		}
		_ = os.Chmod(dst, perm)
		return nil
	}

	// overwrite=true: retry rename + remove (AV/indexers can race).
	var renameErr error
	for attempt := range 6 {
		renameErr = os.Rename(tmpName, dst)
		if renameErr == nil {
			_ = os.Chmod(dst, perm)
			return nil
		}

		// If dest exists, try remove then retry.
		if _, stErr := os.Lstat(dst); stErr == nil {
			_ = os.Remove(dst)
		}
		time.Sleep(time.Duration(15*(attempt+1)) * time.Millisecond)
	}
	return renameErr
}

func syncDirBestEffort(dir string) error {
	_ = dir
	return nil
}
