package profile

import "path/filepath"

// Names of the configuration directory and file.
const (
	DirName  = "agent-id"
	FileName = "config.json"
)

// Path returns the path of the configuration file:
// $XDG_CONFIG_HOME/agent-id/config.json, or $HOME/.config/agent-id/config.json
// when XDG_CONFIG_HOME is unset, empty, or not absolute. It reads the
// environment only through getenv and uses the same rule on macOS and Linux.
// It fails with ErrConfigNotFound when neither variable gives an absolute
// directory.
func Path(getenv func(string) string) (string, error) {
	if dir := getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, DirName, FileName), nil
	}
	if home := getenv("HOME"); filepath.IsAbs(home) {
		return filepath.Join(home, ".config", DirName, FileName), nil
	}
	return "", Errorf(ErrConfigNotFound, "no agent-id configuration: neither XDG_CONFIG_HOME nor HOME is an absolute path")
}
