package testenv

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

// errGHWithoutFakeToken is returned by GH when GH_TOKEN holds no fake token.
var errGHWithoutFakeToken = errors.New("testenv: refusing to run gh without a fake GH_TOKEN")

// GH runs gh with args in the environment of the test and returns its
// combined output. It refuses to start gh, and fails the test, unless
// GH_TOKEN holds a fake token: gh treats an empty GH_TOKEN as unset and can
// then read a token from the macOS keychain, which Isolate cannot redirect.
// It skips the test when gh is not installed. Call Isolate first, then set
// GH_TOKEN to a FakeToken.
func GH(t testing.TB, args ...string) ([]byte, error) {
	t.Helper()
	gh, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("testenv: gh is not on PATH")
	}
	if !IsFakeToken(os.Getenv("GH_TOKEN")) {
		t.Errorf("testenv: GH_TOKEN holds no fake token; gh could fall back to the developer's keychain")
		return nil, errGHWithoutFakeToken
	}
	return exec.Command(gh, args...).CombinedOutput()
}
