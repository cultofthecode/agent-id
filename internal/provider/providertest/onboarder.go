package providertest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

// ErrFakeValue is the error of FakeOnboarder.Check.
var ErrFakeValue = errors.New("fake onboarder: invalid value")

// FakeOnboarder is a provider.Onboarder for core tests: its name and fields
// are chosen by the test, Onboard returns a scripted result or error, and
// every request that passed Check is recorded.
type FakeOnboarder struct {
	name   string
	fields []provider.SetupField

	mu       sync.Mutex
	result   provider.SetupResult
	err      error
	requests []provider.SetupRequest
	onboard  func()
}

// NewFakeOnboarder returns a FakeOnboarder named name with fields, whose
// Onboard returns result.
func NewFakeOnboarder(name string, fields []provider.SetupField, result provider.SetupResult) *FakeOnboarder {
	return &FakeOnboarder{name: name, fields: slices.Clone(fields), result: result}
}

// Name implements provider.Onboarder.
func (f *FakeOnboarder) Name() string { return f.name }

// SetupFields implements provider.Onboarder.
func (f *FakeOnboarder) SetupFields() []provider.SetupField { return slices.Clone(f.fields) }

// Check implements provider.Onboarder: every field is present, and no value
// looks like a fake token, a PEM block, or a JWT, or holds a control
// character. The error names the field, never the value.
func (f *FakeOnboarder) Check(r provider.SetupRequest) error {
	for _, field := range f.fields {
		value := r.Values[field.Name]
		switch {
		case value == "":
			return fmt.Errorf("%w: --%s is required", ErrFakeValue, field.Name)
		case LooksSecret(value, []string{testenv.FakeTokenPrefix}) || strings.ContainsFunc(value, unicode.IsControl):
			return fmt.Errorf("%w: --%s looks like a token or key; it is not shown", ErrFakeValue, field.Name)
		}
	}
	return nil
}

// Onboard implements provider.Onboarder: it calls Check, records the
// request, runs the hook of OnOnboard, and returns the scripted result or
// error.
func (f *FakeOnboarder) Onboard(ctx context.Context, r provider.SetupRequest) (provider.SetupResult, error) {
	if err := f.Check(r); err != nil {
		return provider.SetupResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return provider.SetupResult{}, fmt.Errorf("fake onboarder: %w", err)
	}
	f.mu.Lock()
	f.requests = append(f.requests, provider.SetupRequest{Values: cloneValues(r.Values), Dir: r.Dir})
	hook, result, err := f.onboard, f.result, f.err
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if err != nil {
		return provider.SetupResult{}, err
	}
	return result, nil
}

// Fail makes Onboard fail with err, until it is called with nil.
func (f *FakeOnboarder) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// SetResult makes Onboard return result from now on.
func (f *FakeOnboarder) SetResult(result provider.SetupResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result = result
}

// OnOnboard runs hook during every Onboard, after the request is recorded,
// as the network phase would; a test uses it to change the world meanwhile.
func (f *FakeOnboarder) OnOnboard(hook func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onboard = hook
}

// Requests returns the requests that reached the verification, in order.
func (f *FakeOnboarder) Requests() []provider.SetupRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

// OnboarderSubject is an onboarder under contract test.
type OnboarderSubject struct {
	Onboarder provider.Onboarder
	// Valid is a request that Onboard accepts.
	Valid provider.SetupRequest
	// Calls counts the onboarder's calls to the outside: a key loader, the
	// provider's host.
	Calls func() int
}

// hostPattern is a lowercase DNS host name of at least two labels.
var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// LooksSecret reports whether s looks like token or key material by the
// profile store's rule (profile.LooksSecret) with tokenPrefixes.
func LooksSecret(s string, tokenPrefixes []string) bool {
	return profile.LooksSecret(s, tokenPrefixes)
}

// secretShapes returns values that look like secrets: a fake token, one per
// token prefix, a one-line PEM header, and a JWT shape.
func secretShapes(tokenPrefixes []string) []string {
	shapes := []string{testenv.FakeToken("contract"), "-----BEGIN RSA PRIVATE KEY-----", "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiIxIn0.c2ln"}
	for _, prefix := range tokenPrefixes {
		shapes = append(shapes, prefix+"contractvalue")
	}
	return shapes
}

// RunOnboarderContract checks the contract of provider.Onboarder against s:
// its fields, the refusal of missing and secret-shaped values without a call
// to the outside and without echoing them, and a successful result without
// secrets. tokenPrefixes are the provider's token prefixes. It reports
// failures with t.Errorf only, so a test can pass a testenv.Recorder to prove
// that a broken onboarder fails it.
func RunOnboarderContract(t testing.TB, s OnboarderSubject, tokenPrefixes []string) {
	t.Helper()
	checkSetupFields(t, s.Onboarder)
	checkMissingValues(t, s)
	checkSecretValues(t, s, tokenPrefixes)
	checkResult(t, s, tokenPrefixes)
}

