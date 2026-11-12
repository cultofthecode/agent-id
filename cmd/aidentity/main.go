// Command aidentity gives AI coding agents their own GitHub identity for one
// process and its descendants, without changing the developer's Git or gh
// configuration.
//
// This package is the composition root. It builds the root command from the
// commands of the slices, and it is the only package that prints errors and
// sets the exit code.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Exit codes of aidentity.
const (
	exitOK      = 0
	exitFailure = 1
)

// commandName is the working name of the binary (brief §14: the name
// aidentity versus agent-id is an open decision of the owner).
const commandName = "aidentity"

func main() {
	os.Exit(run(context.Background(), newRoot(), os.Args[1:], os.Stdout, os.Stderr))
}

// newRoot returns the root command of aidentity. Slices add their commands
// here as they are implemented.
func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           commandName,
		Short:         "Run AI agents under their own GitHub App identity",
		Long:          "aidentity binds an agent profile to Git identity, Git transport credentials, and GitHub API credentials for one process and its descendants. Personal Git and gh configuration is never modified.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	return withVersion(root, versionText(version, commit, debug.ReadBuildInfo))
}

// run executes root with args, writes output to stdout and diagnostics to
// stderr, and returns the exit code.
func run(ctx context.Context, root *cobra.Command, args []string, stdout, stderr io.Writer) int {
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return exitFailure
		}
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", commandName, err)
		return exitFailure
	}
	return exitOK
}
