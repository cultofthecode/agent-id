package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

const (
	botName  = "claude-dev[bot]"
	botEmail = "3+claude-dev[bot]@users.noreply.github.com"
	devName  = "Dev Person"
	devEmail = "dev@example.com"
)

const sshSigner = `echo "ssh $*" >> "$SIGNER_LOG"
for last; do :; done
printf -- '-----BEGIN SSH SIGNATURE-----\nZmFrZQ==\n-----END SSH SIGNATURE-----\n' > "$last.sig"
`

const pgpSigner = `echo "pgp $*" >> "$SIGNER_LOG"
cat > /dev/null
echo "[GNUPG:] SIG_CREATED D 1 8 00 0 0 0 F" >&2
printf -- '-----BEGIN PGP SIGNATURE-----\n\nZmFrZQ==\n-----END PGP SIGNATURE-----\n'
`

type gitFixture struct {
	*execFixture
	repo, signerLog, globalPath, repoConfig string
}

func newGitFixture(t *testing.T, format string) *gitFixture {
	t.Helper()
	f := &gitFixture{execFixture: newExecFixture(t)}
	env := f.env
	f.signerLog = filepath.Join(env.TempDir, "signer.log")
	signer := filepath.Join(env.TempDir, "signer.sh")
	body, programSection := sshSigner, "[gpg \"ssh\"]"
	if format == "openpgp" {
		body, programSection = pgpSigner, "[gpg]"
	}
	writeScript(t, signer, body)
	f.repo = filepath.Join(env.Home, "work")
	gitIn(t, env.Home, "init", "--quiet", f.repo)
	resolved, err := filepath.EvalSymlinks(f.repo)
	if err != nil {
		t.Fatal(err)
	}
	include := filepath.Join(env.Home, "included.gitconfig")
	if err := os.WriteFile(include, []byte("[user]\n\temail = included@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	global := strings.Join([]string{
		"[user]", "\tname = " + devName, "\temail = " + devEmail, "\tuseConfigOnly = true", "\tsigningKey = " + filepath.Join(env.Home, "key"),
		"[author]", "\temail = author@example.com",
		"[committer]", "\temail = committer@example.com",
		"[commit]", "\tgpgSign = true",
		"[tag]", "\tgpgSign = true", "\tforceSignAnnotated = true",
		"[gpg]", "\tformat = " + format,
		programSection, "\tprogram = " + signer,
		"[includeIf \"gitdir:" + resolved + "/\"]", "\tpath = " + include, "",
	}, "\n")
	f.globalPath = env.GitConfig
	if err := os.WriteFile(f.globalPath, []byte(global), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.Home, "key"), []byte("fake key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.repoConfig = filepath.Join(f.repo, ".git", "config")
	gitIn(t, f.repo, "config", "commit.gpgSign", "true")
	f.workdir = f.repo
	f.extra = []string{"SIGNER_LOG=" + f.signerLog}
	t.Setenv("SIGNER_LOG", f.signerLog)
	return f
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func (f *gitFixture) agent(script string) execRun {
	f.t.Helper()
	return f.run("exec", "--profile", "claude", "--", "sh", "-c", script)
}

func (f *gitFixture) inParent(args ...string) string {
	f.t.Helper()
	return testenv.Git(f.t, f.repo, args...)
}

func TestExecGitIdentityAndSigning(t *testing.T) {
	for _, format := range []string{"ssh", "openpgp"} {
		t.Run(format, func(t *testing.T) {
			f := newGitFixture(t, format)
			f.extra = append(f.extra, "GIT_AUTHOR_NAME=Inherited", "GIT_COMMITTER_EMAIL=inherited@example.com",
				"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=core.abbrev", "GIT_CONFIG_VALUE_0=12", "GIT_CONFIG_KEY_1=commit.gpgSign", "GIT_CONFIG_VALUE_1=true")
			globalBefore, repoBefore := fileHash(t, f.globalPath), fileHash(t, f.repoConfig)

			r := f.agent(`git commit --allow-empty -q -m x && git tag -a v1 -m v1 && git tag -m v2 v2 &&
git config core.abbrev && git config commit.gpgSign && git config --get-all commit.gpgSign | tr '\n' ,`)
			if r.code != 0 {
				t.Fatalf("agent: %+v", r)
			}
			if got := strings.Split(strings.TrimSpace(r.stdout), "\n"); len(got) != 3 || got[0] != "12" || got[1] != "false" || got[2] != "true,true,true,false," {
				t.Errorf("configuration inside the context: %q", r.stdout)
			}
			bot := botName + " <" + botEmail + ">"
			if got := f.inParent("log", "-1", "--format=%an <%ae>|%cn <%ce>"); got != bot+"|"+bot {
				t.Errorf("identities = %q", got)
			}
			if got := readLog(t, f.signerLog); got != "" {
				t.Errorf("the signer ran inside the context: %q", got)
			}
			for _, obj := range []string{"HEAD", "v1", "v2"} {
				if out := f.inParent("cat-file", "-p", obj); strings.Contains(out, "gpgsig") || strings.Contains(out, "BEGIN") {
					t.Errorf("%s carries a signature:\n%s", obj, out)
				}
			}

			gitIn(t, f.repo, "commit", "--allow-empty", "-q", "-m", "developer")
			if got := f.inParent("log", "-1", "--format=%an <%ae>|%cn <%ce>"); strings.Contains(got, "bot") || !strings.Contains(got, devName) {
				t.Errorf("developer commit identities = %q", got)
			}
			if got := readLog(t, f.signerLog); strings.Count(got, "\n") != 1 {
				t.Errorf("signer log after the developer's commit: %q, want one call", got)
			}
			if fileHash(t, f.globalPath) != globalBefore || fileHash(t, f.repoConfig) != repoBefore {
				t.Error("a configuration file changed")
			}
		})
	}
}

func TestExecGitIdentityWithoutRepositorySigning(t *testing.T) {
	f := newGitFixture(t, "ssh")
	gitIn(t, f.repo, "config", "--unset", "commit.gpgSign")
	if r := f.agent(`git commit --allow-empty -q -m x`); r.code != 0 {
		t.Fatalf("agent: %+v", r)
	}
	if got := readLog(t, f.signerLog); got != "" {
		t.Errorf("signer ran: %q", got)
	}
}

func TestForceSignAnnotatedIsLoadBearing(t *testing.T) {
	f := newGitFixture(t, "ssh")
	cmd := exec.Command("git", "tag", "-m", "v3", "v3")
	cmd.Dir = f.repo
	cmd.Env = append(os.Environ(), "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=commit.gpgSign", "GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=tag.gpgSign", "GIT_CONFIG_VALUE_1=false", "GIT_COMMITTER_NAME=x", "GIT_COMMITTER_EMAIL=x@example.com")
	gitIn(t, f.repo, "commit", "--allow-empty", "-q", "-m", "seed", "--no-gpg-sign")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git tag: %v\n%s", err, out)
	}
	if got := readLog(t, f.signerLog); got == "" {
		t.Error("without tag.forceSignAnnotated=false the signer was not called")
	}
}

func TestExecRefusesAMalformedIdentity(t *testing.T) {
	f := newExecFixture(t)
	dir := filepath.Join(f.env.ConfigHome, "agent-id")
	bad := strings.Replace(execConfig, `"login":"claude-dev[bot]"`, `"login":"claude\nmallory"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(f.env.TempDir, "ran")
	r := f.run("exec", "--profile", "claude", "--", "touch", marker)
	if r.code != 125 || strings.Contains(r.stderr, "mallory") {
		t.Errorf("%+v", r)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the command ran")
	}
}
