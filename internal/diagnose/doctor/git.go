package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// gathererTimeout bounds one git run.
const gathererTimeout = 10 * time.Second

// ExecGit runs the git program in Dir, with the environment Env, which the
// composition root sets to the environment of doctor itself. It never reads
// a configuration value: it asks for the version and the remote URLs only.
type ExecGit struct {
	Dir string
	Env []string
}

// Version implements Git.
func (g ExecGit) Version(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "--version")
	return strings.TrimSpace(out), err
}

// RemoteURLs implements Git. The URLs are as Git resolves them, with its
// rewrite rules, so that a personal insteadOf rule to SSH shows.
func (g ExecGit) RemoteURLs(ctx context.Context) ([]string, bool, error) {
	if stderr, err := g.runStderr(ctx, "rev-parse", "--git-dir"); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && strings.Contains(stderr, "not a git repository") {
			return nil, false, nil
		}
		return nil, false, err
	}
	names, err := g.run(ctx, "remote")
	if err != nil {
		return nil, true, err
	}
	var urls []string
	for _, name := range strings.Fields(names) {
		out, err := g.run(ctx, "remote", "get-url", "--", name)
		if err != nil {
			return nil, true, err
		}
		urls = append(urls, strings.TrimSpace(out))
	}
	return urls, true, nil
}

// run runs git with args under the gatherer timeout. The error names the
// failed command, never its output, which could hold a credential.
func (g ExecGit) run(ctx context.Context, args ...string) (string, error) {
	out, _, err := g.exec(ctx, args...)
	return out, err
}

// runStderr is run for a command whose failure must be told apart by what
// git says: it returns stderr, which the caller only matches and never
// shows.
func (g ExecGit) runStderr(ctx context.Context, args ...string) (string, error) {
	_, stderr, err := g.exec(ctx, args...)
	return stderr, err
}

// exec runs git with args under the gatherer timeout, in the C locale so
// that the messages that callers match do not depend on the user's language.
func (g ExecGit) exec(ctx context.Context, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, gathererTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.Dir
	cmd.Env = append(append([]string(nil), g.Env...), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", stderr.String(), fmt.Errorf("git %s: %w", args[0], err)
	}
	return stdout.String(), "", nil
}
