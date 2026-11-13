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

	"github.com/cultofthecode/agent-id/internal/activation"
	activationexec "github.com/cultofthecode/agent-id/internal/activation/exec"
	"github.com/cultofthecode/agent-id/internal/credential"
	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/github"
	"github.com/cultofthecode/agent-id/internal/setup"
)

// exitCoder is implemented by an error that carries the exit code of the
// process, as the exec slice's errors do (125, 126, 127).
type exitCoder interface{ ExitCode() int }

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
	return buildRootWith([]provider.Onboarder{github.NewOnboarder(loadKey)}, openStore, newExecHandler())
}

// newExecHandler returns the production handler of exec: the profile store
// at the default location, the GitHub adapter's exec-time view of a profile,
// the neutral directories of the file system, and git and ssh for the remote
// checks. exec replaces the process with the command.
func newExecHandler() *activationexec.Handler {
	return &activationexec.Handler{
		Deps: activation.Deps{
			Profiles:  lazyProfiles{},
			Providers: productionProviders{},
			Neutral:   activation.FileNeutralDirs{},
			Checks: func(a provider.Activator) []activation.Check {
				return activation.NewChecks(a, activation.ExecRunner{})
			},
		},
		Execer: activationexec.SyscallExecer{},
		Binary: func() (string, error) { return activation.ResolveBinary(os.Args[0], os.Getenv("PATH")) },
	}
}

// lazyProfiles opens the profile store only when exec runs, so that a broken
// environment fails exec and nothing else.
type lazyProfiles struct{}

func (lazyProfiles) open() (*profile.Store, error) {
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

func (l lazyProfiles) CheckName(name string) error {
	store, err := l.open()
	if err != nil {
		return err
	}
	return store.CheckName(name)
}

func (l lazyProfiles) Resolve(name string) (profile.Profile, error) {
	store, err := l.open()
	if err != nil {
		return profile.Profile{}, err
	}
	return store.Resolve(name)
}

// productionProviders builds the exec-time view of a profile from the adapter
// of its provider. exec removes the provider's credential variables from the
// child, so a provider without an adapter here must not start: fail closed.
type productionProviders struct{}

func (productionProviders) Activator(p profile.Profile) (provider.Activator, error) {
	if p.Provider != "github" {
		return nil, fmt.Errorf("provider %q is not supported by this agent-id", p.Provider)
	}
	return github.NewActivator(p)
}

// buildRoot returns the root command of agent-id with the onboarders of
// setup and the store opener. Slices add their commands here as they are
// implemented; tests build a root with fakes.
func buildRoot(onboarders []provider.Onboarder, openStore func() (setup.Store, error)) *cobra.Command {
	return buildRootWith(onboarders, openStore, newExecHandler())
}

// buildRootWith is buildRoot with the handler of exec, so that tests can
// run exec with fakes.
func buildRootWith(onboarders []provider.Onboarder, openStore func() (setup.Store, error), execHandler *activationexec.Handler) *cobra.Command {
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
	root.AddCommand(activationexec.NewCommand(execHandler))
	return withVersion(root, versionText(version, commit, debug.ReadBuildInfo))
}

// openStore returns the production profile store: the configuration at the
// default location with the production registry.
func openStore() (setup.Store, error) {
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
		var coded exitCoder
		if errors.As(err, &coded) {
			return coded.ExitCode()
		}
		return exitFailure
	}
	return exitOK
}
