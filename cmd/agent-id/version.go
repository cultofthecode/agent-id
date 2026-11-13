package main

import (
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// version and commit are set by the linker: -X main.version and -X main.commit.
var (
	version string
	commit  string
)

// devVersion is what agent-id reports when no source knows its version.
const devVersion = "dev"

// buildInfoReader returns the build information of the running binary, like
// debug.ReadBuildInfo.
type buildInfoReader func() (*debug.BuildInfo, bool)

// resolveVersion returns the version of the build without a leading "v": the
// linked value, else the module version of the build information unless it is
// empty or "(devel)", else "dev".
func resolveVersion(linked string, readBuildInfo buildInfoReader) string {
	if linked != "" {
		return strings.TrimPrefix(linked, "v")
	}
	if info, ok := readBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return devVersion
}

// versionText returns the version, followed by " (commit <commit>)" when the
// build knows its commit.
func versionText(linkedVersion, linkedCommit string, readBuildInfo buildInfoReader) string {
	text := resolveVersion(linkedVersion, readBuildInfo)
	if linkedCommit == "" {
		return text
	}
	return text + " (commit " + linkedCommit + ")"
}

// withVersion gives root the flag --version, which prints
// "agent-id version" followed by text.
func withVersion(root *cobra.Command, text string) *cobra.Command {
	root.Version = text
	root.SetVersionTemplate(commandName + " version {{.Version}}\n")
	return root
}
