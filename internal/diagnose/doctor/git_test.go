package doctor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestExecGitReadsTheRemotesAsGitResolvesThem(t *testing.T) {
	env := testenv.Isolate(t)
	repo := filepath.Join(env.Root, "work")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	testenv.Git(t, repo, "init", "-q")
	testenv.Git(t, repo, "remote", "add", "origin", "https://github.com/o/r.git")
	testenv.Git(t, repo, "config", "--global", "url.git@github.com:.insteadOf", "https://github.com/")
	g := ExecGit{Dir: repo, Env: os.Environ()}

	urls, inRepo, err := g.RemoteURLs(context.Background())
	if err != nil || !inRepo || !slices.Equal(urls, []string{"git@github.com:o/r.git"}) {
		t.Errorf("RemoteURLs = %v, %v, %v; want the rewritten SSH URL", urls, inRepo, err)
	}
	version, err := g.Version(context.Background())
	if err != nil || version == "" {
		t.Errorf("Version = %q, %v", version, err)
	}

	outside := ExecGit{Dir: env.Home, Env: os.Environ()}
	if urls, inRepo, err := outside.RemoteURLs(context.Background()); err != nil || inRepo || urls != nil {
		t.Errorf("outside a repository: %v, %v, %v; want not in a repository", urls, inRepo, err)
	}
}

func TestExecGitReportsAFailureThatIsNotAMissingRepository(t *testing.T) {
	env := testenv.Isolate(t)
	bin := filepath.Join(env.Root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/sh\necho \"fatal: detected dubious ownership in repository\" >&2\nexit 128\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	g := ExecGit{Dir: env.Home, Env: os.Environ()}
	urls, inRepo, err := g.RemoteURLs(context.Background())
	if err == nil || inRepo || urls != nil {
		t.Errorf("git exits 128 with dubious ownership: %v, %v, %v; want a gatherer error, not \"not in a repository\"", urls, inRepo, err)
	}
	if err != nil && strings.Contains(err.Error(), "dubious") {
		t.Errorf("the error echoes git's output: %v", err)
	}
}
