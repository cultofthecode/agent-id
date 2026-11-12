package testenv

import (
	"bytes"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// seedIdentity is the author and committer of the first commit of a bare
// repository: test data, never a real person.
var seedIdentity = []string{"-c", "user.name=aidentity test", "-c", "user.email=test@aidentity.invalid"}

// Git runs git with args in dir, in the environment of the test, and returns
// its standard output without the trailing newline. It fails the test with
// git's output when git fails, and skips the test when git is not installed.
// Call Isolate first.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("testenv: git is not on PATH")
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(git, args...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("testenv: git %s: %v\n%s%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return strings.TrimSuffix(stdout.String(), "\n")
}

// RepositoriesDir returns the directory of the bare repositories of env, which
// NewGitServer serves.
func RepositoriesDir(env Env) string {
	return filepath.Join(env.Root, "repos")
}

// NewBareRepository creates the bare repository name.git in
// RepositoriesDir(env), with one commit on main, and returns its path.
func NewBareRepository(t testing.TB, env Env, name string) string {
	t.Helper()
	if !validRepositoryName(name) {
		t.Fatalf("testenv: invalid repository name %q", name)
	}
	bare := filepath.Join(RepositoriesDir(env), name+".git")
	if err := os.MkdirAll(RepositoriesDir(env), 0o700); err != nil {
		t.Fatalf("testenv: create %s: %v", RepositoriesDir(env), err)
	}
	Git(t, env.Root, "init", "--quiet", "--bare", "--initial-branch=main", bare)
	seed, err := os.MkdirTemp(env.TempDir, "seed-")
	if err != nil {
		t.Fatalf("testenv: create a seed directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(seed) }()
	Git(t, seed, "init", "--quiet", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("# "+name+"\n"), 0o600); err != nil {
		t.Fatalf("testenv: write the first file of %s: %v", name, err)
	}
	Git(t, seed, "add", "README.md")
	Git(t, seed, append(seedIdentity, "commit", "--quiet", "-m", "Initial commit")...)
	Git(t, seed, "push", "--quiet", bare, "main")
	return bare
}

// validRepositoryName reports whether name is a single path element that does
// not start with a dot, so that a repository stays in RepositoriesDir.
func validRepositoryName(name string) bool {
	return name != "" && name == filepath.Base(name) && !strings.HasPrefix(name, ".")
}

// Clone clones remote into env.Home/src and returns the path of the clone.
// The clone is named after the last element of remote, without ".git".
func Clone(t testing.TB, env Env, remote string) string {
	t.Helper()
	name := strings.TrimSuffix(path.Base(filepath.ToSlash(remote)), ".git")
	src := filepath.Join(env.Home, "src")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatalf("testenv: create %s: %v", src, err)
	}
	dir := filepath.Join(src, name)
	Git(t, src, "clone", "--quiet", remote, dir)
	return dir
}
