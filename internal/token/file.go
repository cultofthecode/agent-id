package token

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// File names and limits of the cache directory.
const (
	entrySuffix   = ".json"
	lockSuffix    = ".lock"
	tempSuffix    = ".tmp"
	maxEntrySize  = 64 << 10
	lockPollEvery = 20 * time.Millisecond
)

// checkDir accepts the cache directory only when it is a real directory,
// owned by uid, with no group or other permission bits.
func checkDir(path string, mode fs.FileMode, owner, uid uint32) error {
	return checkOwnedDir(path, mode, owner, uid, 0o077)
}

// checkParent accepts the parent of the cache directory only when it is a
// real directory owned by uid that no group or other user can write, so that
// nobody else can replace the cache directory after checkDir.
func checkParent(path string, mode fs.FileMode, owner, uid uint32) error {
	return checkOwnedDir(path, mode, owner, uid, 0o022)
}

// checkOwnedDir accepts a real directory owned by uid whose mode has none of
// the forbidden permission bits.
func checkOwnedDir(path string, mode fs.FileMode, owner, uid uint32, forbidden fs.FileMode) error {
	switch {
	case mode&fs.ModeSymlink != 0:
		return fmt.Errorf("%w: %s is a symbolic link; remove it so that agent-id can create a private directory", ErrInsecureCache, path)
	case !mode.IsDir():
		return fmt.Errorf("%w: %s is not a directory; remove it so that agent-id can create a private directory", ErrInsecureCache, path)
	case owner != uid:
		return fmt.Errorf("%w: %s belongs to another user; remove it so that agent-id can create a private directory", ErrInsecureCache, path)
	case mode.Perm()&forbidden != 0:
		return fmt.Errorf("%w: %s has mode %04o; run chmod 700 %s", ErrInsecureCache, path, mode.Perm(), path)
	}
	return nil
}

// usableEntryFile reports whether an entry file may be read: a regular file
// owned by uid with mode exactly 0600 and at most maxEntrySize bytes.
func usableEntryFile(mode fs.FileMode, owner, uid uint32, size int64) bool {
	return mode.IsRegular() && mode&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) == 0o600 &&
		owner == uid && size <= maxEntrySize
}

// ownerOf returns the user id that owns the file of info.
func ownerOf(info fs.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Uid, true
}

// readEntry returns the token of the entry name in root when it is a usable
// entry of key. Every failure, a missing file included, is a cache miss.
func readEntry(root *os.Root, name string, key Key, uid uint32) (token provider.Token, ok bool) {
	f, err := openNoFollow(root, name, os.O_RDONLY|syscall.O_NONBLOCK)
	if err != nil {
		return provider.Token{}, false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return provider.Token{}, false
	}
	owner, known := ownerOf(info)
	if !known || !usableEntryFile(info.Mode(), owner, uid, info.Size()) {
		return provider.Token{}, false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxEntrySize+1))
	if err != nil || len(data) > maxEntrySize {
		return provider.Token{}, false
	}
	decoded, err := decodeEntry(data, key)
	return decoded, err == nil
}

// openNoFollow opens name in root and refuses a symbolic link with
// syscall.ELOOP. Methods of os.Root follow a link that stays inside the root
// even with O_NOFOLLOW, so it checks name with Lstat before the open, and
// after the open that name still refers to the opened file. A read without
// the lock can see a writer rename a new entry over name between the open and
// the second Lstat; it then fails with ELOOP, which readEntry treats as a
// miss, and Token reads the entry again under the lock.
func openNoFollow(root *os.Root, name string, flag int) (*os.File, error) {
	if info, err := root.Lstat(name); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return nil, &fs.PathError{Op: "open", Path: name, Err: syscall.ELOOP}
	}
	f, err := root.OpenFile(name, flag, 0)
	if err != nil {
		return nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if named, err := root.Lstat(name); err != nil || !os.SameFile(opened, named) {
		_ = f.Close()
		return nil, &fs.PathError{Op: "open", Path: name, Err: syscall.ELOOP}
	}
	return f, nil
}

// lockPath returns the lock file of the entry name: <sha256>.lock.
func lockPath(name string) string {
	return strings.TrimSuffix(name, entrySuffix) + lockSuffix
}

// inRoot returns the path of name in root, for messages.
func inRoot(root *os.Root, name string) string {
	return filepath.Join(root.Name(), name)
}

