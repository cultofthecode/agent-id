package token

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// CheckCacheDir reports, without creating or changing anything and without
// reading any entry, whether dir would pass the checks that Manager.Token
// applies before it uses the cache: dir is not a symbolic link, is a
// directory owned by the current user with no group or other permission, and
// its parent is the user's and not group- or other-writable. It returns
// exists false and a nil error when dir does not exist yet, and an error that
// wraps ErrInsecureCache otherwise. It is the read-only check of agent-id
// doctor.
func CheckCacheDir(dir string) (exists bool, err error) {
	if !filepath.IsAbs(dir) {
		return false, fmt.Errorf("%w: the cache directory must be an absolute path", ErrNoCacheDir)
	}
	dir = filepath.Clean(dir)
	uid := uint32(os.Getuid())
	if _, err := inspect(filepath.Dir(dir), uid, os.Lstat, checkParent); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if _, err := inspect(dir, uid, os.Lstat, checkDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
