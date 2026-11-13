package diagnose

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
)

func testOutput() Output { return Output{Patterns: testPatterns, Home: "/home/dev"} }

func agentReport() WhoamiReport {
	return WhoamiReport{
		Context: ContextAgent, Profile: "claude", Provider: "github", Host: "github.com", Identity: "claude-dev[bot]",
		Details:    []Detail{{"Installation", "12345678"}, {"Credentials", "GitHub App installation token"}},
		Token:      WhoamiToken{Status: TokenValid, ExpiresIn: 42*time.Minute + 59*time.Second, ExpiresAt: time.Date(2026, 11, 13, 10, 42, 0, 0, time.UTC)},
		Activation: "exec",
	}
}

func TestWhoamiText(t *testing.T) {
	tests := []struct {
		name   string
		report WhoamiReport
		want   string
		exit   int
	}{
		{"agent context", agentReport(), "Context:       agent\nProfile:       claude\nProvider:      github (github.com)\nIdentity:      claude-dev[bot]\nInstallation:  12345678\nCredentials:   GitHub App installation token\nToken status:  valid (expires in 42m)\nActivation:    exec\n", 0},
		{"personal", WhoamiReport{Context: ContextPersonal}, "Context:       personal/unmanaged\n", 0},
		{"identity not resolved", func() WhoamiReport { r := agentReport(); r.Identity = ""; return r }(),
			"Context:       agent\nProfile:       claude\nProvider:      github (github.com)\nIdentity:      not resolved (run agent-id doctor --profile claude)\nInstallation:  12345678\nCredentials:   GitHub App installation token\nToken status:  valid (expires in 42m)\nActivation:    exec\n", 0},
		{"token unavailable", func() WhoamiReport {
			r := agentReport()
			r.Token = WhoamiToken{Status: TokenUnavailable, Class: ClassNotFound, Reason: "the installation does not exist or the App lost access"}
			return r
		}(), "Context:       agent\nProfile:       claude\nProvider:      github (github.com)\nIdentity:      claude-dev[bot]\nInstallation:  12345678\nCredentials:   GitHub App installation token\nToken status:  unavailable (not found: the installation does not exist or the App lost access)\nActivation:    exec\n", 1},
		{"damaged", WhoamiReport{Context: ContextAgent, Problem: "damaged: AGENT_ID_ACTIVE_PROFILE is missing", Token: WhoamiToken{Status: TokenNotChecked, Reason: "damaged context"}},
			"Context:       agent (damaged: AGENT_ID_ACTIVE_PROFILE is missing)\nToken status:  not checked (damaged context)\n", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := testOutput().Whoami(tt.report, false)
			if err != nil || string(got) != tt.want {
				t.Errorf("Whoami text = %q, %v; want %q", got, err, tt.want)
			}
			if tt.report.ExitCode() != tt.exit {
				t.Errorf("ExitCode = %d, want %d", tt.report.ExitCode(), tt.exit)
			}
		})
	}
}

