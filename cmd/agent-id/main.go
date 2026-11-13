// Command agent-id gives AI coding agents their own GitHub identity for one
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
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cultofthecode/agent-id/internal/credential"
	"github.com/cultofthecode/agent-id/internal/diagnose"
	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/github"
	"github.com/cultofthecode/agent-id/internal/setup"
)

// Exit codes of agent-id.
const (
	exitOK      = 0
	exitFailure = 1
)

// commandName is the name of the binary (ARCHITECTURE.md, Key decisions).
const commandName = "agent-id"

// main runs agent-id with a context that the first SIGINT or SIGTERM
// cancels, so that an interrupted command fails closed with exit status 1.
// The handler is removed on that first signal, so a second one ends a
// command that is blocked, for example on the configuration lock.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, stop)
	code := run(ctx, newRoot(), os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// newRoot returns the production root command of agent-id: the GitHub
// onboarder, which loads keys through the credential source, and the
// profile store at the default location.
func newRoot() *cobra.Command {
	return buildRoot([]provider.Onboarder{github.NewOnboarder(loadKey)}, openStore)
}

// buildRoot returns the root command of agent-id with the onboarders of
// setup and the store opener. Slices add their commands here as they are
// implemented; tests build a root with fakes.
func buildRoot(onboarders []provider.Onboarder, openStore func() (setup.Store, error)) *cobra.Command {
	root := &cobra.Command{
		Use:           commandName,
		Short:         "Run AI agents under their own GitHub App identity",
		Long:          "agent-id binds an agent profile to Git identity, Git transport credentials, and GitHub API credentials for one process and its descendants. Personal Git and gh configuration is never modified.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.AddCommand(setup.NewCommand(&setup.Handler{Onboarders: onboarders, OpenStore: openStore}))
	root.AddCommand(diagnosticCommands(github.ProfileRegistration(), openProvider)...)
	return withVersion(root, versionText(version, commit, debug.ReadBuildInfo))
}

// openStore returns the production profile store: the configuration at the
// default location with the production registry.
func openStore() (setup.Store, error) {
	store, err := openProfileStore()
	if err != nil {
		return nil, err
	}
	return store, nil
}

// openProfileStore returns the profile store at the default location.
func openProfileStore() (*profile.Store, error) {
	registry, err := newRegistry()
	if err != nil {
		return nil, err
	}
	path, err := profile.Path(os.Getenv)
	if err != nil {
		return nil, err
	}
	return profile.NewStore(path, registry), nil
}

// openProvider returns the provider of a profile. The credential source
// loads the key, so that a key problem keeps its class.
func openProvider(p profile.Profile) (provider.Provider, error) {
	if p.Provider != github.ProfileRegistration().Name {
		return nil, fmt.Errorf("provider %s is not supported by this agent-id", profile.Quote(p.Provider))
	}
	return github.FromProfile(p, loadKey)
}

// loadKey loads the private key that ref names through the credential
// source, which refuses a file that is not a protected regular file of the
// current user.
func loadKey(ref profile.KeyRef) ([]byte, error) {
	path, err := ref.Path(os.Getenv("HOME"))
	if err != nil {
		return nil, err
	}
	return credential.File{Path: path}.PrivateKeyPEM()
}

// newRegistry returns the providers this agent-id supports, for the profile
// store: the GitHub adapter's validator of the github block and its token
// prefixes, which the inline-secret scan rejects anywhere in the file.
func newRegistry() (*profile.Registry, error) {
	return profile.NewRegistry(github.ProfileRegistration())
}

// exitWith writes the message of a diagnostic command's exit error, which
// has already passed Redact and Guard, and returns its exit code. An error
// without a message was reported on stdout.
func exitWith(stderr io.Writer, exit *diagnose.ExitError) int {
	if exit.Message != "" {
		_, _ = fmt.Fprintf(stderr, "%s: %s\n", commandName, exit.Message)
	}
	return exit.Code
}

// run executes root with args, writes output to stdout and diagnostics to
// stderr, and returns the exit code.
func run(ctx context.Context, root *cobra.Command, args []string, stdout, stderr io.Writer) int {
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		var exit *diagnose.ExitError
		if errors.As(err, &exit) {
			return exitWith(stderr, exit)
		}
		if errors.Is(err, context.Canceled) {
			return exitFailure
		}
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", commandName, err)
		return exitFailure
	}
	return exitOK
}
