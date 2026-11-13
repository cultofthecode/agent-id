package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/integration/claude"
)

const botEmailForClaude = "3+claude-dev[bot]@users.noreply.github.com"

type sh struct {
	stdout, stderr string
	code           int
}

type hookFixture struct {
	*execFixture
	repo, envFile string
	scanned       []string
}

func newHookFixture(t *testing.T) *hookFixture {
	t.Helper()
	f := &hookFixture{execFixture: newExecFixture(t)}
	f.repo = filepath.Join(f.env.Home, "work")
	gitIn(t, f.env.Home, "init", "--quiet", f.repo)
	f.envFile = filepath.Join(f.env.TempDir, "claude-env.sh")
	f.extra = append(f.extra, "GH_TOKEN="+fakePersonal, "SHELL=/bin/bash")
	return f
}

func (f *hookFixture) shell(shell, stdin, script string, extra ...string) sh {
	f.t.Helper()
	path, err := exec.LookPath(shell)
	if err != nil {
		f.t.Skipf("%s not installed", shell)
	}
	args := []string{"-c", script}
	if shell == "zsh" {
		args = []string{"-f", "-c", script}
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = f.repo
	cmd.Env = append(append(os.Environ(), f.extra...), extra...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err = cmd.Run()
	r := sh{stdout: out.String(), stderr: errb.String()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		r.code = ee.ExitCode()
	} else if err != nil {
		f.t.Fatal(err)
	}
	f.scanned = append(f.scanned, r.stdout, r.stderr)
	return r
}

func hookJSON(f *hookFixture, source string) string {
	b, _ := json.Marshal(map[string]string{"hook_event_name": "SessionStart", "source": source, "cwd": f.repo})
	return string(b)
}

func (f *hookFixture) sessionStart(profile, source string) sh {
	f.t.Helper()
	return f.shell("sh", hookJSON(f, source), `"$BIN" claude-code session-start --profile "$PROFILE"`,
		"BIN="+f.binary, "PROFILE="+profile, "CLAUDE_ENV_FILE="+f.envFile)
}

func (f *hookFixture) command(shell, command string, pre ...string) sh {
	f.t.Helper()
	return f.shell(shell, "", strings.Join(pre, "\n")+"\n. \"$CLAUDE_ENV_FILE\"\n"+command, "CLAUDE_ENV_FILE="+f.envFile, "BIN="+f.binary)
}

func (f *hookFixture) noTokens() {
	f.t.Helper()
	data, _ := os.ReadFile(f.envFile)
	all := append(f.scanned, string(data))
	for _, s := range all {
		if secretShapes.MatchString(strings.ReplaceAll(s, fakePersonal, "")) {
			f.t.Errorf("token-shaped text in an output or the env file")
		}
	}
}

func envMap(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if n, v, ok := strings.Cut(line, "="); ok && regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(n) {
			m[n] = v
		}
	}
	return m
}

func TestEnvEvalEqualsExec(t *testing.T) {
	f := newHookFixture(t)
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			exe := f.shell(shell, "", `"$BIN" exec --profile claude -- env`, "BIN="+f.binary)
			evl := f.shell(shell, "", `eval "$("$BIN" env --profile claude --shell `+shell+`)" && env`, "BIN="+f.binary)
			if exe.code != 0 || evl.code != 0 {
				t.Fatalf("exec %+v eval %+v", exe, evl)
			}
			a, b := envMap(exe.stdout), envMap(evl.stdout)
			if a["AGENT_ID_MECHANISM"] != "exec" || b["AGENT_ID_MECHANISM"] != "env" {
				t.Fatalf("mechanisms %q %q", a["AGENT_ID_MECHANISM"], b["AGENT_ID_MECHANISM"])
			}
			for _, n := range []string{"AGENT_ID_MECHANISM", "PWD", "SHLVL", "_", "OLDPWD"} {
				delete(a, n)
				delete(b, n)
			}
			for n, v := range a {
				if b[n] != v {
					t.Errorf("%s: exec %q, eval %q", n, v, b[n])
				}
			}
			for n := range b {
				if _, ok := a[n]; !ok {
					t.Errorf("%s only in eval", n)
				}
			}
			if _, ok := b["GH_TOKEN"]; ok && b["GH_TOKEN"] == fakePersonal {
				t.Error("personal token survived the eval")
			}
		})
	}
	f.noTokens()
}

