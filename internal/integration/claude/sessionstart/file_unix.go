//go:build unix

package sessionstart

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
)

// openEnvFile opens path for appending without following a symlink, checks
// the opened descriptor, and takes an exclusive lock on it. The caller closes
// the file, which releases the lock.
func openEnvFile(path string, privateGroup func(gid uint32) bool) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, errors.New("CLAUDE_ENV_FILE is a symbolic link; refusing to write")
		}
		return nil, fmt.Errorf("cannot open CLAUDE_ENV_FILE: %w", errors.Unwrap(err))
	}
	if err := checkEnvFile(f, privateGroup); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("cannot lock CLAUDE_ENV_FILE: %w", err)
	}
	return f, nil
}

func checkEnvFile(f *os.File, privateGroup func(gid uint32) bool) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("cannot inspect CLAUDE_ENV_FILE: %w", err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() {
		return errors.New("CLAUDE_ENV_FILE is not a regular file")
	}
	if int(st.Uid) != os.Getuid() {
		return errors.New("CLAUDE_ENV_FILE is not owned by the current user")
	}
	if info.Mode().Perm()&0o002 != 0 {
		return errors.New("CLAUDE_ENV_FILE is writable by others")
	}
	if info.Mode().Perm()&0o020 != 0 && !privateGroup(st.Gid) {
		return errors.New("CLAUDE_ENV_FILE is group-writable by a group that is not private to the current user")
	}
	return nil
}

// UserPrivateGroup reports whether gid is a user-private group: its name is
// the current user's name and, in /etc/group, it has no members. A group that
// cannot be read from /etc/group is not private.
func UserPrivateGroup(gid uint32) bool {
	u, err := user.Current()
	if err != nil {
		return false
	}
	g, err := user.LookupGroupId(strconv.FormatUint(uint64(gid), 10))
	if err != nil || g.Name != u.Username {
		return false
	}
	return groupHasNoMembers("/etc/group", g.Name)
}

func groupHasNoMembers(path, name string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) == 4 && fields[0] == name {
			return strings.TrimSpace(fields[3]) == ""
		}
	}
	return false
}
