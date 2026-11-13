package activation

import (
	"github.com/cultofthecode/agent-id/internal/gitint"
	"github.com/cultofthecode/agent-id/internal/provider"
)

// withTransport appends the Git transport group of #22 to the plan: the
// credential helper of the provider's host, useHttpPath, and the SSH→HTTPS
// rewrites. It runs after the reset entries of PlanFor, so the helper is the
// only one for that host.
func withTransport(plan Plan, a provider.Activator) (Plan, error) {
	entries, err := gitint.TransportEntries(plan.Binary, plan.Profile, a.Host())
	if err != nil {
		return Plan{}, unsafePlan("an unusable Git transport entry (" + err.Error() + ")")
	}
	for _, e := range entries {
		plan.GitConfig = append(plan.GitConfig, GitConfigEntry{Key: e.Key, Value: e.Value})
	}
	return plan, nil
}
