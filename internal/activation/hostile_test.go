package activation

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestBuildGitConfigCountIsCanonicalOnEveryPath(t *testing.T) {
	testenv.Isolate(t)
	empty := testPlan()
	empty.GitConfig = nil
	for _, count := range [][]string{nil, {"GIT_CONFIG_COUNT=0"}, {"GIT_CONFIG_COUNT="}, {"GIT_CONFIG_COUNT=00"}} {
		for name, plan := range map[string]Plan{"empty plan": empty, "plan with entries": testPlan()} {
			once, err := Build(count, plan)
			if err != nil {
				t.Fatalf("%v %s: %v", count, name, err)
			}
			twice, err := Build(once, plan)
			if err != nil {
				t.Fatalf("%v %s re-entry: %v", count, name, err)
			}
			if !slices.Equal(once, twice) {
				t.Errorf("%v %s: Build(Build(E)) differs from Build(E):\n%v\n%v", count, name, once, twice)
			}
			want := "GIT_CONFIG_COUNT=" + string(rune('0'+len(plan.GitConfig)))
			if !slices.Contains(once, want) {
				t.Errorf("%v %s: want %s in %v", count, name, want, once)
			}
		}
	}
}

func TestBuildReentryRemovesThePreviousContribution(t *testing.T) {
	testenv.Isolate(t)
	first := testPlan()
	first.PathPrepend = []string{"/old/bin", "/old/other"}
	first.Set = map[string]string{"GH_TOKEN": "d", "ONLY_FIRST": "1", "BOTH": "a"}
	second := testPlan()
	second.Binary = "/other/agent-id"
	second.PathPrepend = []string{"/new/bin"}
	second.Set = map[string]string{"GH_TOKEN": "d", "BOTH": "b"}
	once, err := Build([]string{"PATH=/usr/bin:/old/bin:/bin:/old/bin"}, first)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Build(once, second)
	if err != nil {
		t.Fatalf("re-entry: %v", err)
	}
	env := parseEnv(out)
	if env["PATH"] != "/new/bin:/usr/bin:/bin" {
		t.Errorf("PATH = %q, want the old directories gone", env["PATH"])
	}
	if _, ok := env["ONLY_FIRST"]; ok {
		t.Error("a name that only the previous plan set survived")
	}
	if env["BOTH"] != "b" || env[VarBinary] != "/other/agent-id" || env[VarPathPrepend] != "/new/bin" || env[VarSetNames] != "BOTH,GH_TOKEN" {
		t.Errorf("unexpected context: %v", out)
	}
	fresh, err := Build([]string{"PATH=/usr/bin:/bin"}, second)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(out, fresh) {
		t.Errorf("re-entry with a changed plan differs from a fresh start:\n%v\n%v", out, fresh)
	}
}

