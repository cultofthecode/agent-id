package activation

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func renderAct(parent []string, dirs ...string) (string, Activation, error) {
	plan := Plan{
		Profile: "claude", Mechanism: MechanismEnv, Binary: "/opt/agent-id", Drop: []string{"GH_TOKEN", "GITHUB_TOKEN"},
		Set: map[string]string{"GIT_TERMINAL_PROMPT": "0"}, PathPrepend: dirs,
		GitConfig: []GitConfigEntry{{Key: "credential.helper", Value: ""}}, TokenPrefixes: []string{"ghs_"},
		Unshadow: []string{"git", "gh"},
	}
	env, err := Build(parent, plan)
	if err != nil {
		return "", Activation{}, err
	}
	a := Activation{Env: env, Drop: plan.Drop, Unshadow: plan.Unshadow, PathPrepend: dirs, TokenPrefixes: plan.TokenPrefixes}
	out, err := Render(parent, a)
	return out, a, err
}

func TestRenderOrderAndContent(t *testing.T) {
	testenv.Isolate(t)
	out, _, err := renderAct([]string{"PATH=/bin", "GH_TOKEN=x", "KEEP=1"}, "/shim")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# agent-id env 1 profile=claude\nunset GH_TOKEN GITHUB_TOKEN &&\n") {
		t.Fatalf("head: %q", out)
	}
	order := []string{"unset GH_TOKEN", "unalias git gh", "export AGENT_ID_ACTIVE_PROFILE='claude'", "export GIT_CONFIG_COUNT='1'", "__agent_id_dir='/shim'", "unset __agent_id_path"}
	last := -1
	for _, s := range order {
		i := strings.Index(out, s)
		if i <= last {
			t.Fatalf("%q out of order in\n%s", s, out)
		}
		last = i
	}
	if strings.Contains(out, "KEEP") {
		t.Fatal("unchanged variable printed")
	}
}

func TestRenderNoPathGroupWithoutPrepend(t *testing.T) {
	testenv.Isolate(t)
	out, _, err := renderAct([]string{"PATH=/bin"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "__agent_id") {
		t.Fatalf("path group present:\n%s", out)
	}
}

func TestRenderUnrenderable(t *testing.T) {
	testenv.Isolate(t)
	base := func() Activation {
		return Activation{Env: []string{"AGENT_ID_ACTIVE_PROFILE=claude", "A=1"}, Drop: []string{"GH_TOKEN"}, Unshadow: []string{"gh"}, TokenPrefixes: []string{"ghs_"}}
	}
	tests := map[string]func(*Activation){
		"bad var name":       func(a *Activation) { a.Env = append(a.Env, "A-B=1") },
		"bad drop name":      func(a *Activation) { a.Drop = []string{"X;rm"} },
		"bad unshadow":       func(a *Activation) { a.Unshadow = []string{"gh; x"} },
		"token value":        func(a *Activation) { a.Env = append(a.Env, "B=ghs_secret123") },
		"newline value":      func(a *Activation) { a.Env = append(a.Env, "B=a\nb") },
		"bad profile":        func(a *Activation) { a.Env[0] = "AGENT_ID_ACTIVE_PROFILE=a';x" },
		"relative path dir":  func(a *Activation) { a.PathPrepend = []string{"rel"} },
		"colon in path dir":  func(a *Activation) { a.PathPrepend = []string{"/a:b"} },
		"secret in path dir": func(a *Activation) { a.PathPrepend = []string{"/ghs_x"} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			a := base()
			mutate(&a)
			out, err := Render(nil, a)
			if !errors.Is(err, ErrUnrenderable) || out != "" {
				t.Fatalf("err=%v out=%q", err, out)
			}
			if strings.Contains(err.Error(), "ghs_secret123") {
				t.Fatal("value in error")
			}
		})
	}
}

