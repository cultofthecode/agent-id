package diagnose

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanFindsDefinitionsInStartFiles(t *testing.T) {
	env := testenv.Isolate(t)
	tests := []struct {
		name string
		line string
		want []string
	}{
		{"function keyword", "function gh { command gh \"$@\"; }", []string{"gh:function"}},
		{"function keyword and parens", "function git() { :; }", []string{"git:function"}},
		{"posix function", "gh() { :; }", []string{"gh:function"}},
		{"posix function with space", "  git ()  {", []string{"git:function"}},
		{"alias", "alias gh='echo x'", []string{"gh:alias"}},
		{"alias with flag", "alias -g git=hub", []string{"git:alias"}},
		{"two on one line", "alias gh=a; alias git=b", []string{"gh:alias", "git:alias"}},
		{"after and", "true && gh() { :; }", []string{"gh:function"}},
		{"comment", "# gh() { :; }", nil},
		{"indented comment", "   # alias gh=x", nil},
		{"other name", "ghx() { :; }", nil},
		{"longer name", "github() { :; }", nil},
		{"call is not a definition", "gh pr list", nil},
		{"variable is not a definition", "GH=1; alias ghost=x", nil},
		{"alias other name", "alias ghp=x", nil},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeFiles(t, env.Home, map[string]string{".zshrc": tt.line + "\n"})
			state := ScanShellFiles([]string{"HOME=" + env.Home}, []string{"gh", "git"})()
			var got []string
			for _, f := range state.Found {
				got = append(got, f.Name+":"+f.Kind)
			}
			slices.Sort(got)
			if !state.Scanned || !slices.Equal(got, tt.want) {
				t.Errorf("found %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScanFilesAndLimits(t *testing.T) {
	env := testenv.Isolate(t)
	zdot := filepath.Join(env.Home, "zdot")
	if err := os.Mkdir(zdot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, env.Home, map[string]string{".zshrc": "gh() { :; }\n", ".bashrc": "alias git=x\n", "benv": "git() { :; }\n"})
	writeFiles(t, zdot, map[string]string{".zshrc": "alias gh=x\n"})
	names := []string{"gh", "git"}
	files := func(s ShellState) []string {
		var out []string
		for _, f := range s.Found {
			out = append(out, filepath.Base(f.File)+":"+f.Name)
		}
		slices.Sort(out)
		return out
	}
	t.Run("home files", func(t *testing.T) {
		got := files(ScanShellFiles([]string{"HOME=" + env.Home}, names)())
		if !slices.Equal(got, []string{".bashrc:git", ".zshrc:gh"}) {
			t.Errorf("got %v", got)
		}
	})
	t.Run("ZDOTDIR and BASH_ENV", func(t *testing.T) {
		got := files(ScanShellFiles([]string{"HOME=" + env.Home, "ZDOTDIR=" + zdot, "BASH_ENV=" + filepath.Join(env.Home, "benv")}, names)())
		if !slices.Equal(got, []string{".bashrc:git", ".zshrc:gh", "benv:git"}) {
			t.Errorf("got %v", got)
		}
	})
	t.Run("agent context reads the saved values", func(t *testing.T) {
		environ := []string{"HOME=" + env.Home, "ZDOTDIR=/agent-id/wrapper", "AGENT_ID_CONTEXT=1", "AGENT_ID_USER_ZDOTDIR=" + zdot, "BASH_ENV=/agent-id/wrapper/bash-env.sh"}
		got := files(ScanShellFiles(environ, names)())
		if !slices.Equal(got, []string{".bashrc:git", ".zshrc:gh"}) {
			t.Errorf("got %v", got)
		}
	})
	t.Run("relative and missing paths", func(t *testing.T) {
		state := ScanShellFiles([]string{"HOME=rel", "ZDOTDIR=rel", "BASH_ENV=rel"}, names)()
		if !state.Scanned || len(state.Found) != 0 {
			t.Errorf("state %+v", state)
		}
	})
	t.Run("a large file is read up to the limit", func(t *testing.T) {
		big := strings.Repeat("# filler\n", maxStartFile/9+10) + "gh() { :; }\n"
		writeFiles(t, env.Home, map[string]string{".zshrc": big})
		state := ScanShellFiles([]string{"HOME=" + env.Home}, names)()
		for _, f := range state.Found {
			if f.Name == "gh" {
				t.Errorf("read past the limit: %+v", f)
			}
		}
	})
}

func TestShellCheck(t *testing.T) {
	s := healthy()
	if r := resultsOf(s, "context.shell"); len(r) != 1 || r[0].Status != StatusInfo {
		t.Errorf("not scanned: %+v", r)
	}
	s.Shell = ShellState{Scanned: true}
	if r := resultsOf(s, "context.shell"); len(r) != 1 || r[0].Status != StatusOK {
		t.Errorf("clean scan: %+v", r)
	}
	s.Shell = ShellState{Scanned: true, Found: []ShellName{{Name: "gh", Kind: "function", File: "/h/.zshrc"}, {Name: "git", Kind: "alias", File: "/h/.bashrc"}}}
	r := resultsOf(s, "context.shell")
	if len(r) != 2 || r[0].Status != StatusWarn || r[1].Status != StatusWarn || r[0].Source != "/h/.zshrc" || !strings.Contains(r[1].Title, "alias named git") {
		t.Errorf("found: %+v", r)
	}
	if got := ShellWarnings(s.Shell); len(got) != 2 || !strings.Contains(got[0], "function named gh") {
		t.Errorf("warnings %v", got)
	}
}
