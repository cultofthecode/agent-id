package sessionstart

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cultofthecode/agent-id/internal/activation"
	"github.com/cultofthecode/agent-id/internal/integration/claude"
	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

type profiles struct{ err error }

func (p profiles) CheckName(string) error { return nil }
func (p profiles) Resolve(n string) (profile.Profile, error) {
	return profile.Profile{Name: n, Provider: "fake", Host: providertest.FakeHost}, p.err
}

type source struct{ a *providertest.FakeActivator }

func (s source) Activator(profile.Profile) (provider.Activator, error) { return s.a, nil }

type neutral struct{ dir string }

func (n neutral) Ensure([]string, string, provider.NeutralCLI) (activation.NeutralPaths, error) {
	return activation.NeutralPaths{ConfigDir: n.dir, BinDir: n.dir}, nil
}

func newHandler(t *testing.T) *Handler {
	t.Helper()
	return &Handler{
		Deps: activation.Deps{
			Profiles: profiles{}, Providers: source{providertest.NewFakeActivator()}, Neutral: neutral{t.TempDir()},
			Checks: func(provider.Activator) []activation.Check { return nil },
		},
		Binary: func() (string, error) { return "/bin/agent-id", nil },
	}
}

const hook = `{"hook_event_name":"SessionStart","source":"startup","cwd":"/"}`

func input(file string) Input {
	return Input{
		Profile: "claude", HookInput: []byte(hook), EnvFile: file,
		Parent: []string{"PATH=/usr/bin:/bin", "SHELL=/bin/bash", "FAKE_CLI_TOKEN=" + testenv.FakeToken("personal")},
	}
}

func envFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env.sh")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func code(err error) int {
	var coded interface{ ExitCode() int }
	if errors.As(err, &coded) {
		return coded.ExitCode()
	}
	return 0
}

func runCommand(t *testing.T, shell, file, command string) (string, int) {
	t.Helper()
	path, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s not installed", shell)
	}
	args := []string{"-c", `. "$CLAUDE_ENV_FILE"; ` + command}
	if shell == "zsh" {
		args = append([]string{"-f"}, args...)
	}
	cmd := exec.Command(path, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "CLAUDE_ENV_FILE=" + file, "FAKE_CLI_TOKEN=" + testenv.FakeToken("personal")}
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	c := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		c = ee.ExitCode()
	}
	return string(out), c
}

func TestStartupThenResumeAppendsOneBlockAndKeepsOtherLines(t *testing.T) {
	testenv.Isolate(t)
	other := "export OTHER_HOOK='1'\n"
	file := envFile(t, other, 0o600)
	h := newHandler(t)
	if err := h.Handle(context.Background(), input(file)); err != nil {
		t.Fatal(err)
	}
	resume := input(file)
	resume.HookInput = []byte(`{"hook_event_name":"SessionStart","source":"resume","cwd":"/"}`)
	if err := h.Handle(context.Background(), resume); err != nil {
		t.Fatal(err)
	}
	got := read(t, file)
	if !strings.HasPrefix(got, other) || strings.Count(got, claude.BlockStart) != 1 || strings.Count(got, claude.BlockEnd) != 1 {
		t.Fatalf("file:\n%s", got)
	}
	if strings.Contains(got, testenv.FakeToken("personal")) || strings.Contains(got, testenv.FakeTokenPrefix) {
		t.Fatal("token in the env file")
	}
	for _, shell := range []string{"bash", "zsh"} {
		out, c := runCommand(t, shell, file, `echo "mech=$AGENT_ID_MECHANISM tok=$FAKE_CLI_TOKEN other=$OTHER_HOOK"`)
		if c != 0 || !strings.Contains(out, "mech=env tok=fake-no-token other=1") {
			t.Fatalf("%s: %d %q", shell, c, out)
		}
	}
}

func TestUnknownSourceIsAccepted(t *testing.T) {
	testenv.Isolate(t)
	file := envFile(t, "", 0o600)
	in := input(file)
	in.HookInput = []byte(`{"hook_event_name":"SessionStart","source":"from-the-future","cwd":"/"}`)
	if err := newHandler(t).Handle(context.Background(), in); err != nil {
		t.Fatal(err)
	}
}