// writeEntry replaces the entry name in root with data atomically: a 0600
// temporary file in root, synced, renamed over name, and the directory synced
// on a best-effort basis, because the entry is already in place by then. On
// failure it removes the temporary file and wraps ErrCacheWrite. The caller
// holds the entry's lock, so temporary files of the entry left by a crashed
// writer are removed first. Every operation goes through root, so a
// directory swapped in at the cache path after the checks is never written.
func writeEntry(root *os.Root, name string, data []byte) (err error) {
	removeStaleTemps(root, name)
	if info, statErr := root.Lstat(name); statErr == nil && info.IsDir() {
		return fmt.Errorf("%w: %s is a directory; remove it", ErrCacheWrite, inRoot(root, name))
	}
	temp := name + "." + rand.Text() + tempSuffix
	f, err := root.OpenFile(temp, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("%w: create a file in %s: %w", ErrCacheWrite, root.Name(), cause(err))
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = root.Remove(temp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("%w: restrict %s: %w", ErrCacheWrite, inRoot(root, temp), cause(err))
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("%w: write %s: %w", ErrCacheWrite, inRoot(root, temp), cause(err))
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("%w: sync %s: %w", ErrCacheWrite, inRoot(root, temp), cause(err))
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%w: close %s: %w", ErrCacheWrite, inRoot(root, temp), cause(err))
	}
	if err := root.Rename(temp, name); err != nil {
		return fmt.Errorf("%w: replace %s: %w", ErrCacheWrite, inRoot(root, name), cause(err))
	}
	syncDir(root)
	return nil
}

// removeStaleTemps removes the temporary files of the entry name that a
// crashed writer left in root.
func removeStaleTemps(root *os.Root, name string) {
	d, err := root.Open(".")
	if err != nil {
		return
	}
	entries, err := d.ReadDir(-1)
	_ = d.Close()
	if err != nil {
		return
	}
	for _, entry := range entries {
		if found := entry.Name(); strings.HasPrefix(found, name+".") && strings.HasSuffix(found, tempSuffix) {
			_ = root.Remove(found)
		}
	}
}

// syncDir makes a rename in root durable where the system allows it.
func syncDir(root *os.Root) {
	d, err := root.Open(".")
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// lockFile takes an exclusive flock on the lock file name in root, waiting
// until ctx ends or timeout passes, and returns the function that releases
// it. The kernel releases the lock when the process dies.
func lockFile(ctx context.Context, root *os.Root, name string, timeout time.Duration) (func(), error) {
	path := inRoot(root, name)
	if err := createMissing(root, name); err != nil {
		return nil, lockOpenError(path, err)
	}
	f, err := openNoFollow(root, name, os.O_RDWR|syscall.O_NONBLOCK)
	if err != nil {
		return nil, lockOpenError(path, err)
	}
	if err := checkLockFile(f, path); err != nil {
		_ = f.Close()
		return nil, err
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("token: lock %s: %w", filepath.Base(path), err)
		}
		if err := waitForLock(ctx, deadline.C); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
}

// createMissing creates the empty mode 0600 file name in root unless a file,
// link, or other entry of that name exists. It uses O_EXCL because on darwin
// an os.Root open with O_CREATE but without O_EXCL can fail with ENOENT while
// other processes create the same file.
func createMissing(root *os.Root, name string) error {
	f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return f.Close()
}

// lockOpenError classifies a failure to open the lock file: a symbolic link
// (refused by O_NOFOLLOW) is an insecure cache, anything else a failed write.
func lockOpenError(path string, err error) error {
	if errors.Is(err, syscall.ELOOP) {
		return fmt.Errorf("%w: the lock file %s is a symbolic link; remove it", ErrInsecureCache, path)
	}
	return fmt.Errorf("%w: open the lock file %s: %w", ErrCacheWrite, filepath.Base(path), cause(err))
}

// checkLockFile accepts the lock file only when it is a regular file of the
// current user.
func checkLockFile(f *os.File, path string) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("%w: inspect the lock file %s: %w", ErrInsecureCache, filepath.Base(path), cause(err))
	}
	owner, known := ownerOf(info)
	if !info.Mode().IsRegular() || !known || owner != uint32(os.Getuid()) {
		return fmt.Errorf("%w: the lock file %s is not a regular file of the current user; remove it", ErrInsecureCache, path)
	}
	return nil
}

// waitForLock waits one poll interval, or fails when ctx ends or the deadline
// fires.
func waitForLock(ctx context.Context, deadline <-chan time.Time) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("token: wait for the cache lock: %w", ctx.Err())
	case <-deadline:
		return ErrLockTimeout
	case <-time.After(lockPollEvery):
		return nil
	}
}

// cause returns the system error inside a path or link error, so that a
// message names the path once; other errors are returned unchanged.
func cause(err error) error {
	if inner := errors.Unwrap(err); inner != nil {
		return inner
	}
	return err
}
