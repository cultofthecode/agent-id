//go:build !unix

package sessionstart

import (
	"errors"
	"os"
)

func openEnvFile(string, func(uint32) bool) (*os.File, error) {
	return nil, errors.New("the Claude Code integration is not supported on this platform")
}

// UserPrivateGroup reports false: there are no groups on this platform.
func UserPrivateGroup(uint32) bool { return false }
