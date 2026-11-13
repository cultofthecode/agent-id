package providertest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

var fakeFields = []provider.SetupField{{Name: "account-id", Usage: "the account"}, {Name: "key", Usage: "the key file"}}

func fakeResult() provider.SetupResult {
	return provider.SetupResult{
		Host:         FakeHost,
		Settings:     json.RawMessage(`{"account_id":7}`),
		Account:      FakeUsername,
		Identity:     provider.Identity{Name: "fake bot", Email: "bot@" + FakeHost},
		Details:      []provider.SetupDetail{{Label: "Account", Value: "7"}},
		Repositories: []string{"o/a", "o/b"},
	}
}

func fakeSubject(f *FakeOnboarder) OnboarderSubject {
	return OnboarderSubject{
		Onboarder: f,
		Valid:     provider.SetupRequest{Values: map[string]string{"account-id": "7", "key": "/k.pem"}, Dir: "/work"},
		Calls:     func() int { return len(f.Requests()) },
	}
}

func TestFakeOnboarderMeetsTheContract(t *testing.T) {
	testenv.Isolate(t)
	RunOnboarderContract(t, fakeSubject(NewFakeOnboarder("acme", fakeFields, fakeResult())), nil)
}

func TestOnboarderContractCatchesBrokenOnboarders(t *testing.T) {
	tests := []struct {
		name   string
		fields []provider.SetupField
		result func(provider.SetupResult) provider.SetupResult
		want   string
	}{
		{"token in a repository name", fakeFields, func(r provider.SetupResult) provider.SetupResult {
			r.Repositories = append(r.Repositories, testenv.FakeToken("leak"))
			return r
		}, "the result holds what looks like a token or key"},
		{"PEM in a detail", fakeFields, func(r provider.SetupResult) provider.SetupResult {
			r.Details = []provider.SetupDetail{{Label: "Key", Value: "-----BEGIN RSA PRIVATE KEY-----"}}
			return r
		}, "the result holds what looks like a token or key"},
		{"no account", fakeFields, func(r provider.SetupResult) provider.SetupResult {
			r.Account = ""
			return r
		}, "result has no account"},
		{"host with a scheme", fakeFields, func(r provider.SetupResult) provider.SetupResult {
			r.Host = "https://" + FakeHost
			return r
		}, "is not a host name"},
		{"empty settings", fakeFields, func(r provider.SetupResult) provider.SetupResult {
			r.Settings = json.RawMessage(`{}`)
			return r
		}, "result settings are not a non-empty JSON object"},
		{"duplicate field", append(fakeFields, fakeFields[0]), func(r provider.SetupResult) provider.SetupResult { return r }, `field "account-id" appears twice`},
		{"field named like no flag", []provider.SetupField{{Name: "Key_File", Usage: "x"}}, func(r provider.SetupResult) provider.SetupResult { return r }, "is not a flag name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.Isolate(t)
			recorder := testenv.NewRecorder(t)
			f := NewFakeOnboarder("acme", tt.fields, tt.result(fakeResult()))
			s := fakeSubject(f)
			for _, field := range tt.fields {
				if _, ok := s.Valid.Values[field.Name]; !ok {
					s.Valid.Values[field.Name] = "1"
				}
			}
			RunOnboarderContract(recorder, s, nil)
			if !recorder.Reported(tt.want) {
				t.Errorf("contract errors = %q, want one containing %q", recorder.Errors(), tt.want)
			}
		})
	}
}

func TestOnboarderContractCatchesAnEcho(t *testing.T) {
	testenv.Isolate(t)
	recorder := testenv.NewRecorder(t)
	RunOnboarderContract(recorder, OnboarderSubject{Onboarder: echoOnboarder{}, Valid: provider.SetupRequest{Values: map[string]string{"key": "/k"}}, Calls: func() int { return 0 }}, nil)
	if !recorder.Reported("echoes the value") {
		t.Errorf("contract errors = %q, want an echo failure", recorder.Errors())
	}
}

// echoOnboarder refuses every value and echoes it.
type echoOnboarder struct{}

func (echoOnboarder) Name() string { return "echo" }

func (echoOnboarder) SetupFields() []provider.SetupField {
	return []provider.SetupField{{Name: "key", Usage: "a key"}}
}

func (echoOnboarder) Check(r provider.SetupRequest) error {
	return errors.New("bad value " + r.Values["key"])
}

func (o echoOnboarder) Onboard(_ context.Context, r provider.SetupRequest) (provider.SetupResult, error) {
	return provider.SetupResult{}, o.Check(r)
}

func TestFakeOnboarderScripts(t *testing.T) {
	testenv.Isolate(t)
	f := NewFakeOnboarder("acme", fakeFields, fakeResult())
	s := fakeSubject(f)
	f.Fail(provider.ErrNotFound)
	if _, err := f.Onboard(context.Background(), s.Valid); !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("Onboard error = %v, want ErrNotFound", err)
	}
	f.Fail(nil)
	hooked := false
	f.OnOnboard(func() { hooked = true })
	result, err := f.Onboard(context.Background(), s.Valid)
	if err != nil || result.Account != FakeUsername || !hooked {
		t.Fatalf("Onboard = %+v, %v (hook run %v)", result, err, hooked)
	}
	if got := f.Requests(); len(got) != 2 || got[0].Values["account-id"] != "7" || got[0].Dir != "/work" {
		t.Errorf("Requests = %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Onboard(ctx, s.Valid); !errors.Is(err, context.Canceled) {
		t.Errorf("Onboard after cancel = %v", err)
	}
}