func TestRefusalsGiveExit3AndStopCommands(t *testing.T) {
	testenv.Isolate(t)
	tests := []struct {
		name   string
		mutate func(h *Handler, in *Input)
		msg    string
	}{
		{"bad json", func(_ *Handler, in *Input) { in.HookInput = []byte("{") }, "not a JSON object"},
		{"empty input", func(_ *Handler, in *Input) { in.HookInput = nil }, "not a JSON object"},
		{"wrong event", func(_ *Handler, in *Input) { in.HookInput = []byte(`{"hook_event_name":"Stop","cwd":"/"}`) }, "not a SessionStart"},
		{"no cwd", func(_ *Handler, in *Input) { in.HookInput = []byte(`{"hook_event_name":"SessionStart"}`) }, "absolute cwd"},
		{"relative cwd", func(_ *Handler, in *Input) { in.HookInput = []byte(`{"hook_event_name":"SessionStart","cwd":"x"}`) }, "absolute cwd"},
		{"numeric cwd", func(_ *Handler, in *Input) { in.HookInput = []byte(`{"hook_event_name":"SessionStart","cwd":5}`) }, "absolute cwd"},
		{"too large", func(_ *Handler, in *Input) { in.HookInput = []byte(strings.Repeat(" ", MaxHookInput+1)) }, "larger than 64 KiB"},
		{"hostile profile", func(_ *Handler, in *Input) { in.Profile = "a';x" }, "profile name is not valid"},
		{"empty profile", func(_ *Handler, in *Input) { in.Profile = "" }, "profile name is not valid"},
		{"no identity", func(h *Handler, _ *Input) {
			a := providertest.NewFakeActivator()
			a.IdentityError = errors.New("no cache")
			h.Deps.Providers = source{a}
		}, "cannot read the bot identity"},
		{"nested other profile", func(_ *Handler, in *Input) {
			in.Parent = append(in.Parent, "AGENT_ID_CONTEXT=1", "AGENT_ID_ACTIVE_PROFILE=other", "AGENT_ID_GIT_CONFIG_BASE=0", "AGENT_ID_GIT_CONFIG_END=0")
		}, `"other"`},
	}
	for _, tt := range tests {
		for _, shell := range []string{"bash", "zsh"} {
			t.Run(tt.name+"/"+shell, func(t *testing.T) {
				file := envFile(t, "export KEEP=1\n", 0o600)
				h, in := newHandler(t), input(file)
				tt.mutate(h, &in)
				err := h.Handle(context.Background(), in)
				if code(err) != ExitRefused || !strings.Contains(err.Error(), tt.msg) {
					t.Fatalf("err=%v", err)
				}
				got := read(t, file)
				if strings.Count(got, claude.RefusalMark) != 1 || strings.Contains(got, claude.BlockStart) || !strings.HasPrefix(got, "export KEEP=1\n") {
					t.Fatalf("file:\n%s", got)
				}
				if tt.name == "hostile profile" && strings.Contains(got, "a';x") {
					t.Fatal("hostile name in the refusal line")
				}
				out, c := runCommand(t, shell, file, "echo ran; touch pwned")
				if c != 125 || strings.Contains(out, "ran") || !strings.Contains(out, "the command was not run") {
					t.Fatalf("command: %d %q", c, out)
				}
				if _, err := os.Stat("pwned"); err == nil {
					t.Fatal("injected effect")
				}
			})
		}
	}
}

func TestOtherProfileBlockIsRefusedNamingBoth(t *testing.T) {
	testenv.Isolate(t)
	file := envFile(t, "", 0o600)
	h := newHandler(t)
	first := input(file)
	first.Profile = "reviewer"
	if err := h.Handle(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	err := h.Handle(context.Background(), input(file))
	if code(err) != ExitRefused || !strings.Contains(err.Error(), "reviewer") || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("err=%v", err)
	}
	got := read(t, file)
	if strings.Count(got, claude.BlockStart) != 1 {
		t.Fatalf("blocks:\n%s", got)
	}
	out, c := runCommand(t, "bash", file, "echo ran")
	if c != 125 || strings.Contains(out, "ran") {
		t.Fatalf("%d %q", c, out)
	}
}