func TestEnvOutputAndErrors(t *testing.T) {
	f := newHookFixture(t)
	r := f.run("env", "--profile", "claude", "--shell", "bash")
	if r.code != 0 || !strings.Contains(r.stdout, "unset ") || !strings.Contains(r.stdout, "GH_TOKEN") || r.stderr != "" {
		t.Fatalf("%+v", r)
	}
	if !strings.HasPrefix(r.stdout, "# agent-id env 1 profile=claude\n") || strings.Contains(r.stdout, "-----BEGIN") {
		t.Fatalf("block:\n%s", r.stdout)
	}
	tests := []struct {
		name   string
		args   []string
		parent []string
		stderr string
	}{
		{"fish", []string{"env", "--profile", "claude", "--shell", "fish"}, nil, `unsupported shell "fish"; use bash or zsh`},
		{"no shell", []string{"env", "--profile", "claude"}, nil, "--shell is required"},
		{"no profile", []string{"env", "--shell", "bash"}, nil, "--profile is required"},
		{"unknown flag", []string{"env", "--bogus"}, nil, "unknown flag"},
		{"extra argument", []string{"env", "--profile", "claude", "--shell", "bash", "x"}, nil, "unexpected argument"},
		{"unknown profile", []string{"env", "--profile", "nobody", "--shell", "bash"}, nil, `profile "nobody" not found`},
		{"no identity", []string{"env", "--profile", "reviewer", "--shell", "bash"}, nil, "agent-id setup github --profile reviewer"},
		{"different profile inside a session", []string{"env", "--profile", "other", "--shell", "zsh"},
			[]string{"AGENT_ID_CONTEXT=1", "AGENT_ID_ACTIVE_PROFILE=claude", "AGENT_ID_GIT_CONFIG_BASE=0", "AGENT_ID_GIT_CONFIG_END=0", "AGENT_ID_PATH_PREPEND=", "AGENT_ID_SET_NAMES="}, `"claude"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.extra = append(f.extra[:0:0], tt.parent...)
			r := f.run(tt.args...)
			if r.code != 125 || r.stdout != "" || !strings.Contains(r.stderr, tt.stderr) {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestEnvHostilePathStaysLiteral(t *testing.T) {
	f := newHookFixture(t)
	hostile := "/tmp/x $(touch pwned)'\"; \\n*"
	for _, shell := range []string{"bash", "zsh"} {
		f.shell(shell, "", `PATH="$PATH:$HOSTILE"; eval "$("$BIN" env --profile claude --shell `+shell+`)" && [ "${PATH##*:}" = "$HOSTILE" ] && printf ok`, "BIN="+f.binary, "HOSTILE="+hostile)
		if _, err := os.Stat(filepath.Join(f.repo, "pwned")); err == nil {
			t.Fatalf("%s: injected file", shell)
		}
	}
}

func TestSessionStartSimulatedSession(t *testing.T) {
	f := newHookFixture(t)
	if err := os.WriteFile(f.envFile, []byte("export FROM_OTHER_HOOK='1'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	helperLog := filepath.Join(f.env.TempDir, "helper.log")
	helper := filepath.Join(f.env.TempDir, "helper.sh")
	writeScript(t, helper, `echo called >> "`+helperLog+`"`+"\n")
	gitIn(t, f.repo, "config", "credential.helper", "!"+helper)
	gitIn(t, f.repo, "config", "user.name", "Dev")
	gitIn(t, f.repo, "config", "user.email", "dev@example.com")
	f.extra = append(f.extra, "GIT_AUTHOR_NAME=Inherited")

	if r := f.sessionStart("claude", "startup"); r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("startup: %+v", r)
	}
	if r := f.sessionStart("claude", "resume"); r.code != 0 || r.stdout != "" {
		t.Fatalf("resume: %+v", r)
	}
	data, _ := os.ReadFile(f.envFile)
	if !strings.HasPrefix(string(data), "export FROM_OTHER_HOOK='1'\n") || strings.Count(string(data), claude.BlockStart) != 1 {
		t.Fatalf("env file:\n%s", data)
	}
	decoyFn := "gh() { GH_TOKEN=decoy command gh \"$@\"; }\ngit() { echo decoy-git; }"
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			r := f.command(shell, `git commit --allow-empty -q -m x && git log -1 --format='%an <%ae>|%cn <%ce>' && echo "other=$FROM_OTHER_HOOK tok=$GH_TOKEN mech=$AGENT_ID_MECHANISM" && gh api repos/o/r; echo "gh=$?"`, decoyFn)
			bot := "claude-dev[bot] <" + botEmailForClaude + ">"
			if r.code != 0 || !strings.Contains(r.stdout, bot+"|"+bot) || strings.Contains(r.stdout, "decoy") || !strings.Contains(r.stdout, "other=1 tok=agent-id-no-token mech=env") || !strings.Contains(r.stdout, "gh=1") {
				t.Fatalf("%+v", r)
			}
			if !strings.Contains(r.stderr, "gh is not available in this agent context") {
				t.Errorf("gh did not reach the refusing stub: %q", r.stderr)
			}
			nested := f.command(shell, `"$BIN" exec --profile claude -- env`)
			direct := f.command(shell, `env`)
			a, b := envMap(nested.stdout), envMap(direct.stdout)
			for _, n := range []string{"GIT_CONFIG_COUNT", "AGENT_ID_GIT_CONFIG_END", "PATH"} {
				if a[n] != b[n] || a[n] == "" {
					t.Errorf("%s: nested %q direct %q", n, a[n], b[n])
				}
			}
			shim := strings.Split(a["PATH"], ":")[0]
			if strings.Count(":"+a["PATH"]+":", ":"+shim+":") != 1 {
				t.Errorf("shim directory repeated in PATH %q", a["PATH"])
			}
			other := f.command(shell, `"$BIN" exec --profile other -- true`)
			if other.code != 125 || !strings.Contains(other.stderr, `"claude"`) || !strings.Contains(other.stderr, `"other"`) {
				t.Errorf("different profile: %+v", other)
			}
		})
	}
	if got := readLog(t, helperLog); got != "" {
		t.Errorf("the personal credential helper was called: %q", got)
	}
	f.noTokens()
}

func TestSessionStartFailClosed(t *testing.T) {
	f := newHookFixture(t)
	for _, shell := range []string{"bash", "zsh", "sh"} {
		for _, profile := range []string{"reviewer", "a';x", "nobody"} {
			t.Run(shell+"/"+profile, func(t *testing.T) {
				if err := os.WriteFile(f.envFile, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				r := f.sessionStart(profile, "startup")
				if r.code != 3 || r.stdout != "" || r.stderr == "" {
					t.Fatalf("session-start: %+v", r)
				}
				data, _ := os.ReadFile(f.envFile)
				if strings.Count(string(data), claude.RefusalMark) != 1 || (profile == "a';x" && strings.Contains(string(data), "a';x")) {
					t.Fatalf("env file:\n%s", data)
				}
				c := f.command(shell, "echo ran; touch pwned")
				if c.code != 125 || strings.Contains(c.stdout, "ran") {
					t.Fatalf("command: %+v", c)
				}
				if profile == "reviewer" && !strings.Contains(c.stderr, "agent-id setup github --profile reviewer") {
					t.Errorf("no setup hint: %q", c.stderr)
				}
				if _, err := os.Stat(filepath.Join(f.repo, "pwned")); err == nil {
					t.Fatal("injected effect")
				}
			})
		}
	}
	f.noTokens()
}

func TestSessionStartNotAHookAndUnsafeFiles(t *testing.T) {
	f := newHookFixture(t)
	r := f.shell("sh", hookJSON(f, "startup"), `"$BIN" claude-code session-start --profile claude`, "BIN="+f.binary, "CLAUDE_ENV_FILE=")
	if r.code != 4 || !strings.Contains(r.stderr, "must run as a Claude Code SessionStart command hook") {
		t.Fatalf("%+v", r)
	}
	target := filepath.Join(f.env.TempDir, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, f.envFile); err != nil {
		t.Fatal(err)
	}
	if r := f.sessionStart("claude", "startup"); r.code != 4 || !strings.Contains(r.stderr, "symbolic link") {
		t.Fatalf("symlink: %+v", r)
	}
	if data, _ := os.ReadFile(target); len(data) != 0 {
		t.Fatal("the target was written")
	}
	other := filepath.Join(f.env.TempDir, "bad.sh")
	if err := os.WriteFile(other, []byte("echo 'open\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.envFile = other
	if r := f.sessionStart("claude", "startup"); r.code != 4 {
		t.Fatalf("unparseable: %+v", r)
	}
}

var settingsHook = regexp.MustCompile("(?s)```json\n(.*?)```")

func readmeHookCommand(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "integration", "claude", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks struct {
			SessionStart []struct {
				Hooks []struct{ Type, Command string }
			}
		}
	}
	if err := json.Unmarshal([]byte(settingsHook.FindStringSubmatch(string(data))[1]), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Hooks.SessionStart[0].Hooks[0].Command
}

func TestReadmeHookCommandFailsClosedWhenAgentIDCannotRun(t *testing.T) {
	f := newHookFixture(t)
	command := readmeHookCommand(t)
	if !strings.Contains(command, "agent-id claude-code session-start --profile claude") {
		t.Fatalf("command: %s", command)
	}
	empty := t.TempDir()
	for _, shell := range []string{"bash", "zsh"} {
		if err := os.WriteFile(f.envFile, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		r := f.shell("sh", hookJSON(f, "startup"), command, "PATH=/usr/bin:/bin:"+empty, "CLAUDE_ENV_FILE="+f.envFile)
		data, _ := os.ReadFile(f.envFile)
		if r.code != 1 || !strings.Contains(string(data), claude.RefusalMark) || strings.Count(string(data), "\n") != 1 {
			t.Fatalf("hook: %+v\n%s", r, data)
		}
		c := f.command(shell, "echo ran")
		if c.code != 125 || strings.Contains(c.stdout, "ran") || !strings.Contains(c.stderr, "the SessionStart hook could not run agent-id") {
			t.Fatalf("%s command: %+v", shell, c)
		}
	}
	if err := os.WriteFile(f.envFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(f.env.TempDir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.binary, filepath.Join(binDir, "agent-id")); err != nil {
		t.Fatal(err)
	}
	r := f.shell("sh", hookJSON(f, "startup"), command, "PATH="+binDir+":/usr/bin:/bin", "CLAUDE_ENV_FILE="+f.envFile)
	if r.code != 0 {
		t.Fatalf("hook with agent-id on PATH: %+v", r)
	}
	if err := os.WriteFile(f.envFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r = f.shell("sh", "", strings.Replace(command, "session-start --profile claude", "session-start --bogus", 1), "PATH="+binDir+":/usr/bin:/bin", "CLAUDE_ENV_FILE="+f.envFile)
	if data, _ := os.ReadFile(f.envFile); r.code != 1 || !strings.Contains(string(data), claude.RefusalMark) {
		t.Fatalf("usage error: %+v\n%s", r, data)
	}
	f.noTokens()
}

func TestReadonlyTokenStopsTheCommand(t *testing.T) {
	f := newHookFixture(t)
	if r := f.sessionStart("claude", "startup"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	b := f.command("bash", "echo ran", "readonly GH_TOKEN")
	if b.code != 125 || strings.Contains(b.stdout, "ran") || !strings.Contains(b.stderr, "agent activation failed") {
		t.Fatalf("bash: %+v", b)
	}
	z := f.command("zsh", "echo ran", "readonly GH_TOKEN")
	if z.code != 125 || strings.Contains(z.stdout, "ran") || !strings.Contains(z.stderr, "GH_TOKEN") || !strings.Contains(z.stderr, "agent activation failed") {
		t.Fatalf("zsh: %+v", z)
	}
	same := f.shell("bash", "", "alias gh='echo decoy'; . \"$CLAUDE_ENV_FILE\"; gh x", "CLAUDE_ENV_FILE="+f.envFile)
	t.Logf("bash alias on the same line as the env file: %q (documented limitation)", strings.TrimSpace(same.stdout))
	sep := f.shell("bash", "", "alias gh='echo decoy'\n. \"$CLAUDE_ENV_FILE\"\ngh x", "CLAUDE_ENV_FILE="+f.envFile)
	if strings.Contains(sep.stdout, "decoy") && !strings.Contains(sep.stderr, "not available") {
		t.Logf("alias survived on separate lines in non-interactive bash (aliases are off): %+v", sep)
	}
}
