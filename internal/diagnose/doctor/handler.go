package doctor

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cultofthecode/agent-id/internal/diagnose"
	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/token"
)

// commandDeadline bounds the whole command, provider calls included.
const commandDeadline = 60 * time.Second

// Handler runs doctor. OpenStore is called when a doctor runs, so that a
// broken environment is a check result and not a crash.
type Handler struct {
	OpenStore    func() (Store, error)
	Supports     func(providerName string) bool
	OpenProvider func(profile.Profile) (provider.Provider, error)
	Git          Git
	CacheDir     func() (string, error)
	Environ      func() []string
	// Shell scans the start files of the shells; nil skips the scan.
	Shell  diagnose.ShellScan
	Output diagnose.Output
	Now    func() time.Time
}

// Handle gathers the snapshot, runs the checks, and returns the rendered
// report and the exit status. A non-nil error is a *diagnose.ExitError for
// output that was withheld.
func (h *Handler) Handle(ctx context.Context, in Input) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, commandDeadline)
	defer cancel()
	snap := h.gather(ctx, in.Profile)
	report := diagnose.DoctorReport{Profile: in.Profile, Provider: snap.Provider, Host: snap.Host, Results: diagnose.Run(snap)}
	out, err := h.Output.Doctor(report, in.JSON)
	if err != nil {
		return nil, diagnose.ExitWithheld, diagnose.WithheldError()
	}
	return out, report.ExitCode(), nil
}

// gather collects the snapshot in the order of the spec: the configuration
// and the key, the token and the identity, then Git and the environment.
func (h *Handler) gather(ctx context.Context, name string) diagnose.Snapshot {
	snap := diagnose.Snapshot{Profile: name}
	prof, ok := h.gatherConfig(&snap, name)
	var prov provider.Provider
	var tok provider.Token
	if ok {
		prov = h.gatherProvider(&snap, prof)
	} else {
		snap.Key.Skip, snap.Token.Skip, snap.Identity.Skip = "the profile cannot be used", "the profile cannot be used", "the profile cannot be used"
	}
	h.gatherCache(&snap)
	if prov != nil {
		tok = h.gatherToken(ctx, &snap, prov)
		h.gatherIdentity(ctx, &snap, prov, name)
	}
	h.gatherGit(ctx, &snap, prov, tok)
	h.gatherEnv(&snap)
	if h.Shell != nil {
		snap.Shell = h.Shell()
	}
	return snap
}

// gatherConfig loads the configuration and resolves the profile. It reports
// whether the profile can be used.
func (h *Handler) gatherConfig(snap *diagnose.Snapshot, name string) (profile.Profile, bool) {
	store, err := h.OpenStore()
	if err != nil {
		snap.Config.Load = diagnose.ClassifyError(err)
		return profile.Profile{}, false
	}
	snap.Config.Path = store.Path()
	config, err := store.Load()
	if err != nil {
		snap.Config.Load = diagnose.Failure{Message: err.Error()}
		return profile.Profile{}, false
	}
	for _, n := range config.Names() {
		if !h.Supports(config.Profiles[n].Provider) {
			snap.Config.Unsupported = append(snap.Config.Unsupported, n)
		}
	}
	prof, err := store.Resolve(name)
	if err != nil {
		snap.Config.Profile = diagnose.Failure{Message: err.Error()}
		return profile.Profile{}, false
	}
	snap.Provider, snap.Host = prof.Provider, prof.Host
	return prof, true
}

// gatherProvider opens the provider, which loads the private key through
// the credential source; a failure is the key result.
func (h *Handler) gatherProvider(snap *diagnose.Snapshot, prof profile.Profile) provider.Provider {
	prov, err := h.OpenProvider(prof)
	if err != nil {
		f := diagnose.ClassifyError(err)
		f.Class = diagnose.ClassKey
		snap.Key.Err = f
		snap.Token.Skip, snap.Identity.Skip = "the private key cannot be used", "the private key cannot be used"
		return nil
	}
	snap.Provider, snap.Host = prov.Name(), prov.Host()
	return prov
}

// gatherCache checks the token cache directory without creating it or
// reading an entry.
func (h *Handler) gatherCache(snap *diagnose.Snapshot) {
	dir, err := h.CacheDir()
	if err != nil {
		snap.Cache.Err = diagnose.ClassifyError(err)
		return
	}
	exists, err := token.CheckCacheDir(dir)
	snap.Cache.Exists = exists
	if err != nil {
		snap.Cache.Err = diagnose.ClassifyError(err)
	}
}