func TestWhoamiJSON(t *testing.T) {
	tests := []struct {
		name   string
		report WhoamiReport
		want   string
	}{
		{"agent", agentReport(), `{"schema":1,"context":"agent","context_problem":null,"profile":"claude","provider":"github","host":"github.com","identity":"claude-dev[bot]","details":[{"label":"Installation","value":"12345678"},{"label":"Credentials","value":"GitHub App installation token"}],"token":{"status":"valid","expires_at":"2026-11-13T10:42:00Z","class":null,"reason":null},"activation":"exec"}`},
		{"personal", WhoamiReport{Context: ContextPersonal}, `{"context":"personal/unmanaged","schema":1}`},
		{"identity unresolved", func() WhoamiReport { r := agentReport(); r.Identity = ""; r.Details = nil; return r }(), `{"schema":1,"context":"agent","context_problem":null,"profile":"claude","provider":"github","host":"github.com","identity":null,"details":[],"token":{"status":"valid","expires_at":"2026-11-13T10:42:00Z","class":null,"reason":null},"activation":"exec"}`},
		{"unavailable", func() WhoamiReport {
			r := agentReport()
			r.Token = WhoamiToken{Status: TokenUnavailable, Class: ClassRateLimited, Reason: "wait"}
			return r
		}(), `{"schema":1,"context":"agent","context_problem":null,"profile":"claude","provider":"github","host":"github.com","identity":"claude-dev[bot]","details":[{"label":"Installation","value":"12345678"},{"label":"Credentials","value":"GitHub App installation token"}],"token":{"status":"unavailable","expires_at":null,"class":"rate limited","reason":"wait"},"activation":"exec"}`},
		{"damaged", WhoamiReport{Context: ContextAgent, Problem: "damaged: x", Token: WhoamiToken{Status: TokenNotChecked, Reason: "damaged context"}}, `{"schema":1,"context":"agent","context_problem":"damaged: x","profile":null,"provider":null,"host":null,"identity":null,"details":[],"token":{"status":"not checked","expires_at":null,"class":null,"reason":"damaged context"},"activation":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := testOutput().Whoami(tt.report, true)
			if err != nil {
				t.Fatal(err)
			}
			if tt.name == "personal" {
				var a, b map[string]any
				if json.Unmarshal(got, &a) != nil || json.Unmarshal([]byte(tt.want), &b) != nil || !reflect.DeepEqual(a, b) {
					t.Errorf("personal JSON = %s, want %s", got, tt.want)
				}
				return
			}
			if strings.TrimSpace(string(got)) != tt.want {
				t.Errorf("JSON = %s, want %s", got, tt.want)
			}
		})
	}
}

func sampleDoctor() DoctorReport {
	return DoctorReport{Profile: "claude", Provider: "github", Host: "github.com", Results: []Result{
		{ID: "config.load", Status: StatusOK, Title: "configuration ~/.config/agent-id/config.json loaded"},
		{ID: "git.helper.personal", Status: StatusWarn, Title: `credential helper "osxkeychain"`, Source: "global ~/.gitconfig", Fix: "none needed in agent mode"},
		{ID: "git.remote.ssh", Status: StatusFail, Title: "remote origin resolves to SSH", Fix: "remove that rule"},
	}}
}

func TestDoctorText(t *testing.T) {
	got, err := testOutput().Doctor(sampleDoctor(), false)
	want := "agent-id doctor — profile claude (github, github.com)\n\n" +
		"ok    config.load          configuration ~/.config/agent-id/config.json loaded\n" +
		"warn  git.helper.personal  credential helper \"osxkeychain\" (global ~/.gitconfig)\n      fix: none needed in agent mode\n" +
		"FAIL  git.remote.ssh       remote origin resolves to SSH\n      fix: remove that rule\n" +
		"\n1 failure, 1 warning, 1 ok\n"
	if err != nil || string(got) != want {
		t.Errorf("Doctor text = %q, %v; want %q", got, err, want)
	}
	if sampleDoctor().ExitCode() != ExitFailure {
		t.Error("a report with a failure must exit 1")
	}
}

func TestDoctorJSON(t *testing.T) {
	got, err := testOutput().Doctor(sampleDoctor(), true)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Schema  int
		Profile string
		Results []Result
		Summary Summary
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc.Schema != 1 || doc.Profile != "claude" || len(doc.Results) != 3 || doc.Summary != (Summary{OK: 1, Warn: 1, Fail: 1}) {
		t.Errorf("doctor JSON = %+v", doc)
	}
	empty, _ := testOutput().Doctor(DoctorReport{Profile: "p"}, true)
	if !strings.Contains(string(empty), `"results":[]`) {
		t.Errorf("an empty report must render results as [], got %s", empty)
	}
}

func TestOutputAbbreviatesHomeInFreeText(t *testing.T) {
	r := sampleDoctor()
	r.Results[0].Detail = "key /home/dev/.config/k.pem: permission denied"
	got, _ := testOutput().Doctor(r, false)
	if strings.Contains(string(got), "/home/dev") || !strings.Contains(string(got), "~/.config/k.pem") {
		t.Errorf("home path not abbreviated: %s", got)
	}
}

func TestRedactIsTheFirstLayerAndGuardTheSecond(t *testing.T) {
	r := sampleDoctor()
	r.Results[0].Detail = "leaked " + "ghs_decoyvalue1"
	got, err := testOutput().Doctor(r, false)
	if err != nil || strings.Contains(string(got), "ghs_") {
		t.Fatalf("Redact must remove the token: %q, %v", got, err)
	}
	o := testOutput()
	o.redact = func(s string) string { return s }
	got, err = o.Doctor(r, false)
	if !errors.Is(err, ErrWithheld) || got != nil {
		t.Errorf("with Redact replaced by the identity, Guard must withhold: %q, %v", got, err)
	}
	for _, asJSON := range []bool{false, true} {
		if got, err := o.Whoami(WhoamiReport{Context: ContextAgent, Profile: "claude", Token: WhoamiToken{Status: TokenUnavailable, Class: ClassKey, Reason: "ghs_decoy"}}, asJSON); !errors.Is(err, ErrWithheld) || got != nil {
			t.Errorf("whoami json=%v with an unredacted token: %q, %v", asJSON, got, err)
		}
	}
}

func TestCatalogStaticStringsPassTheGuard(t *testing.T) {
	snapshots := []Snapshot{
		{Profile: "p", Provider: "github", Host: "github.com"},
		{Profile: "p", Config: ConfigState{Load: Failure{Message: "x"}, Profile: Failure{Class: "c"}, Unsupported: []string{"a"}}, Cache: CacheState{Err: Failure{Message: "x"}}, Key: KeyState{Err: Failure{Class: "key"}},
			Token: TokenState{Err: Failure{Class: ClassNotFound}}, Identity: IdentityState{Err: Failure{Class: ClassForbidden}}, Repo: RepoState{Err: Failure{Class: ClassUnavailable}},
			Git: GitState{Err: "x"}, Env: EnvState{TokenLike: []string{"X"}, GHHost: "other.test"}},
	}
	for _, s := range snapshots {
		for _, res := range Run(s) {
			for _, text := range []string{res.Title, res.Fix} {
				if err := testPatterns.Guard([]byte(text)); err != nil {
					t.Errorf("static text of %s trips the guard: %q", res.ID, text)
				}
			}
		}
	}
	for id, reason := range DeferredChecks {
		if err := testPatterns.Guard([]byte(reason)); err != nil {
			t.Errorf("deferred reason of %s trips the guard", id)
		}
	}
}

func TestNoReportFieldCanHoldASecret(t *testing.T) {
	forbidden := []reflect.Type{reflect.TypeOf(provider.Token{}), reflect.TypeOf([]byte(nil)), reflect.TypeOf(provider.GitCredential{})}
	var walk func(path string, t reflect.Type)
	walk = func(path string, typ reflect.Type) {
		for _, f := range forbidden {
			if typ == f {
				t.Errorf("%s has the type %s, which can hold a secret", path, typ)
			}
		}
		switch typ.Kind() {
		case reflect.Struct:
			for i := range typ.NumField() {
				walk(path+"."+typ.Field(i).Name, typ.Field(i).Type)
			}
		case reflect.Slice, reflect.Array, reflect.Pointer, reflect.Map:
			walk(path+"[]", typ.Elem())
		}
	}
	for _, v := range []any{Snapshot{}, DoctorReport{}, WhoamiReport{}, Result{}} {
		walk(reflect.TypeOf(v).Name(), reflect.TypeOf(v))
	}
}

func TestRedactedLabelFollowedByTextIsNotWithheld(t *testing.T) {
	r := sampleDoctor()
	r.Results[0].Detail = "header Authorization: Bearer abc.def"
	r.Results[0].Source = "git config"
	for _, asJSON := range []bool{false, true} {
		got, err := testOutput().Doctor(r, asJSON)
		if err != nil || strings.Contains(string(got), "abc.def") || !strings.Contains(string(got), "Authorization: [redacted]") {
			t.Errorf("json=%v: %q, %v; want the label kept, its value redacted, and no withholding", asJSON, got, err)
		}
	}
}

func TestDoctorTextShowsTheTitleAndTheDetail(t *testing.T) {
	r := sampleDoctor()
	r.Results[0].Detail = "the file is unreadable"
	got, err := testOutput().Doctor(r, false)
	if err != nil || !strings.Contains(string(got), "loaded: the file is unreadable") {
		t.Errorf("%q, %v; want the title followed by the detail", got, err)
	}
}
