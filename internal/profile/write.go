package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// Write changes the configuration under an exclusive lock: it reads the
// current file (or starts from an empty v1 configuration when there is
// none), applies change, validates the result, and replaces the file
// atomically with a mode 0600 file in a mode 0700 directory. A crash leaves
// either the old or the new file. Profiles that change leaves alone, those
// of unregistered providers included, keep their members and values. When
// the path is a symlink, the file it points to is replaced. Write never
// writes a configuration that Load would refuse.
func (s *Store) Write(change func(*Config) error) error {
	if !filepath.IsAbs(s.path) {
		return Errorf(ErrConfigNotFound, "the configuration path %s is not absolute", Quote(s.path))
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return s.wrap(Errorf(ErrConfigInvalid, "cannot create the directory: %v", err))
	}
	if err := s.checkDir(dir); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return s.wrap(Errorf(ErrConfigInvalid, "cannot restrict the directory: %v", err))
	}
	unlock, err := lock(filepath.Join(dir, filepath.Base(s.path)+".lock"))
	if err != nil {
		return s.wrap(Errorf(ErrConfigInvalid, "cannot lock: %v", err))
	}
	defer unlock()
	return s.writeLocked(change)
}

// writeLocked is Write while the lock is held.
func (s *Store) writeLocked(change func(*Config) error) error {
	config, err := s.Load()
	if errors.Is(err, errMissing) {
		config, err = Config{Version: Version, Profiles: map[string]Profile{}}, nil
	}
	if err != nil {
		return err
	}
	if err := change(&config); err != nil {
		return err
	}
	data, err := marshal(config)
	if err != nil {
		return s.wrap(err)
	}
	if _, err := Parse(data, s.registry); err != nil {
		return s.wrap(err)
	}
	target, err := s.target()
	if err != nil {
		return err
	}
	if err := replaceFile(target, data); err != nil {
		return s.wrap(Errorf(ErrConfigInvalid, "cannot write: %v", err))
	}
	return nil
}

// target returns the file that a write replaces: the configuration path, or
// the file it points to when it is a symlink. A symlink whose target does not
// exist is refused rather than replaced by a regular file.
func (s *Store) target() (string, error) {
	info, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.path, nil
	}
	if err != nil {
		return "", s.wrap(Errorf(ErrConfigInvalid, "cannot resolve: %v", err))
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return s.path, nil
	}
	target, err := filepath.EvalSymlinks(s.path)
	if err != nil {
		return "", s.wrap(Errorf(ErrConfigInvalid, "is a symlink whose target cannot be resolved; create the target or remove the link: %v", err))
	}
	return target, nil
}

// marshal returns the document of config, indented with two spaces and
// ending in a newline. Profiles appear in lexical order, each with provider,
// host, and its provider block.
func marshal(config Config) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"version":`)
	buf.WriteString(jsonInt(config.Version))
	buf.WriteString(`,"profiles":{`)
	for i, name := range config.Names() {
		p := config.Profiles[name]
		if p.Name != name {
			return nil, Errorf(ErrConfigInvalid, "profiles.%s: the profile is named %s", sanitize(name), Quote(p.Name))
		}
		if i > 0 {
			buf.WriteByte(',')
		}
		encodeString(&buf, name)
		buf.WriteString(`:{"provider":`)
		encodeString(&buf, p.Provider)
		buf.WriteString(`,"host":`)
		encodeString(&buf, p.Host)
		buf.WriteByte(',')
		encodeString(&buf, p.Provider)
		buf.WriteByte(':')
		buf.Write(p.Settings)
		buf.WriteByte('}')
	}
	buf.WriteString("}}")
	var out bytes.Buffer
	if err := json.Indent(&out, buf.Bytes(), "", "  "); err != nil {
		return nil, Errorf(ErrConfigInvalid, "a provider block is not valid JSON")
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// jsonInt formats n as a JSON number.
func jsonInt(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// replaceFile writes data to a new mode 0600 file next to path, syncs it,
// and renames it over path. The temporary file never outlives a failure.
func replaceFile(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir flushes the directory entry of a rename to disk.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// lock takes an exclusive flock on the file at path, creating it with mode
// 0600, and returns the function that releases it.
func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
