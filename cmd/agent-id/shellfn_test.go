package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fakeRC = `gh() { echo "user gh function"; }
git() { echo "user git function"; }
alias gh='echo user-gh-alias'
alias git='echo user-git-alias'
MARKER=${MARKER:+$MARKER,}$1
`

func writeRC(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func needShell(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s is not installed", name)
	}
}

func TestExecNeutralizesZshFunctionsAndAliases(t *testing.T) {
	needShell(t, "zsh")
	for _, tt := range []struct {
		name  string
		flags []string
		rc    string
	}{
		{"non-interactive (zshenv)", []string{"-c"}, ".zshenv"},
		{"login (zprofile)", []string{"-l", "-c"}, ".zprofile"},
		{"interactive (zshrc)", []string{"-i", "-c"}, ".zshrc"},
		{"login zlogin", []string{"-l", "-c"}, ".zlogin"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newExecFixture(t)
			writeRC(t, f.env.Home, map[string]string{tt.rc: fakeRC})
			script := `type gh; type git`
			args := append([]string{"exec", "--profile", "claude", "--", "zsh"}, tt.flags...)
			inside := f.run(append(args, script)...)
			if inside.code != 0 {
				t.Fatalf("exit %d: %s", inside.code, inside.stderr)
			}
			launcher := filepath.Join(f.env.CacheHome, "agent-id", "github", "neutral-bin")
			if want := "gh is " + launcher + "/gh"; !strings.Contains(inside.stdout, want) {
				t.Errorf("inside exec %q, want it to contain %q", inside.stdout, want)
			}
			if strings.Contains(inside.stdout, "function") || strings.Contains(inside.stdout, "alias") {
				t.Errorf("inside exec a function or alias survived: %q", inside.stdout)
			}
			outside := exec.Command("zsh", append(tt.flags, script)...)
			outside.Env = os.Environ()
			out, _ := outside.CombinedOutput()
			if !strings.Contains(string(out), "gh is a shell function") && !strings.Contains(string(out), "gh is an alias") {
				t.Errorf("outside exec the user's definition is gone: %q", out)
			}
		})
	}
}

func TestExecKeepsTheUsersZshStartup(t *testing.T) {
	needShell(t, "zsh")
	f := newExecFixture(t)
	custom := filepath.Join(f.env.Home, "zdot")
	writeRC(t, f.env.Home, map[string]string{".zshenv": "export ZDOTDIR=" + custom + "\n"})
	writeRC(t, custom, map[string]string{".zshenv": "ENVMARK=1\n", ".zshrc": fakeRC + "RCMARK=1\nHISTFILE=\"${ZDOTDIR:-$HOME}/hist\"\n"})
	r := f.run("exec", "--profile", "claude", "--", "zsh", "-i", "-c", `echo "$ENVMARK$RCMARK"; type gh; echo "$HISTFILE"`)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if !strings.HasPrefix(r.stdout, "1\n") {
		t.Errorf("the user's .zshrc at the ZDOTDIR that .zshenv chose did not run exactly once: %q", r.stdout)
	}
	if strings.Contains(r.stdout, "function") || strings.Contains(r.stdout, "alias") {
		t.Errorf("a function or alias survived under the user's ZDOTDIR: %q", r.stdout)
	}
	if !strings.Contains(r.stdout, "neutral-bin/gh") {
		t.Errorf("gh does not resolve to the launcher: %q", r.stdout)
	}
	if !strings.Contains(r.stdout, filepath.Join(custom, "hist")) {
		t.Errorf("HISTFILE moved out of the user's ZDOTDIR: %q", r.stdout)
	}
}

func TestExecNeutralizesBashFunctionsThroughBashEnv(t *testing.T) {
	needShell(t, "bash")
	f := newExecFixture(t)
	userEnv := filepath.Join(f.env.Home, "user-bash-env")
	writeRC(t, f.env.Home, map[string]string{"user-bash-env": fakeRC + "USERENV=ran\n"})
	f.extra = append(f.extra, "BASH_ENV="+userEnv)
	r := f.run("exec", "--profile", "claude", "--", "bash", "-c", `type gh; type git; echo "user=$USERENV"`)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	launcher := filepath.Join(f.env.CacheHome, "agent-id", "github", "neutral-bin")
	if !strings.Contains(r.stdout, "gh is "+launcher+"/gh") || strings.Contains(r.stdout, "function") {
		t.Errorf("inside exec %q, want gh at the launcher and no function", r.stdout)
	}
	if !strings.Contains(r.stdout, "git is ") {
		t.Errorf("git is not a plain program inside exec: %q", r.stdout)
	}
	if !strings.Contains(r.stdout, "user=ran") {
		t.Errorf("the user's BASH_ENV did not run: %q", r.stdout)
	}
	outside := exec.Command("bash", "-c", `type gh`)
	outside.Env = append(os.Environ(), "BASH_ENV="+userEnv)
	out, _ := outside.CombinedOutput()
	if !strings.Contains(string(out), "function") {
		t.Errorf("outside exec the user's function is gone: %q", out)
	}
}