// fieldPattern is the shape of a setup field name.
var fieldPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// checkSetupFields checks that the fields are non-empty, unique, and named
// like flags.
func checkSetupFields(t testing.TB, o provider.Onboarder) {
	t.Helper()
	if !fieldPattern.MatchString(o.Name()) {
		t.Errorf("onboarder name %q is not a provider name", o.Name())
	}
	fields := o.SetupFields()
	if len(fields) == 0 {
		t.Errorf("%s: no setup fields", o.Name())
	}
	seen := map[string]bool{}
	for _, field := range fields {
		if !fieldPattern.MatchString(field.Name) || field.Name == "profile" {
			t.Errorf("%s: field name %q is not a flag name", o.Name(), field.Name)
		}
		if field.Usage == "" {
			t.Errorf("%s: field %q has no usage", o.Name(), field.Name)
		}
		if seen[field.Name] {
			t.Errorf("%s: field %q appears twice", o.Name(), field.Name)
		}
		seen[field.Name] = true
	}
}

// checkMissingValues checks that a request without one field fails in Check
// and in Onboard, without a call to the outside.
func checkMissingValues(t testing.TB, s OnboarderSubject) {
	t.Helper()
	for _, field := range s.Onboarder.SetupFields() {
		r := withValue(s.Valid, field.Name, "")
		delete(r.Values, field.Name)
		expectRefused(t, s, r, "missing --"+field.Name, "")
	}
}

// checkSecretValues checks that a secret-shaped value in any field fails in
// Check and in Onboard, without a call to the outside and without being
// echoed.
func checkSecretValues(t testing.TB, s OnboarderSubject, tokenPrefixes []string) {
	t.Helper()
	for _, field := range s.Onboarder.SetupFields() {
		for _, shape := range secretShapes(tokenPrefixes) {
			expectRefused(t, s, withValue(s.Valid, field.Name, shape), "secret-shaped --"+field.Name, shape)
		}
	}
}

// expectRefused checks that r fails in Check and in Onboard without a call
// to the outside, and that neither error contains echo.
func expectRefused(t testing.TB, s OnboarderSubject, r provider.SetupRequest, what, echo string) {
	t.Helper()
	before := s.Calls()
	checkErr := s.Onboarder.Check(r)
	_, onboardErr := s.Onboarder.Onboard(context.Background(), r)
	for name, err := range map[string]error{"Check": checkErr, "Onboard": onboardErr} {
		switch {
		case err == nil:
			t.Errorf("%s: %s accepted", what, name)
		case echo != "" && strings.Contains(err.Error(), echo):
			t.Errorf("%s: the %s error echoes the value", what, name)
		}
	}
	if calls := s.Calls() - before; calls != 0 {
		t.Errorf("%s: %d calls to the outside, want none", what, calls)
	}
}

// checkResult checks a successful Onboard: a host, an account, a non-empty
// settings object, and no secret anywhere in the result.
func checkResult(t testing.TB, s OnboarderSubject, tokenPrefixes []string) {
	t.Helper()
	if err := s.Onboarder.Check(s.Valid); err != nil {
		t.Errorf("Check of the valid request: %v", err)
	}
	result, err := s.Onboarder.Onboard(context.Background(), s.Valid)
	if err != nil {
		t.Errorf("Onboard of the valid request: %v", err)
		return
	}
	if !hostPattern.MatchString(result.Host) {
		t.Errorf("result host %q is not a host name", result.Host)
	}
	if result.Account == "" {
		t.Errorf("result has no account")
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(result.Settings, &settings); err != nil || len(settings) == 0 {
		t.Errorf("result settings are not a non-empty JSON object")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Errorf("encode the result: %v", err)
		return
	}
	if LooksSecret(string(encoded), append([]string{testenv.FakeTokenPrefix}, tokenPrefixes...)) {
		t.Errorf("the result holds what looks like a token or key")
	}
}

// withValue returns a copy of r with field set to value.
func withValue(r provider.SetupRequest, field, value string) provider.SetupRequest {
	values := cloneValues(r.Values)
	values[field] = value
	return provider.SetupRequest{Values: values, Dir: r.Dir}
}

// cloneValues copies a value map.
func cloneValues(values map[string]string) map[string]string {
	clone := make(map[string]string, len(values))
	for k, v := range values {
		clone[k] = v
	}
	return clone
}
