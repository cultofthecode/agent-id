package testenv

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// developerEnvironment is the environment of the test binary before any test
// isolated it. BuildBinary reads the Go caches from it.
var developerEnvironment = os.Environ()

// goCaches holds the module and build caches of the developer.
type goCaches struct {
	GOMODCACHE string
	GOCACHE    string
}

// developerGoCaches asks go once for the caches of the developer's
// environment.
var developerGoCaches = sync.OnceValues(func() (goCaches, error) {
	cmd := exec.Command("go", "env", "-json", "GOMODCACHE", "GOCACHE")
	cmd.Env = developerEnvironment
	out, err := cmd.Output()
	if err != nil {
		return goCaches{}, fmt.Errorf("go env: %w", err)
	}
	var caches goCaches
	if err := json.Unmarshal(out, &caches); err != nil {
		return goCaches{}, fmt.Errorf("go env: %w", err)
	}
	return caches, nil
})

// BuildBinary builds the package pkg, an import path of this module, into
// env.Root/bin and returns the path of the executable. It reuses the
// developer's module and build caches, never downloads anything
// (GOPROXY=off, GOTOOLCHAIN=local), and installs nothing. Call Isolate first.
func BuildBinary(t testing.TB, env Env, pkg string) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("testenv: go is not on PATH: %v", err)
	}
	caches, err := developerGoCaches()
	if err != nil {
		t.Fatalf("testenv: find the Go caches: %v", err)
	}
	out := filepath.Join(env.Root, "bin", path.Base(pkg))
	cmd := exec.Command(goTool, "build", "-o", out, pkg)
	cmd.Env = append(os.Environ(),
		"GOMODCACHE="+caches.GOMODCACHE,
		"GOCACHE="+caches.GOCACHE,
		"GOPROXY=off",
		"GOFLAGS=-mod=readonly",
		"GOTOOLCHAIN=local",
		"GOWORK=off",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("testenv: go build %s: %v\n%s", pkg, err, strings.TrimSpace(string(output)))
	}
	return out
}
