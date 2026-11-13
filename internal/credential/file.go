package credential

import (
	"crypto/rsa"
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
// only. On macOS an extended ACL can grant access that the mode bits do not
// show; it is not detected yet (see internal/credential/AGENTS.md).
type File struct {
	Path string
}

// PrivateKey loads and parses the key. Errors name the path and the fix and
// never contain key material.
func (f File) PrivateKey() (*rsa.PrivateKey, error) {
	path, err := resolvePath(f.Path, os.Getenv("HOME"))
	if err != nil {
		return nil, err
	}
	data, err := readKeyFile(path)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	key, err := parsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("private key %s: %w", path, err)
	}
	return key, nil
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

func readKeyFile(path string) ([]byte, error) {
	target := symlinkTarget(path)
	if err := checkBeforeOpen(path, target); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, openError(path, target, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("private key %s: %w: %w", path, ErrKeyUnreadable, unwrapPathError(err))
	}
	if err := checkKeyFile(stateOf(path, target, info)); err != nil {
		return nil, err
	}
	return readLimited(path, file)
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

func checkBeforeOpen(path, target string) error {
	info, err := os.Stat(path)
	if err != nil {
		return openError(path, target, err)
	}
	if !info.Mode().IsRegular() {
		return checkKeyFile(fileState{path: path, target: target, mode: info.Mode()})
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
