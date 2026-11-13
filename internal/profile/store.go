package profile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// Store reads and writes the configuration file at one path. Its methods
// never write during a read and never print the document.
type Store struct {
	path     string
	registry *Registry
}

// NewStore returns the store of the configuration file at path, usually the
// result of Path, with the providers of registry.
func NewStore(path string, registry *Registry) *Store {
	return &Store{path: path, registry: registry}
}

// Load reads and validates the configuration. It refuses a file or
// directory that is not owned by the current user or that group or others
// can write, following symlinks to the file that is read.
func (s *Store) Load() (Config, error) {
	data, err := s.read()
	if err != nil {
		return Config{}, err
	}
	config, err := Parse(data, s.registry)
	if err != nil {
		return Config{}, s.wrap(err)
	}
	return config, nil
}

// Resolve loads the configuration and returns the profile named name. It
// fails with ErrProfileNotFound when there is no such profile and with
// ErrUnsupportedProvider when no validator is registered for its provider.
// There is no default profile.
func (s *Store) Resolve(name string) (Profile, error) {
	config, err := s.Load()
	if err != nil {
		return Profile{}, err
	}
	p, ok := config.Profiles[name]
	if !ok {
		return Profile{}, Errorf(ErrProfileNotFound, "profile %s not found in %s; %s", Quote(name), s.path, available(config))
	}
	if !s.registry.Supports(p.Provider) {
		return Profile{}, Errorf(ErrUnsupportedProvider, "profile %s: provider %s is not supported by this agent-id (supported: %s); upgrade agent-id or fix the provider name", Quote(name), Quote(p.Provider), s.supported())
	}
	return p, nil
}

// read returns the content of the configuration file after the ownership
// and permission checks.
func (s *Store) read() ([]byte, error) {
	if !filepath.IsAbs(s.path) {
		return nil, Errorf(ErrConfigNotFound, "the configuration path %s is not absolute", Quote(s.path))
	}
	dir := filepath.Dir(s.path)
	if err := s.checkDir(dir); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, s.openError(err)
	}
	defer func() { _ = f.Close() }()
	if err := s.checkFile(f); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if err != nil {
		return nil, s.wrap(Errorf(ErrConfigInvalid, "cannot read: %v", err))
	}
	return data, nil
}

// checkFile checks the opened configuration file and, when the path is a
// symlink, the directory of every link of the chain and of the file it ends
// at. It opens without blocking, so a FIFO is refused rather than waited on.
func (s *Store) checkFile(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return s.wrap(Errorf(ErrConfigInvalid, "cannot read: %v", err))
	}
	if !info.Mode().IsRegular() {
		return s.wrap(Errorf(ErrConfigInvalid, "not a regular file"))
	}
	if err := secure(info, "", "600 "+s.path); err != nil {
		return s.wrap(err)
	}
	dirs, err := linkDirs(s.path)
	if err != nil {
		return s.wrap(Errorf(ErrConfigInvalid, "cannot resolve: %v", err))
	}
	for _, dir := range dirs[1:] {
		if err := s.checkDir(dir); err != nil {
			return err
		}
	}
	return nil
}

// maxLinks is the longest symlink chain linkDirs follows.
const maxLinks = 40

// linkDirs returns the directory of path, of every symlink its chain passes,
// and of the file it ends at, each once and in chain order. Every one of
// them decides which file is read, so none may be writable by others.
func linkDirs(path string) ([]string, error) {
	var dirs []string
	for range maxLinks {
		dir, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
		path = filepath.Join(dir, filepath.Base(path))
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			return dirs, nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return nil, err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		path = target
	}
	return nil, errors.New("too many levels of symbolic links")
}

// checkDir checks a directory that holds the configuration file.
func (s *Store) checkDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return s.openError(err)
	}
	if !info.IsDir() {
		return s.wrap(Errorf(ErrConfigInvalid, "%s is not a directory", dir))
	}
	return s.wrapErr(secure(info, "directory "+dir+" is ", "700 "+dir))
}

// secure returns ErrInsecurePermissions when info belongs to another user
// or group or others can write it. subject starts the message ("" for the
// configuration file itself), and chmod holds the arguments of the fix.
func secure(info fs.FileInfo, subject, chmod string) error {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		return Errorf(ErrInsecurePermissions, "%snot owned by the current user (uid %d)", subject, stat.Uid)
	}
	if mode := info.Mode().Perm(); mode&0o022 != 0 {
		return Errorf(ErrInsecurePermissions, "%swritable by group or others (mode %04o); run chmod %s", subject, mode, chmod)
	}
	return nil
}

// openError turns a failure to open the file or its directory into
// ErrConfigNotFound when it does not exist.
func (s *Store) openError(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return &configError{
			sentinels: []error{ErrConfigNotFound, errMissing},
			message:   fmt.Sprintf("no agent-id configuration at %s; run agent-id setup %s --profile <name>", s.path, s.setupProvider()),
		}
	}
	return s.wrap(Errorf(ErrConfigInvalid, "cannot read: %v", err))
}

// wrap prefixes err with the configuration path.
func (s *Store) wrap(err error) error {
	return fmt.Errorf("config %s: %w", s.path, err)
}

// wrapErr is wrap for an error that may be nil.
func (s *Store) wrapErr(err error) error {
	if err == nil {
		return nil
	}
	return s.wrap(err)
}

// setupProvider names the provider argument of the setup command in a hint.
func (s *Store) setupProvider() string {
	if supported := s.registry.Supported(); len(supported) == 1 {
		return supported[0]
	}
	return "<provider>"
}

// supported lists the registered providers for a message.
func (s *Store) supported() string {
	if supported := s.registry.Supported(); len(supported) > 0 {
		return strings.Join(supported, ", ")
	}
	return "none"
}

// available lists the configured profile names for a message.
func available(config Config) string {
	if len(config.Profiles) == 0 {
		return "no profiles configured"
	}
	return "available: " + strings.Join(config.Names(), ", ")
}

// Path returns the absolute configuration path this store reads and writes.
// Unlike the package-level Path, which resolves the default location from
// the environment, it reports the path the store was built with.
func (s *Store) Path() string {
	return s.path
}

// CheckName checks a profile name before any I/O: the inline-secret scan
// with the registered token prefixes first, then the profile name rule. A
// name that looks like a token or key fails with ErrInlineSecret and is
// never echoed; any other invalid name fails with ErrConfigInvalid.
func (s *Store) CheckName(name string) error {
	if looksSecret(name, s.registry.TokenPrefixes()) {
		return Errorf(ErrInlineSecret, "the profile name looks like a token or private key; it is not shown — remove it and revoke it if it was real")
	}
	if !validProfileName(name) {
		return Errorf(ErrConfigInvalid, `invalid profile name %s: use 1–32 lowercase letters, digits or "-", starting with a letter and not ending with "-"`, Quote(name))
	}
	return nil
}
