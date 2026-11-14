package activation

import (
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestShellHookSet(t *testing.T) {
	testenv.Isolate(t)
	const dir = "/cache/shell-hook"
	tests := []struct {
		name   string
		parent []string
		want   map[string]string
	}{
		{"fresh start keeps the developer's values", []string{"ZDOTDIR=/u/z", "BASH_ENV=/u/b", "CLAUDE_ENV_FILE=/u/c"},
			map[string]string{VarUserZDotDir: "/u/z", VarUserBashEnv: "/u/b", VarUserClaudeEnvFile: "/u/c"}},
		{"fresh start without values", []string{"PATH=/bin"},
			map[string]string{VarUserZDotDir: "", VarUserBashEnv: "", VarUserClaudeEnvFile: ""}},
		{"re-entry keeps the recorded ZDOTDIR and BASH_ENV, and sources the outer session file",
			[]string{VarContext + "=1", "ZDOTDIR=" + dir, "BASH_ENV=" + dir + "/bash-env.sh", "CLAUDE_ENV_FILE=" + dir + "/sessions/9.b/claude-env.sh",
				VarUserZDotDir + "=/u/z", VarUserBashEnv + "=/u/b", VarUserClaudeEnvFile + "=/u/c"},
			map[string]string{VarUserZDotDir: "/u/z", VarUserBashEnv: "/u/b", VarUserClaudeEnvFile: dir + "/sessions/9.b/claude-env.sh"}},
		{"re-entry does not trust a CLAUDE_ENV_FILE outside the sessions directory",
			[]string{VarContext + "=1", "CLAUDE_ENV_FILE=" + dir + "/sessions/../../evil.sh",
				VarUserZDotDir + "=/u/z", VarUserBashEnv + "=/u/b", VarUserClaudeEnvFile + "=/u/c"},
			map[string]string{VarUserZDotDir: "/u/z", VarUserBashEnv: "/u/b", VarUserClaudeEnvFile: "/u/c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellHookSet(tt.parent, dir, dir+"/sessions/1.a/claude-env.sh")
			want := map[string]string{"ZDOTDIR": dir, "BASH_ENV": dir + "/bash-env.sh", "CLAUDE_ENV_FILE": dir + "/sessions/1.a/claude-env.sh"}
			for k, v := range tt.want {
				want[k] = v
			}
			if len(got) != len(want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestShellHookFilesHoldNoSecretAndCleanEveryName(t *testing.T) {
	files := ShellHookFiles([]string{"gh", "git"})
	for _, name := range []string{".zshenv", ".zprofile", ".zshrc", ".zlogin", "bash-env.sh"} {
		content, ok := files[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		for _, want := range []string{"unset -f gh git 2>/dev/null", "unalias gh git 2>/dev/null"} {
			if !strings.Contains(content, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
		if strings.Contains(content, "GH_TOKEN") || strings.Contains(content, "ghs_") {
			t.Errorf("%s names a credential", name)
		}
	}
}

func TestShellHookFilesHaveNoSharedClaudeEnvFile(t *testing.T) {
	if _, ok := ShellHookFiles([]string{"gh", "git"})[claudeEnvFile]; ok {
		t.Error("the CLAUDE_ENV_FILE script is shared again")
	}
}

func TestClaudeEnvScript(t *testing.T) {
	got := ClaudeEnvScript("/u/it's here.sh", []string{"gh", "git"})
	for _, want := range []string{`. '/u/it'\''s here.sh'`, "unset -f gh git 2>/dev/null", "unalias gh git 2>/dev/null"} {
		if !strings.Contains(got, want) {
			t.Errorf("script lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(ClaudeEnvScript("", []string{"gh"}), ". ") {
		t.Error("an empty user file is sourced")
	}
}

func TestEnvMechanismSetsNoShellHook(t *testing.T) {
	testenv.Isolate(t)
	a := providertest.NewFakeActivator()
	paths := NeutralPaths{ConfigDir: "/c", BinDir: "/b", ShellDir: "/s", ClaudeEnvFile: "/s/sessions/1.a/claude-env.sh"}
	plan := PlanFor(Request{Profile: "claude", Mechanism: MechanismEnv, Binary: "/bin/agent-id"}, a, paths)
	for _, n := range []string{"ZDOTDIR", "BASH_ENV", "CLAUDE_ENV_FILE", VarUserZDotDir} {
		if _, ok := plan.Set[n]; ok {
			t.Errorf("the env mechanism sets %s", n)
		}
	}
	plan = PlanFor(Request{Profile: "claude", Mechanism: MechanismExec, Binary: "/bin/agent-id"}, a, paths)
	if plan.Set["CLAUDE_ENV_FILE"] != "/s/sessions/1.a/claude-env.sh" {
		t.Errorf("the exec mechanism does not set CLAUDE_ENV_FILE: %v", plan.Set)
	}
}
