package claude

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

func sh(t *testing.T, shell, script string) (string, int) {
	t.Helper()
	path, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s not installed", shell)
	}
	args := []string{"-c", script}
	if shell == "zsh" {
		args = []string{"-f", "-c", script}
	}
	cmd := exec.Command(path, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return string(out), code
}

func TestRefusalLineIsPlainAndStops(t *testing.T) {
	testenv.Isolate(t)
	long := strings.Repeat("x", 1024)
	reasons := []string{"plain", "it's \"quoted\"", "$(touch pwned) `id` \\ !", "esc \x1b[31m red \x00 \r\n", long, "bad \xff utf8", "é✓"}
	profiles := []string{"claude", "a';x", "", "$(id)", "A"}
	for _, shell := range []string{"bash", "zsh", "sh"} {
		for _, r := range reasons {
			for _, p := range profiles {
				line := RefusalLine(r, p)
				if strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, RefusalMark+"\n") {
					t.Fatalf("shape: %q", line)
				}
				out, code := sh(t, shell, line+"echo ran; touch pwned")
				if code != 125 || strings.Contains(out, "ran") {
					t.Fatalf("%s: code=%d out=%q", shell, code, out)
				}
				if !strings.HasPrefix(out, "agent-id: ") || !strings.Contains(out, "the command was not run") || strings.Count(out, "\n") != 1 {
					t.Fatalf("%s: message %q", shell, out)
				}
				if ValidProfile(p) != strings.Contains(out, "--profile "+p) {
					t.Fatalf("profile %q naming wrong: %q", p, out)
				}
			}
		}
	}
	if strings.Contains(RefusalLine(long, "claude"), strings.Repeat("x", MaxReason+1)) {
		t.Error("reason not cut")
	}
	if strings.Contains(RefusalLine("a\x1b[0mb", "claude"), "\x1b") {
		t.Error("control character kept")
	}
}

func TestGuardedBlockRunsOrStops(t *testing.T) {
	testenv.Isolate(t)
	ok := GuardedBlock("# agent-id env 1 profile=claude\nexport A='1' &&\nexport B='2'\n", GuardRefusal("claude"))
	bad := GuardedBlock("# agent-id env 1 profile=claude\nfalse\n", GuardRefusal("claude"))
	if !strings.HasPrefix(ok, BlockStart+"claude\neval '") || !strings.HasSuffix(ok, BlockEnd+"\n") {
		t.Fatalf("shape: %q", ok)
	}
	for _, shell := range []string{"bash", "zsh", "sh"} {
		out, code := sh(t, shell, ". /dev/stdin <<'EOF'\n"+ok+"EOF\necho \"ran $A$B\"")
		if code != 0 || strings.TrimSpace(out) != "ran 12" {
			t.Fatalf("%s ok: code=%d %q", shell, code, out)
		}
		out, code = sh(t, shell, "eval '"+strings.ReplaceAll(bad, "'", `'\''`)+"'; echo ran")
		if code != 125 || strings.Contains(out, "ran") || !strings.Contains(out, "activation failed") {
			t.Fatalf("%s bad: code=%d %q", shell, code, out)
		}
	}
}

func TestProfilesInAndHasBlock(t *testing.T) {
	b := GuardedBlock("# agent-id env 1 profile=claude\ntrue\n", GuardRefusal("claude"))
	content := "export X=1\n" + b + "echo after\n"
	if got := ProfilesIn(content); len(got) != 1 || got[0] != "claude" {
		t.Fatalf("%v", got)
	}
	if got := ProfilesIn(BlockStart + "a';x\n"); got[0] != "?" {
		t.Fatalf("%v", got)
	}
	if !HasBlock(content, b) || !HasBlock(b, b) || HasBlock("export X=1\n", b) || HasBlock("x"+b, b) {
		t.Fatal("HasBlock")
	}
}
