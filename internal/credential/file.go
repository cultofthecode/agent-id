package credential

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
)

const maxKeyFileSize = 64 << 10

// File is a private key kept in a protected file: the value of a file: key
// reference. Path is absolute or starts with ~/, which expands to $HOME when
// the key is loaded. The file, or the target of a symlink, must be a regular
// file owned by the current user with no group or other permission bits
// (0600 or stricter), and must hold exactly one unencrypted RSA private key in
// PKCS#1 or PKCS#8 PEM form. The key is read into memory only; it is never
// copied to another file.
//
// The type is checked before the file is opened, so that opening a device has
// no side effect; the authoritative checks run on the opened descriptor. A
// symlink target named in an error is resolved separately, for the message
// only. On macOS a key file that carries an extended ACL (chmod +a) is
// refused, because the ACL can grant access that the mode bits do not show.
// Every directory above the key file, and above the target of a symlink, must
// be owned by the current user or root and must not be writable by group or
// others (a sticky directory is accepted), so that no other user can replace
// the file. Errors show a path under $HOME as ~/….
type File struct {
	Path string
}

// PrivateKeyPEM loads the key file, checks it, parses the key, and returns a
// copy of the file content; the read buffer is cleared. Errors name the path
// and the fix and never contain key material.
func (f File) PrivateKeyPEM() ([]byte, error) {
	path, err := resolvePath(f.Path, os.Getenv("HOME"))
	if err != nil {
		return nil, err
	}
	home := os.Getenv("HOME")
	data, err := readKeyFile(path, home)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	if _, err := parsePrivateKey(data); err != nil {
		return nil, fmt.Errorf("private key %s: %w", displayPath(path, home), err)
	}
	return bytes.Clone(data), nil
}

func resolvePath(path, home string) (string, error) {
	if strings.ContainsFunc(path, unicode.IsControl) {
		return "", fmt.Errorf("%w: the path contains a control character", ErrInvalidReference)
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		return expandHome(rest, home)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %q: a file reference needs an absolute path or ~/…", ErrInvalidReference, path)
	}
	return filepath.Clean(path), nil
}

func expandHome(rest, home string) (string, error) {
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("%w: ~/%s: HOME is not set to an absolute path", ErrInvalidReference, rest)
	}
	if rest == "" {
		return "", fmt.Errorf("%w: ~/: the reference names the home directory, not a key file", ErrInvalidReference)
	}
	return filepath.Join(home, rest), nil
}

func readKeyFile(path, home string) ([]byte, error) {
	shown := displayPath(path, home)
	target := displayPath(symlinkTarget(path), home)
	if err := checkBeforeOpen(path, shown, target); err != nil {
		return nil, err
	}
	dirs, err := inspectDirs(path, home)
	if err != nil {
		return nil, err
	}
	if err := checkDirs(dirs, uint32(os.Geteuid())); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, openError(shown, target, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("private key %s: %w: %w", shown, ErrKeyUnreadable, unwrapPathError(err))
	}
	state := stateOf(shown, target, info)
	state.acl, state.aclErr = extendedACL(file)
	if err := checkKeyFile(state); err != nil {
		return nil, err
	}
	return readLimited(shown, file)
}

func symlinkTarget(path string) string {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return ""
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return target
}

func checkBeforeOpen(path, shown, target string) error {
	info, err := os.Stat(path)
	if err != nil {
		return openError(shown, target, err)
	}
	if !info.Mode().IsRegular() {
		return checkKeyFile(fileState{path: shown, target: target, mode: info.Mode()})
	}
	return nil
}

func openError(path, target string, err error) error {
	label := describe(fileState{path: path, target: target})
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s: %w; save the private key there with mode 0600 or fix the reference", label, ErrKeyNotFound)
	}
	return fmt.Errorf("%s: %w: %w", label, ErrKeyUnreadable, unwrapPathError(err))
}

func unwrapPathError(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	return err
}

func stateOf(path, target string, info fs.FileInfo) fileState {
	state := fileState{path: path, target: target, mode: info.Mode(), user: uint32(os.Geteuid())}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		state.owner = stat.Uid
		state.ownerKnown = true
	}
	return state
}

func readLimited(path string, file io.Reader) ([]byte, error) {
	buf := make([]byte, maxKeyFileSize+1)
	n, err := io.ReadFull(file, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		clear(buf)
		return nil, fmt.Errorf("private key %s: %w: %w", path, ErrKeyUnreadable, unwrapPathError(err))
	}
	if n > maxKeyFileSize {
		clear(buf)
		return nil, fmt.Errorf("private key %s: %w: the file is larger than %d KiB", path, ErrInvalidKey, maxKeyFileSize>>10)
	}
	return buf[:n], nil
}