func TestExecShellHookSurvivesNesting(t *testing.T) {
	needShell(t, "zsh")
	f := newExecFixture(t)
	writeRC(t, f.env.Home, map[string]string{".zshenv": fakeRC})
	r := f.run("exec", "--profile", "claude", "--", f.binary, "exec", "--profile", "claude", "--", "zsh", "-c", `type gh`)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if strings.Contains(r.stdout, "function") || !strings.Contains(r.stdout, "neutral-bin/gh") {
		t.Errorf("nested exec: %q", r.stdout)
	}
}

// TestExecNeutralizesTheClaudeCodeOrder models how Claude Code runs a Bash
// tool command (#135): a snapshot of the developer's functions, taken once
// from the real start files, is sourced first. Then CLAUDE_ENV_FILE is
// sourced, then the command runs. ZDOTDIR and BASH_ENV act too early for this
// order, so the CLAUDE_ENV_FILE script must remove the functions.
func TestExecNeutralizesTheClaudeCodeOrder(t *testing.T) {
	for _, shell := range []string{"zsh", "bash"} {
		t.Run(shell, func(t *testing.T) {
			needShell(t, shell)
			f := newExecFixture(t)
			snapshot := filepath.Join(f.env.Home, "snapshot.sh")
			userFile := filepath.Join(f.env.Home, "user-claude-env.sh")
			writeRC(t, f.env.Home, map[string]string{"snapshot.sh": fakeRC, "user-claude-env.sh": "USERFILE=ran\n"})
			f.extra = append(f.extra, "CLAUDE_ENV_FILE="+userFile)
			script := `source ` + snapshot + `; source "$CLAUDE_ENV_FILE"; type gh; type git; echo "user=$USERFILE"`
			inside := f.run("exec", "--profile", "claude", "--", shell, "-c", script)
			if inside.code != 0 {
				t.Fatalf("exit %d: %s", inside.code, inside.stderr)
			}
			launcher := filepath.Join(f.env.CacheHome, "agent-id", "github", "neutral-bin")
			if !strings.Contains(inside.stdout, "gh is "+launcher+"/gh") || strings.Contains(inside.stdout, "function") {
				t.Errorf("inside exec %q, want gh at the launcher and no function", inside.stdout)
			}
			if !strings.Contains(inside.stdout, "user=ran") {
				t.Errorf("the developer's CLAUDE_ENV_FILE did not run: %q", inside.stdout)
			}
			outside := exec.Command(shell, "-c", script)
			outside.Env = append(os.Environ(), "CLAUDE_ENV_FILE="+userFile)
			out, _ := outside.CombinedOutput()
			if !strings.Contains(string(out), "function") && !strings.Contains(string(out), "alias") {
				t.Errorf("outside exec the developer's function is gone: %q", out)
			}
		})
	}
}

func TestExecClaudeEnvFileWithoutAnInheritedOne(t *testing.T) {
	needShell(t, "bash")
	f := newExecFixture(t)
	r := f.run("exec", "--profile", "claude", "--", "bash", "-c", `. "$CLAUDE_ENV_FILE" && echo ok`)
	if r.code != 0 || !strings.Contains(r.stdout, "ok") {
		t.Errorf("exit %d stdout %q stderr %q", r.code, r.stdout, r.stderr)
	}
	files, err := os.ReadDir(filepath.Join(f.env.CacheHome, "agent-id", "github", "shell-hook"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range files {
		info, _ := e.Info()
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is open to group or others: %v", e.Name(), info.Mode())
		}
	}
	dir, err := os.Stat(filepath.Join(f.env.CacheHome, "agent-id", "github", "shell-hook"))
	if err != nil || dir.Mode().Perm() != 0o700 {
		t.Errorf("shell-hook directory: %v %v", dir, err)
	}
}

func TestDoctorWarnsAboutAShellFunctionInAStartFile(t *testing.T) {
	f := newExecFixture(t)
	writeRC(t, f.env.Home, map[string]string{".zshrc": "gh() { command gh \"$@\"; }\nalias git=hub\n"})
	r := f.run("doctor", "--profile", "claude")
	if !strings.Contains(r.stdout, "a shell function named gh is defined in a start file") || !strings.Contains(r.stdout, "a shell alias named git is defined in a start file") {
		t.Errorf("doctor output lacks the warnings:\n%s", r.stdout)
	}
	if strings.Contains(r.stdout, f.env.Home) {
		t.Errorf("doctor printed the home directory in full:\n%s", r.stdout)
	}
	inside := f.run("whoami")
	if strings.Contains(inside.stdout, "Warning") {
		t.Errorf("whoami outside an agent context printed a warning: %q", inside.stdout)
	}
	agent := f.run("exec", "--profile", "claude", "--", f.binary, "whoami")
	if !strings.Contains(agent.stdout, "Warning:") || !strings.Contains(agent.stdout, "function named gh") {
		t.Errorf("whoami inside exec lacks the warning: %q (%s)", agent.stdout, agent.stderr)
	}
}
