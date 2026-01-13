package fileutil

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

var errSearchLimitReached = errors.New("search limit reached")

// SearchFiles walks root (default ".") recursively and returns up to maxResults files
// whose *path* or UTF-8 text content* match the regexp pattern.
// If maxResults <= 0, it is treated as "no limit".
func SearchFiles(ctx context.Context, root, pattern string, maxResults int) ([]string, error) {
	if pattern == "" {
		return nil, errors.New("pattern is required")
	}
	if root == "" {
		root = "."
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
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

		// If we've already hit the limit, abort the walk entirely.
		if len(matches) >= limit {
			return errSearchLimitReached
		}

		// Skip directories; just continue walking.
		if d.IsDir() {
			return nil
		}

		// Path match first.
		if re.MatchString(path) {
			matches = append(matches, path)
		} else {
			// Check file content only for reasonably small files.
			if info, _ := d.Info(); info != nil && info.Size() < 1*1024*1024 { // 1 MB guard
				if data, rerr := os.ReadFile(path); rerr == nil && re.Match(data) {
					matches = append(matches, path)
				}
			}
		}

		// If we just reached or exceeded the limit, abort the walk.
		if len(matches) >= limit {
			return errSearchLimitReached
		}

		return nil
	}

	err = filepath.WalkDir(root, walkFn)
	if err != nil && !errors.Is(err, errSearchLimitReached) {
		return nil, err
	}

	// Safety clamp: should not be needed, but guarantees we never return more than limit.
	if len(matches) > limit {
		matches = matches[:limit]
	}

	return matches, nil
}