func TestReadContextRejectsDamagedContribution(t *testing.T) {
	testenv.Isolate(t)
	base := []string{"AGENT_ID_CONTEXT=1", "AGENT_ID_ACTIVE_PROFILE=claude", "AGENT_ID_GIT_CONFIG_BASE=0", "AGENT_ID_GIT_CONFIG_END=0"}
	with := func(dirs, names string) []string {
		return append(slices.Clone(base), "AGENT_ID_PATH_PREPEND="+dirs, "AGENT_ID_SET_NAMES="+names)
	}
	tests := map[string][]string{
		"both missing":     base,
		"dirs missing":     append(slices.Clone(base), "AGENT_ID_SET_NAMES="),
		"names missing":    append(slices.Clone(base), "AGENT_ID_PATH_PREPEND="),
		"relative dir":     with("/a:rel", ""),
		"empty element":    with("/a::/b", ""),
		"duplicate dir":    with("/a:/a", ""),
		"unsorted names":   with("", "B,A"),
		"duplicate name":   with("", "A,A"),
		"bad name":         with("", "A B"),
		"owned name":       with("", "AGENT_ID_X"),
		"PATH as name":     with("", "PATH"),
		"empty name entry": with("", "A,,B"),
	}
	for name, env := range tests {
		if _, err := ReadContext(env); !errors.Is(err, ErrContextInvalid) {
			t.Errorf("%s: error %v, want ErrContextInvalid", name, err)
		}
	}
	for name, env := range map[string][]string{"empty lists": with("", ""), "lists": with("/a:/b", "A,B")} {
		if _, err := ReadContext(env); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestStrayContributionVariablesAreRemovedOnAFreshStart(t *testing.T) {
	testenv.Isolate(t)
	out, err := Build([]string{"PATH=/bin:/stray", "AGENT_ID_PATH_PREPEND=/stray", "AGENT_ID_SET_NAMES=HOME"}, testPlan())
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnv(out)
	if env[VarPathPrepend] != "/cache/neutral-bin" || env[VarSetNames] != "GH_TOKEN,GIT_TERMINAL_PROMPT" || !strings.Contains(env["PATH"], "/stray") {
		t.Errorf("stray variables were used or kept: %v", out)
	}
}

func TestPlanGuardRefusesNamesAndDirectoriesThatBreakReentry(t *testing.T) {
	testenv.Isolate(t)
	mutations := map[string]func(*Plan){
		"Set PATH":               func(p *Plan) { p.Set["PATH"] = "/x" },
		"Set GIT_CONFIG_COUNT":   func(p *Plan) { p.Set["GIT_CONFIG_COUNT"] = "9" },
		"Set GIT_CONFIG_KEY_0":   func(p *Plan) { p.Set["GIT_CONFIG_KEY_0"] = "a.b" },
		"duplicate directory":    func(p *Plan) { p.PathPrepend = []string{"/a", "/a"} },
		"empty directory":        func(p *Plan) { p.PathPrepend = []string{""} },
		"separator in directory": func(p *Plan) { p.PathPrepend = []string{"/a:/b"} },
	}
	for name, mutate := range mutations {
		p := testPlan()
		mutate(&p)
		if _, err := Build(nil, p); !errors.Is(err, ErrPlanUnsafe) {
			t.Errorf("%s: error %v, want ErrPlanUnsafe", name, err)
		}
	}
}

func TestInheritedValuesAreNeverEchoed(t *testing.T) {
	testenv.Isolate(t)
	shapes := []string{testenv.FakeToken("count"), "github_pat_" + strings.Repeat("a", 40), strings.Repeat("9", 40), "-" + strings.Repeat("1", 40)}
	for _, shape := range shapes {
		for name, fn := range map[string]func([]string) error{
			"CheckInheritedGit": CheckInheritedGit,
			"Build":             func(e []string) error { _, err := Build(e, testPlan()); return err },
		} {
			err := fn([]string{"GIT_CONFIG_COUNT=" + shape})
			if !errors.Is(err, ErrInheritedGitConfig) {
				t.Fatalf("%s: error %v, want ErrInheritedGitConfig", name, err)
			}
			requireNoSharedRun(t, err.Error(), shape)
			if !strings.Contains(err.Error(), "GIT_CONFIG_COUNT") {
				t.Errorf("%s: the message does not name the variable", name)
			}
		}
	}
	for _, version := range []string{testenv.FakeToken("version"), "2", "1 "} {
		_, err := ReadContext([]string{"AGENT_ID_CONTEXT=" + version})
		if !errors.Is(err, ErrContextInvalid) {
			t.Fatalf("version %q: %v", version, err)
		}
		requireNoSharedRun(t, err.Error(), version)
	}
	params := "GIT_CONFIG_PARAMETERS='credential.https://user:" + testenv.FakeToken("p") + "@github.com.token'='x'"
	err := CheckInheritedGit([]string{params})
	if !errors.Is(err, ErrInheritedGitConfig) || !strings.Contains(err.Error(), "credential.<…>") {
		t.Fatalf("error %v", err)
	}
	requireNoSharedRun(t, err.Error(), testenv.FakeToken("p"))
	for _, name := range []string{"credential.helper-" + testenv.FakeToken("v"), "URL.x." + testenv.FakeToken("v")} {
		err := CheckInheritedGit([]string{"GIT_CONFIG_PARAMETERS='" + name + "'='x'"})
		if err == nil {
			t.Fatalf("%s accepted", name)
		}
		requireNoSharedRun(t, err.Error(), testenv.FakeToken("v"))
	}
}

func requireNoSharedRun(t *testing.T, message, value string) {
	t.Helper()
	for i := 0; i+8 <= len(value); i++ {
		if strings.Contains(message, value[i:i+8]) {
			t.Errorf("the message %q shares %q with an inherited value", message, value[i:i+8])
			return
		}
	}
}

func TestNormalizeEnvDuplicatesAndMalformedEntries(t *testing.T) {
	testenv.Isolate(t)
	personal := testenv.FakeToken("personal")
	parent := []string{
		"GH_TOKEN=" + personal, "GH_TOKEN=other", "GH_TOKEN=" + testenv.FakeToken("last"),
		"GITHUB_TOKEN=" + personal, "GITHUB_TOKEN=" + personal, "GITHUB_TOKEN=" + personal,
		"GIT_CONFIG_COUNT=7", "GIT_CONFIG_COUNT=0",
		"GH_ENTERPRISE_TOKEN", "", "=x", "gh_token=lower",
	}
	got := NormalizeEnv(parent)
	names := map[string]int{}
	for _, kv := range got {
		name, _, _ := strings.Cut(kv, "=")
		names[name]++
	}
	for name, n := range names {
		if n != 1 || name == "" {
			t.Errorf("name %q occurs %d times", name, n)
		}
	}
	if env := parseEnv(parent); env["GH_TOKEN"] != testenv.FakeToken("last") || env["gh_token"] != "lower" || len(got) != 4 {
		t.Errorf("normalized = %v", got)
	}
	out, err := Build(parent, testPlan())
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnv(out)
	if env["GH_TOKEN"] != "agent-id-no-token" {
		t.Errorf("a dropped value survived: %v", out)
	}
	if _, ok := env["GITHUB_TOKEN"]; ok {
		t.Error("GITHUB_TOKEN survived")
	}
	for _, kv := range out {
		if strings.Contains(kv, personal) || strings.Contains(kv, "last") {
			t.Errorf("a personal value survived in %q", strings.SplitN(kv, "=", 2)[0])
		}
	}
	if env["GIT_CONFIG_COUNT"] != "2" {
		t.Errorf("count = %q, want the last occurrence (0) plus 2 entries", env["GIT_CONFIG_COUNT"])
	}
}

func TestChecksAndBuildSeeTheSameDuplicate(t *testing.T) {
	testenv.Isolate(t)
	parent := []string{"GIT_CONFIG_COUNT=banana", "GIT_CONFIG_COUNT=0"}
	if err := CheckInheritedGit(parent); err != nil {
		t.Fatalf("the check refused the last value (0): %v", err)
	}
	if _, err := Build(parent, testPlan()); err != nil {
		t.Fatalf("Build refused what the check accepted: %v", err)
	}
	bad := []string{"GIT_CONFIG_COUNT=0", "GIT_CONFIG_COUNT=banana"}
	if CheckInheritedGit(bad) == nil {
		t.Error("the check accepted a bad last value")
	}
	if _, err := Build(bad, testPlan()); !errors.Is(err, ErrInheritedGitConfig) {
		t.Errorf("Build: %v", err)
	}
	ctx := []string{"AGENT_ID_CONTEXT=2", "AGENT_ID_CONTEXT=1", "AGENT_ID_ACTIVE_PROFILE=claude", "AGENT_ID_GIT_CONFIG_BASE=0", "AGENT_ID_GIT_CONFIG_END=0", "AGENT_ID_PATH_PREPEND=", "AGENT_ID_SET_NAMES="}
	if c, err := ReadContext(ctx); err != nil || c == nil {
		t.Errorf("ReadContext of a duplicate context: %v %v", c, err)
	}
}