// gatherToken mints one token, directly through the provider, to prove that
// a mint works now. The token stays a local value; only its expiry enters
// the snapshot.
func (h *Handler) gatherToken(ctx context.Context, snap *diagnose.Snapshot, prov provider.Provider) provider.Token {
	tok, err := prov.Token(ctx)
	if err != nil {
		snap.Token.Err = diagnose.ClassifyError(err)
		return provider.Token{}
	}
	snap.Token.ExpiresInMinutes = int(tok.ExpiresAt.Sub(h.now()) / time.Minute)
	return tok
}

// gatherIdentity compares the cached identity with the host's bot and
// refreshes the cache when it is missing or differs. It is the only write
// of doctor, and it writes the identity member only.
func (h *Handler) gatherIdentity(ctx context.Context, snap *diagnose.Snapshot, prov provider.Provider, name string) {
	check, err := prov.VerifyIdentity(ctx)
	if err != nil {
		snap.Identity.Err = diagnose.ClassifyError(err)
		snap.Identity.RateLimit = errors.Is(err, provider.ErrRateLimited)
		return
	}
	snap.Identity.Bot, snap.Identity.Match = check.Bot, check.Match
	if check.Match || check.Refresh == nil {
		return
	}
	if err := h.refresh(name, check); err != nil {
		snap.Identity.RefreshErr = err.Error()
		return
	}
	snap.Identity.Refreshed = true
}

// refresh writes the refreshed provider block of the profile through the
// store's locked, atomic write.
func (h *Handler) refresh(name string, check provider.IdentityCheck) error {
	store, err := h.OpenStore()
	if err != nil {
		return err
	}
	return store.Write(func(c *profile.Config) error {
		p, ok := c.Profiles[name]
		if !ok {
			return profile.Errorf(profile.ErrProfileNotFound, "profile %s disappeared while doctor ran", profile.Quote(name))
		}
		settings, err := check.Refresh(p.Settings)
		if err != nil {
			return err
		}
		p.Settings = settings
		c.Profiles[name] = p
		return nil
	})
}

// gatherGit runs git --version and checks the repositories of the remotes
// that name the provider's host.
func (h *Handler) gatherGit(ctx context.Context, snap *diagnose.Snapshot, prov provider.Provider, tok provider.Token) {
	version, err := h.Git.Version(ctx)
	if err != nil {
		snap.Git.Err = err.Error()
	}
	snap.Git.Version = version
	if err != nil {
		snap.Repo.Skip = "git is not available"
		return
	}
	h.gatherRepos(ctx, snap, prov, tok)
}

// gatherRepos lists the installation's repositories and compares them with
// those of the remotes.
func (h *Handler) gatherRepos(ctx context.Context, snap *diagnose.Snapshot, prov provider.Provider, tok provider.Token) {
	if prov == nil || tok == (provider.Token{}) {
		snap.Repo.Skip = "no installation token"
		return
	}
	urls, inRepo, err := h.Git.RemoteURLs(ctx)
	if err != nil {
		snap.Repo.Err = diagnose.Failure{Class: diagnose.ClassConfig, Message: "git cannot list the remotes: " + err.Error()}
		return
	}
	if !inRepo {
		snap.Repo.Skip = "not in a Git repository"
		return
	}
	wanted := repositoriesOn(urls, prov.Host())
	if len(wanted) == 0 {
		snap.Repo.Skip = "no remote on " + prov.Host()
		return
	}
	listed, err := prov.Repositories(ctx, tok)
	if err != nil {
		snap.Repo.Err = diagnose.ClassifyError(err)
		return
	}
	snap.Repo.Checked = len(wanted)
	snap.Repo.Missing = missing(wanted, listed)
}

// missing returns the wanted repositories that listed lacks, compared
// without regard to case.
func missing(wanted, listed []string) []string {
	have := make(map[string]bool, len(listed))
	for _, r := range listed {
		have[strings.ToLower(r)] = true
	}
	var out []string
	for _, w := range wanted {
		if !have[strings.ToLower(w)] {
			out = append(out, w)
		}
	}
	return out
}

// gatherEnv records the names of the variables that hold a token-shaped
// value, and the values of GH_HOST and GH_REPO only.
func (h *Handler) gatherEnv(snap *diagnose.Snapshot) {
	for _, entry := range h.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch name {
		case "GH_HOST":
			snap.Env.GHHost = value
		case "GH_REPO":
			snap.Env.GHRepo = value
		}
		if h.Output.Patterns.Matches(value) {
			snap.Env.TokenLike = append(snap.Env.TokenLike, name)
		}
	}
}

func (h *Handler) now() time.Time {
	if h.Now == nil {
		return time.Now()
	}
	return h.Now()
}