func TestRenderQuotesHostileValues(t *testing.T) {
	testenv.Isolate(t)
	hostile := []string{"it's", "a\\b", "$(touch pwned)", "`id`", "!x", "%s", "é✓", "'; rm -rf x; '", "a b\tc"}
	for _, v := range hostile {
		got := shellQuote(v)
		if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") || strings.Contains(strings.ReplaceAll(got, `'\''`, ""), "'") && strings.Count(strings.ReplaceAll(got, `'\''`, ""), "'") != 2 {
			t.Fatalf("%q -> %s", v, got)
		}
	}
}

func shell(t *testing.T, sh string, env []string, script string) (string, error) {
	t.Helper()
	path, err := exec.LookPath(sh)
	if err != nil {
		t.Skipf("%s not installed", sh)
	}
	args := []string{"-c", script}
	if sh == "zsh" {
		args = []string{"-f", "-c", script}
	}
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestRenderEvalYieldsChildEnvironment(t *testing.T) {
	testenv.Isolate(t)
	hostileDir := "/tmp/x $(touch pwned)'\"; *y"
	parent := []string{"PATH=/usr/bin:/bin:" + hostileDir, "GH_TOKEN=personal", "HOME=" + t.TempDir(), "KEEP=1", "STRAY=1"}
	out, a, err := renderAct(parent, "/x *y/sh'im")
	if err != nil {
		t.Fatal(err)
	}
	for _, sh := range []string{"bash", "zsh"} {
		t.Run(sh, func(t *testing.T) {
			dump := "env | LC_ALL=C sort"
			got, err := shell(t, sh, parent, out+"\n"+dump)
			if err != nil {
				t.Fatalf("%v\n%s", err, got)
			}
			want := map[string]string{}
			for _, kv := range a.Env {
				n, v, _ := strings.Cut(kv, "=")
				want[n] = v
			}
			have := map[string]string{}
			for _, line := range strings.Split(got, "\n") {
				if n, v, ok := strings.Cut(line, "="); ok {
					have[n] = v
				}
			}
			for n, v := range want {
				if have[n] != v {
					t.Errorf("%s = %q, want %q", n, have[n], v)
				}
			}
			for _, n := range []string{"GH_TOKEN", "STRAY"} {
				if _, ok := have[n]; ok && want[n] == "" {
					t.Errorf("%s survived", n)
				}
			}
			if _, err := os.Stat(filepath.Join(t.TempDir(), "pwned")); err == nil {
				t.Error("injected file")
			}
		})
	}
}

func TestRenderTwiceKeepsPathAndNoInjection(t *testing.T) {
	testenv.Isolate(t)
	dir := "/x *y/sh'im"
	parent := []string{"PATH=" + dir + ":/usr/bin:" + dir}
	out, _, err := renderAct(parent, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, sh := range []string{"bash", "zsh"} {
		got, err := shell(t, sh, parent, out+"\n"+"printf '%s' \"$PATH\"; echo; "+out+"\nprintf '%s' \"$PATH\"")
		if err != nil {
			t.Fatalf("%s: %v\n%s", sh, err, got)
		}
		lines := strings.Split(strings.TrimSpace(got), "\n")
		if len(lines) != 2 || lines[0] != lines[1] || lines[0] != dir+":/usr/bin" {
			t.Fatalf("%s: %q", sh, lines)
		}
	}
}

func TestRenderReadonlyFailsAsAWhole(t *testing.T) {
	testenv.Isolate(t)
	parent := []string{"PATH=/usr/bin:/bin", "GH_TOKEN=x"}
	out, _, err := renderAct(parent, "/shim")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := shell(t, "bash", parent, "readonly GH_TOKEN; { "+out+"\n} || { echo failed; exit 125; }; echo ran")
	if !strings.Contains(got, "failed") || strings.Contains(got, "ran") {
		t.Fatalf("bash: %q", got)
	}
	got, err = shell(t, "zsh", parent, "readonly GH_TOKEN; { "+out+"\n} || { echo failed; exit 125; }; echo ran")
	if err == nil || strings.Contains(got, "ran") || !strings.Contains(got, "GH_TOKEN") {
		t.Fatalf("zsh: err=%v %q", err, got)
	}
}

func TestRenderRemovesFunctionAndAlias(t *testing.T) {
	testenv.Isolate(t)
	parent := []string{"PATH=/usr/bin:/bin"}
	out, _, err := renderAct(parent, "/shim")
	if err != nil {
		t.Fatal(err)
	}
	fn := "gh() { echo decoy; }; git() { echo decoy; }\n"
	for _, sh := range []string{"bash", "zsh"} {
		got, _ := shell(t, sh, parent, fn+out+"\ngh x 2>&1; git --version 2>&1; true")
		if strings.Contains(got, "decoy") {
			t.Fatalf("%s: gh survived: %q", sh, got)
		}
	}
	got, _ := shell(t, "zsh", parent, "alias gh='echo decoy'\n"+out+"\nalias gh || echo gone")
	if !strings.Contains(got, "gone") {
		t.Fatalf("zsh alias: %q", got)
	}
}

func TestRenderIsSortedAndStable(t *testing.T) {
	testenv.Isolate(t)
	a, _, _ := renderAct([]string{"PATH=/bin", "Z=1", "A=1"}, "/s")
	b, _, _ := renderAct([]string{"A=1", "PATH=/bin", "Z=1"}, "/s")
	if a != b || !slices.IsSorted(strings.Fields(strings.SplitN(strings.Split(a, "\n")[1], " &&", 2)[0])[1:]) {
		t.Fatalf("unstable or unsorted:\n%s\n%s", a, b)
	}
}
