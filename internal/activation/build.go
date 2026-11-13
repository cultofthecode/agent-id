package activation

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/cultofthecode/agent-id/internal/profile"
)

var varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Build returns the child environment for plan from the parent environment,
// sorted by variable name. It is pure and idempotent: building from its own
// result gives the same result (re-entry). It never adds a token or key.
//
// The steps are: remove the previous contribution on re-entry; remove every
// AGENT_ID_* variable and Plan.Drop; set the context variables and Plan.Set;
// append Plan.GitConfig after the inherited entries; prepend Plan.PathPrepend
// to PATH.
func Build(parent []string, plan Plan) ([]string, error) {
	if err := checkPlan(plan); err != nil {
		return nil, err
	}
	env := parseEnv(parent)
	ctx, err := ReadContext(parent)
	if err != nil {
		return nil, err
	}
	if _, err := Decide(ctx, plan.Profile); err != nil {
		return nil, err
	}
	if ctx != nil {
		if err := stripContribution(env, ctx); err != nil {
			return nil, err
		}
	}
	count, err := inheritedCount(env)
	if err != nil {
		return nil, err
	}
	removeOwned(env, plan.Drop)
	setContext(env, plan, count)
	for name, value := range plan.Set {
		env[name] = value
	}
	appendGitConfig(env, plan.GitConfig, count)
	prependPath(env, plan.PathPrepend)
	return render(env), nil
}

// stripContribution removes the Git entries that a previous Build appended,
// after checking that nothing changed them since.
func stripContribution(env map[string]string, ctx *Context) error {
	count, err := inheritedCount(env)
	if err != nil || count != ctx.GitConfigEnd {
		return invalid("damaged: the Git configuration entries changed after activation (GIT_CONFIG_COUNT is not %s)", VarGitEnd)
	}
	for n := ctx.GitConfigBase; n < ctx.GitConfigEnd; n++ {
		delete(env, "GIT_CONFIG_KEY_"+strconv.Itoa(n))
		delete(env, "GIT_CONFIG_VALUE_"+strconv.Itoa(n))
	}
	if ctx.GitConfigBase == 0 {
		delete(env, "GIT_CONFIG_COUNT")
	} else {
		env["GIT_CONFIG_COUNT"] = strconv.Itoa(ctx.GitConfigBase)
	}
	return nil
}

func removeOwned(env map[string]string, drop []string) {
	for name := range env {
		if strings.HasPrefix(name, Prefix) {
			delete(env, name)
		}
	}
	for _, name := range drop {
		delete(env, name)
	}
}

func setContext(env map[string]string, plan Plan, base int) {
	env[VarContext] = ContextVersion
	env[VarProfile] = plan.Profile
	env[VarMechanism] = plan.Mechanism
	env[VarBinary] = plan.Binary
	env[VarGitBase] = strconv.Itoa(base)
	env[VarGitEnd] = strconv.Itoa(base + len(plan.GitConfig))
}

func appendGitConfig(env map[string]string, entries []GitConfigEntry, base int) {
	if len(entries) == 0 {
		if env["GIT_CONFIG_COUNT"] == "" {
			delete(env, "GIT_CONFIG_COUNT")
		}
		return
	}
	for i, e := range entries {
		env["GIT_CONFIG_KEY_"+strconv.Itoa(base+i)] = e.Key
		env["GIT_CONFIG_VALUE_"+strconv.Itoa(base+i)] = e.Value
	}
	env["GIT_CONFIG_COUNT"] = strconv.Itoa(base + len(entries))
}

func prependPath(env map[string]string, dirs []string) {
	if len(dirs) == 0 {
		return
	}
	var rest []string
	for _, p := range strings.Split(env["PATH"], ":") {
		if p != "" && !slices.Contains(dirs, p) {
			rest = append(rest, p)
		}
	}
	env["PATH"] = strings.Join(append(slices.Clone(dirs), rest...), ":")
}

func render(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for name, value := range env {
		out = append(out, name+"="+value)
	}
	slices.Sort(out)
	return out
}

// checkPlan refuses a plan with an empty drop list or a value that looks
// like a token or key. It guards against a contributor's bug; the error names
// the variable, never the value.
func checkPlan(plan Plan) error {
	if len(plan.Drop) == 0 {
		return unsafePlan("the drop list is empty")
	}
	if !profileName.MatchString(plan.Profile) || (plan.Mechanism != MechanismExec && plan.Mechanism != MechanismEnv) {
		return unsafePlan("an invalid profile or mechanism")
	}
	if plan.Binary == "" {
		return unsafePlan("an empty binary path")
	}
	check := func(where, value string) error {
		if profile.LooksSecret(value, plan.TokenPrefixes) {
			return unsafePlan("a forbidden value in " + where)
		}
		return nil
	}
	for name, value := range plan.Set {
		if !varName.MatchString(name) || strings.HasPrefix(name, Prefix) {
			return unsafePlan("an invalid variable name")
		}
		if err := check(name, value); err != nil {
			return err
		}
	}
	for i, e := range plan.GitConfig {
		if e.Key == "" {
			return unsafePlan("an empty Git configuration key")
		}
		if err := check("GIT_CONFIG_KEY_"+strconv.Itoa(i), e.Key); err != nil {
			return err
		}
		if err := check("GIT_CONFIG_VALUE_"+strconv.Itoa(i), e.Value); err != nil {
			return err
		}
	}
	for _, dir := range plan.PathPrepend {
		if !strings.HasPrefix(dir, "/") || strings.Contains(dir, ":") {
			return unsafePlan("an invalid PATH directory")
		}
		if err := check("PATH", dir); err != nil {
			return err
		}
	}
	return check(VarBinary, plan.Binary)
}

func unsafePlan(what string) error {
	return failf(ErrPlanUnsafe, "internal error: activation plan contains %s", what)
}
