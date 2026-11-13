// Package doctor is the doctor slice: it runs the catalog of checks of
// internal/diagnose for one profile and prints the report as text or JSON
// (spec .ai/specs/2026-11-13-whoami-doctor-output.md). The gatherers collect
// a Snapshot through narrow ports; the checks are pure functions of it. The
// checks that wait for exec, the Git integration, and the gh shim are
// reported as skip until those slices merge (diagnose.DeferredChecks).
package doctor

import (
	"context"

	"github.com/cultofthecode/agent-id/internal/profile"
)

// Input is one doctor request.
type Input struct {
	Profile string
	JSON    bool
}

// Store is the part of the profile store that doctor uses. Write is used for
// one thing only: refreshing the cached identity of the profile.
type Store interface {
	Load() (profile.Config, error)
	Resolve(name string) (profile.Profile, error)
	Write(change func(*profile.Config) error) error
	Path() string
}

// Git is what doctor asks of the git program.
type Git interface {
	// Version returns the output of git --version.
	Version(ctx context.Context) (string, error)
	// RemoteURLs returns the effective fetch URL of every remote of the
	// repository in the working directory, as Git resolves them with its
	// rewrite rules. inRepo is false outside a repository. The URLs may hold
	// credentials: the caller keeps only the repository they name.
	RemoteURLs(ctx context.Context) (urls []string, inRepo bool, err error)
}
