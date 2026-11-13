package credential

import (
	"fmt"
	"io/fs"
)

const groupOrOtherBits fs.FileMode = 0o077

type fileState struct {
	path       string
	target     string
	mode       fs.FileMode
	owner      uint32
	ownerKnown bool
	user       uint32
}

func checkKeyFile(state fileState) error {
	if !state.mode.IsRegular() {
		return fmt.Errorf("%s: %w: not a regular file (%s); point the reference at the PEM file of the private key",
			describe(state), ErrInsecureKeyFile, fileType(state.mode))
	}
	if state.mode.Perm()&groupOrOtherBits != 0 {
		return fmt.Errorf("%s: %w: accessible by group or others (mode %04o); run chmod 600 %s",
			describe(state), ErrInsecureKeyFile, state.mode.Perm(), fixTarget(state))
	}
	if !state.ownerKnown {
		return fmt.Errorf("%s: %w: the owner of the file cannot be determined", describe(state), ErrInsecureKeyFile)
	}
	if state.owner != state.user {
		return fmt.Errorf("%s: %w: owned by uid %d, not by the current user (uid %d); keep the key in a file you own with mode 0600",
			describe(state), ErrInsecureKeyFile, state.owner, state.user)
	}
	return nil
}

func describe(state fileState) string {
	if state.target == "" {
		return "private key " + state.path
	}
	return fmt.Sprintf("private key %s (symlink to %s)", state.path, state.target)
}

func fixTarget(state fileState) string {
	if state.target == "" {
		return state.path
	}
	return state.target
}

func fileType(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "directory"
	case mode&fs.ModeSymlink != 0:
		return "symlink"
	case mode&fs.ModeNamedPipe != 0:
		return "named pipe"
	case mode&fs.ModeSocket != 0:
		return "socket"
	case mode&fs.ModeDevice != 0:
		return "device"
	default:
		return "irregular file"
	}
}
