// Package token caches provider tokens on disk, one entry per key, and
// refreshes them before they expire (brief §8 Token lifecycle, §10;
// spec .ai/specs/2026-11-13-token-manager.md).
//
// An entry is a 0600 file in a private 0700 directory, replaced atomically
// with rename; a refresh holds an exclusive flock on the entry's lock file, so
// concurrent processes mint one token. The package fails closed: when no
// valid token can be obtained, Token returns an error and the zero token. No
// error of this package contains a token or the content of a cache file. It
// knows no forge: the key comes from the provider interface.
package token

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// Defaults of a Manager whose fields are zero.
const (
	DefaultMargin      = 5 * time.Minute
	DefaultLockTimeout = 60 * time.Second
)

// Errors of Token besides the wrapped provider errors. Match them with
// errors.Is.
var (
	ErrInvalidKey    = errors.New("token: invalid cache key")
	ErrNoCacheDir    = errors.New("token: no cache directory")
	ErrInsecureCache = errors.New("token: the token cache is not private")
	ErrLockTimeout   = errors.New("token: timed out waiting for the token cache lock")
	ErrUnusableToken = errors.New("token: the provider returned an unusable token")
	ErrCacheWrite    = errors.New("token: cannot write the token cache")
)

// Source mints the tokens of one key. Every provider.Provider is a Source.
type Source interface {
	Name() string
	Host() string
	TokenSubject() string
	Token(ctx context.Context) (provider.Token, error)
}

// Manager hands out cached tokens and refreshes them. The zero values of
// Margin, LockTimeout, and Now select the defaults; Dir is required and comes
// from Dir(os.Getenv) in production.
type Manager struct {
	// Dir is the private directory of the cache entries.
	Dir string
	// Margin is how long before its expiry a token is refreshed.
	Margin time.Duration
	// LockTimeout bounds the wait for another process's refresh.
	LockTimeout time.Duration
	// Now is the clock.
	Now func() time.Time
}

// Token returns a token of src that stays valid for longer than the margin:
// the cached one, or a new one that it mints with src and caches. It never
// returns a token after an error.
func (m *Manager) Token(ctx context.Context, src Source) (provider.Token, error) {
	key, err := KeyFor(src)
	if err != nil {
		return provider.Token{}, err
	}
	if err := m.prepareDir(); err != nil {
		return provider.Token{}, err
	}
	path := filepath.Join(m.Dir, key.fileName())
	if token, ok := m.cached(path, key); ok {
		return token, nil
	}
	unlock, err := lockFile(ctx, path+lockSuffix, m.lockTimeout())
	if err != nil {
		return provider.Token{}, fmt.Errorf("token: refresh %s: %w", key, err)
	}
	defer unlock()
	if token, ok := m.cached(path, key); ok {
		return token, nil
	}
	return m.refresh(ctx, src, key, path)
}

// cached returns the entry at path when it belongs to key and is fresh.
func (m *Manager) cached(path string, key Key) (provider.Token, bool) {
	token, ok := readEntry(path, key, uint32(os.Getuid()))
	if !ok || !fresh(token, m.now(), m.margin()) {
		return provider.Token{}, false
	}
	return token, true
}

// refresh mints a token with src, checks it, and caches it at path. The
// caller holds the lock of path.
func (m *Manager) refresh(ctx context.Context, src Source, key Key, path string) (provider.Token, error) {
	token, err := src.Token(ctx)
	if err != nil {
		return provider.Token{}, fmt.Errorf("token: mint a token for %s: %w", key, err)
	}
	if err := checkMinted(token, m.now(), m.margin()); err != nil {
		return provider.Token{}, fmt.Errorf("token: mint a token for %s: %w", key, err)
	}
	if err := writeEntry(m.Dir, path, encodeEntry(key, token)); err != nil {
		return provider.Token{}, fmt.Errorf("token: cache the token of %s: %w", key, err)
	}
	return token, nil
}

// prepareDir creates the cache directory when it is missing and checks that
// it is private and that no other user can replace it.
func (m *Manager) prepareDir() error {
	if !filepath.IsAbs(m.Dir) {
		return fmt.Errorf("%w: the cache directory must be an absolute path", ErrNoCacheDir)
	}
	dir := filepath.Clean(m.Dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("%w: create %s: %w", ErrInsecureCache, dir, cause(err))
	}
	uid := uint32(os.Getuid())
	if err := inspect(filepath.Dir(dir), uid, checkParent); err != nil {
		return err
	}
	return inspect(dir, uid, checkDir)
}

// inspect applies check to the mode and owner of path, without following a
// symbolic link.
func inspect(path string, uid uint32, check func(path string, mode fs.FileMode, owner, uid uint32) error) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: inspect %s: %w", ErrInsecureCache, path, cause(err))
	}
	owner, known := ownerOf(info)
	if !known {
		return fmt.Errorf("%w: cannot tell who owns %s", ErrInsecureCache, path)
	}
	return check(path, info.Mode(), owner, uid)
}

func (m *Manager) margin() time.Duration {
	if m.Margin <= 0 {
		return DefaultMargin
	}
	return m.Margin
}

func (m *Manager) lockTimeout() time.Duration {
	if m.LockTimeout <= 0 {
		return DefaultLockTimeout
	}
	return m.LockTimeout
}

func (m *Manager) now() time.Time {
	if m.Now == nil {
		return time.Now()
	}
	return m.Now()
}