func TestNoSafeFileGivesExit4AndWritesNothing(t *testing.T) {
	testenv.Isolate(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		file func(t *testing.T) string
		msg  string
	}{
		{"unset", func(*testing.T) string { return "" }, "must run as a Claude Code SessionStart"},
		{"relative", func(*testing.T) string { return "env.sh" }, "not an absolute path"},
		{"symlink", func(*testing.T) string { return link }, "symbolic link"},
		{"directory", func(*testing.T) string { return dir }, "CLAUDE_ENV_FILE"},
		{"world writable", func(t *testing.T) string { return envFile(t, "", 0o602) }, "writable by others"},
		{"group writable shared group", func(t *testing.T) string { return envFile(t, "", 0o660) }, "group-writable"},
		{"unparseable existing", func(t *testing.T) string { return envFile(t, "echo 'unterminated\n", 0o600) }, "does not parse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := tt.file(t)
			var before string
			if file != "" && file != dir && filepath.IsAbs(file) {
				b, _ := os.ReadFile(file)
				before = string(b)
			}
			h := newHandler(t)
			h.PrivateGroup = func(uint32) bool { return false }
			err := h.Handle(context.Background(), input(file))
			if code(err) != ExitNoFile || !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("err=%v", err)
			}
			if before != "" && read(t, file) != before {
				t.Fatal("the file was changed")
			}
		})
	}
	if read(t, target) != "x\n" {
		t.Fatal("the symlink target was written")
	}
}

func TestGroupWritableByPrivateGroupIsAccepted(t *testing.T) {
	testenv.Isolate(t)
	file := envFile(t, "", 0o660)
	h := newHandler(t)
	h.PrivateGroup = func(uint32) bool { return true }
	if err := h.Handle(context.Background(), input(file)); err != nil {
		t.Fatal(err)
	}
}

func TestMissingFileIsCreatedPrivate(t *testing.T) {
	testenv.Isolate(t)
	file := filepath.Join(t.TempDir(), "new.sh")
	if err := newHandler(t).Handle(context.Background(), input(file)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("%v %v", info, err)
	}
}

func TestBashOnlySyntaxIsAcceptedWithBashShell(t *testing.T) {
	testenv.Isolate(t)
	file := envFile(t, "arr=(a b)\n", 0o600)
	if err := newHandler(t).Handle(context.Background(), input(file)); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentRunsWriteOneBlock(t *testing.T) {
	testenv.Isolate(t)
	file := envFile(t, "", 0o600)
	var wg sync.WaitGroup
	h := newHandler(t)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.Handle(context.Background(), input(file)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := read(t, file); strings.Count(got, claude.BlockStart) != 1 {
		t.Fatalf("file:\n%s", got)
	}
}

func TestShortWriteRollsBackAndWritesRefusal(t *testing.T) {
	testenv.Isolate(t)
	file := envFile(t, "export KEEP=1\n", 0o600)
	h := newHandler(t)
	calls := 0
	h.Write = func(f *os.File, b []byte) (int, error) {
		calls++
		if calls == 1 {
			return f.Write(b[:len(b)/2])
		}
		return f.Write(b)
	}
	err := h.Handle(context.Background(), input(file))
	if code(err) != ExitRefused {
		t.Fatalf("err=%v", err)
	}
	got := read(t, file)
	if !strings.HasPrefix(got, "export KEEP=1\n") || strings.Contains(got, claude.BlockStart) || strings.Count(got, claude.RefusalMark) != 1 {
		t.Fatalf("file:\n%s", got)
	}
	if out, c := runCommand(t, "bash", file, "echo ran"); c != 125 || strings.Contains(out, "ran") {
		t.Fatalf("%d %q", c, out)
	}
}

func TestTruncatedBlockAloneLetsACommandRun(t *testing.T) {
	testenv.Isolate(t)
	file := envFile(t, "", 0o600)
	if err := newHandler(t).Handle(context.Background(), input(file)); err != nil {
		t.Fatal(err)
	}
	full := read(t, file)
	cut := full[:len(full)/2]
	trunc := envFile(t, cut, 0o600)
	out, c := runCommand(t, "bash", trunc, "echo ran")
	if strings.Contains(out, "ran") && !strings.Contains(out, "mech=") && c == 0 {
		t.Log("a truncated block lets the command run: this is why a failed write is rolled back")
		return
	}
	t.Fatalf("expected the command to run: %d %q", c, out)
}

func TestWriteFailureAfterRollbackFailureIsExit4(t *testing.T) {
	testenv.Isolate(t)
	file := envFile(t, "", 0o600)
	h := newHandler(t)
	h.Write = func(*os.File, []byte) (int, error) { return 0, errors.New("disk full") }
	err := h.Handle(context.Background(), input(file))
	if code(err) != ExitNoFile {
		t.Fatalf("err=%v", err)
	}
	if read(t, file) != "" {
		t.Fatal("the file was changed")
	}
}
