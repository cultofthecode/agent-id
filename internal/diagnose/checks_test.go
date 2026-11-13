package diagnose

import "testing"

// resultsOf returns the results of id in Run(s).
func resultsOf(s Snapshot, id string) []Result {
	var out []Result
	for _, r := range Run(s) {
		if r.ID == id {
			out = append(out, r)
		}
	}
	return out
}

func healthy() Snapshot {
	return Snapshot{
		Profile: "claude", Provider: "github", Host: "github.com",
		Config:   ConfigState{Path: "~/.config/agent-id/config.json"},
		Cache:    CacheState{Exists: true},
		Token:    TokenState{ExpiresInMinutes: 59},
		Identity: IdentityState{Bot: "claude-dev[bot]", Match: true},
		Repo:     RepoState{Checked: 1},
		Git:      GitState{Version: "git version 2.54.0"},
	}
}

func TestChecksTable(t *testing.T) {
	boom := Failure{Class: ClassKey, Message: "details"}
	tests := []struct {
		name   string
		mutate func(*Snapshot)
		id     string
		want   []Status
	}{
		{"healthy config", func(*Snapshot) {}, "config.load", []Status{StatusOK}},
		{"config load error", func(s *Snapshot) { s.Config.Load = boom }, "config.load", []Status{StatusFail}},
		{"profile skipped after load error", func(s *Snapshot) { s.Config.Load = boom }, "config.profile", []Status{StatusSkip}},
		{"profile not found", func(s *Snapshot) { s.Config.Profile = boom }, "config.profile", []Status{StatusFail}},
		{"unsupported listed", func(s *Snapshot) { s.Config.Unsupported = []string{"gl"} }, "config.unsupported", []Status{StatusInfo}},
		{"cache missing", func(s *Snapshot) { s.Cache.Exists = false }, "token.cache", []Status{StatusSkip}},
		{"cache insecure", func(s *Snapshot) { s.Cache.Err = boom }, "token.cache", []Status{StatusFail}},
		{"key bad", func(s *Snapshot) { s.Key.Err = boom }, "key.file", []Status{StatusFail}},
		{"key skipped", func(s *Snapshot) { s.Key.Skip = "no profile" }, "key.file", []Status{StatusSkip}},
		{"token ok", func(*Snapshot) {}, "provider.token", []Status{StatusOK}},
		{"token 404", func(s *Snapshot) { s.Token.Err = Failure{Class: ClassNotFound} }, "provider.token", []Status{StatusFail}},
		{"identity match", func(*Snapshot) {}, "provider.identity", []Status{StatusOK}},
		{"identity refreshed", func(s *Snapshot) { s.Identity.Match = false; s.Identity.Refreshed = true }, "provider.identity", []Status{StatusWarn}},
		{"identity refresh write failed", func(s *Snapshot) { s.Identity.Match = false; s.Identity.RefreshErr = "read-only" }, "provider.identity", []Status{StatusWarn}},
		{"identity rate limited", func(s *Snapshot) { s.Identity.Err = Failure{Class: ClassRateLimited}; s.Identity.RateLimit = true }, "provider.identity", []Status{StatusWarn}},
		{"identity unresolved", func(s *Snapshot) { s.Identity.Err = Failure{Class: ClassUnavailable} }, "provider.identity", []Status{StatusFail}},
		{"repo ok", func(*Snapshot) {}, "provider.repo", []Status{StatusOK}},
		{"repo outside a repository", func(s *Snapshot) { s.Repo.Skip = "not in a Git repository" }, "provider.repo", []Status{StatusSkip}},
		{"repo denied twice", func(s *Snapshot) { s.Repo.Missing = []string{"o/a", "o/b"} }, "provider.repo", []Status{StatusFail, StatusFail}},
		{"repo list failed", func(s *Snapshot) { s.Repo.Err = Failure{Class: ClassForbidden} }, "provider.repo", []Status{StatusFail}},
		{"git ok", func(*Snapshot) {}, "git.version", []Status{StatusOK}},
		{"git 2.42", func(s *Snapshot) { s.Git.Version = "git version 2.42.1" }, "git.version", []Status{StatusFail}},
		{"git 3.0", func(s *Snapshot) { s.Git.Version = "git version 3.0.0" }, "git.version", []Status{StatusOK}},
		{"git garbled", func(s *Snapshot) { s.Git.Version = "hello" }, "git.version", []Status{StatusFail}},
		{"git missing", func(s *Snapshot) { s.Git.Err = "executable file not found" }, "gather.git", []Status{StatusFail}},
		{"no token-like variable", func(*Snapshot) {}, "env.tokenlike", []Status{StatusOK}},
		{"two token-like variables", func(s *Snapshot) { s.Env.TokenLike = []string{"GH_TOKEN", "X"} }, "env.tokenlike", []Status{StatusWarn, StatusWarn}},
		{"gh host other", func(s *Snapshot) { s.Env.GHHost = "ghe.test" }, "gh.host", []Status{StatusWarn}},
		{"gh repo with other host", func(s *Snapshot) { s.Env.GHRepo = "ghe.test/o/r" }, "gh.host", []Status{StatusWarn}},
		{"gh repo without host", func(s *Snapshot) { s.Env.GHRepo = "o/r" }, "gh.host", []Status{StatusOK}},
		{"gh host same, other case", func(s *Snapshot) { s.Env.GHHost = "GitHub.com" }, "gh.host", []Status{StatusOK}},
		{"shell note", func(*Snapshot) {}, "context.shell", []Status{StatusInfo}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := healthy()
			tt.mutate(&s)
			got := resultsOf(s, tt.id)
			if len(got) != len(tt.want) {
				t.Fatalf("%s: %d results %v, want %v", tt.id, len(got), got, tt.want)
			}
			for i, r := range got {
				if r.Status != tt.want[i] {
					t.Errorf("%s[%d] = %s, want %s", tt.id, i, r.Status, tt.want[i])
				}
				if r.Status != StatusOK && r.Status != StatusInfo && r.Status != StatusSkip && r.Fix == "" {
					t.Errorf("%s[%d] has status %s and no fix line", tt.id, i, r.Status)
				}
			}
		})
	}
}

func TestRunKeepsTheCatalogOrderAndReportsDeferredChecksAsSkip(t *testing.T) {
	results := Run(healthy())
	pos := map[string]int{}
	for i, r := range results {
		pos[r.ID] = i
	}
	for i := 1; i < len(catalog); i++ {
		if pos[catalog[i-1]] >= pos[catalog[i]] {
			t.Errorf("%s must come before %s", catalog[i-1], catalog[i])
		}
	}
	for id := range DeferredChecks {
		got := resultsOf(healthy(), id)
		if len(got) != 1 || got[0].Status != StatusSkip || got[0].Detail == "" {
			t.Errorf("deferred check %s = %v, want one skip with a reason", id, got)
		}
	}
}

func TestAPanickingCheckBecomesAFailure(t *testing.T) {
	got := safely("x", func(Snapshot) []Result { panic("boom") }, Snapshot{})
	if len(got) != 1 || got[0].ID != "internal" || got[0].Status != StatusFail {
		t.Errorf("safely = %v, want one internal fail", got)
	}
}
