package fileutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/flexigpt/llmtools-go/internal/toolutil"
)

// Root-level macOS compatibility symlinks we allow and normalize.
// Keep this list small and explicit.
var darwinSystemSymlinkAliases = map[string]string{
	"/var":  "/private/var",
	"/tmp":  "/private/tmp",
	"/etc":  "/private/etc",
	"/bin":  "/usr/bin",
	"/sbin": "/usr/sbin",
	"/lib":  "/usr/lib",
}

// ApplyDarwinSystemRootAliases rewrites known macOS root-level compatibility symlink prefixes
// to their canonical target paths (e.g. /var/... -> /private/var/...).
//
// It does not access the filesystem and does not resolve arbitrary symlinks.
func ApplyDarwinSystemRootAliases(p string) string {
	if runtime.GOOS != toolutil.GOOSDarwin {
		return p
	}
	if strings.TrimSpace(p) == "" {
		return p
	}
	clean := filepath.Clean(p)
	sep := string(os.PathSeparator)

	for from, to := range darwinSystemSymlinkAliases {
		if clean == from {
			return to
		}
		if strings.HasPrefix(clean, from+sep) {
			return to + clean[len(from):]
		}
	}
	return clean
}

// allowDarwinSystemSymlink checks whether cur is one of the macOS compatibility
// symlinks and (if so) returns the expected resolved absolute directory.
//
// This allows us to keep "no symlink traversal" behavior while permitting:
//
//	/var -> /private/var
//	/tmp -> /private/tmp
//	/etc -> /private/etc
//
// (You can extend this list if needed, but keep it small.)
func allowDarwinSystemSymlink(cur string) (resolved string, ok bool, err error) {
	if runtime.GOOS != toolutil.GOOSDarwin {
		return "", false, nil
	}
	// Only allow exact root-level paths.
	expected, okAlias := darwinSystemSymlinkAliases[cur]
	if !okAlias || expected == "" {
		return "", false, nil
	}

	target, rerr := os.Readlink(cur)
	if rerr != nil {
		return "", false, rerr
	}

	// Readlink may return relative targets like "private/var".
	res := target
	if !filepath.IsAbs(res) {
		res = filepath.Join(filepath.Dir(cur), res)
	}
	res = filepath.Clean(res)

	if res != expected {
		return "", false, nil
	}

	// Ensure the resolved target is a real directory (and not itself a symlink).
	st, serr := os.Lstat(res)
	if serr != nil {
		return "", false, serr
	}
	if (st.Mode() & os.ModeSymlink) != 0 {
		return "", false, nil
	}
	if !st.IsDir() {
		return "", false, nil
	}

	return res, true, nil
}
