package token

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

// Variables that turn the test binary into one child of the process test.
const (
	childDirVar     = "TOKEN_TEST_CHILD_DIR"
	childMintLogVar = "TOKEN_TEST_CHILD_MINT_LOG"
	childReadyVar   = "TOKEN_TEST_CHILD_READY_DIR"
	childGoVar      = "TOKEN_TEST_CHILD_GO_FILE"
	processes       = 20
)

func TestMain(m *testing.M) {
	if os.Getenv(childDirVar) != "" {
		os.Exit(runChild())
	}
	testenv.Main(m)
}

func TestConcurrentProcessesMintOnce(t *testing.T) {
	m := newManager(t, newClock())
	root := t.TempDir()
	mintLog, readyDir, goFile := filepath.Join(root, "mints"), filepath.Join(root, "ready"), filepath.Join(root, "go")
	mkdir(t, readyDir)
	env := append(os.Environ(), childDirVar+"="+m.Dir, childMintLogVar+"="+mintLog, childReadyVar+"="+readyDir, childGoVar+"="+goFile)
	children := make([]*exec.Cmd, processes)
	outputs := make([]*bytes.Buffer, processes)
	for i := range children {
		outputs[i] = &bytes.Buffer{}
		children[i] = exec.Command(os.Args[0], "-test.run=^$")
		children[i].Env, children[i].Stdout, children[i].Stderr = env, outputs[i], outputs[i]
		if err := children[i].Start(); err != nil {
			t.Fatalf("start child %d: %v", i, err)
		}
	}
	waitForReady(t, readyDir)
	if err := os.WriteFile(goFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	hashes := map[string]int{}
	for i, child := range children {
		err := child.Wait()
		output := outputs[i].String()
		if strings.Contains(output, testenv.FakeTokenPrefix) {
			t.Errorf("child %d printed a token", i)
		}
		if err != nil {
			t.Errorf("child %d: %v: %s", i, err, output)
			continue
		}
		hashes[strings.TrimSpace(output)]++
	}
	mints, err := os.ReadFile(mintLog)
	if err != nil {
		t.Fatalf("read the mint log: %v", err)
	}
	if n := bytes.Count(mints, []byte("\n")); n != 1 {
		t.Errorf("the provider saw %d mints, want 1", n)
	}
	if len(hashes) != 1 {
		t.Errorf("the processes got %d different tokens, want one", len(hashes))
	}
}

// waitForReady waits until every child has reported that it is ready.
func waitForReady(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) == processes {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the children did not get ready")
}

// runChild reports ready, waits for the start file, gets a token from the
// shared cache, and prints its SHA-256, never the token.
func runChild() int {
	ready := filepath.Join(os.Getenv(childReadyVar), strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(ready, nil, 0o600); err != nil {
		return fail(err)
	}
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(time.Millisecond) {
		if _, err := os.Stat(os.Getenv(childGoVar)); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fail(fmt.Errorf("no start signal"))
		}
	}
	m := &Manager{Dir: os.Getenv(childDirVar)}
	token, err := m.Token(context.Background(), loggingSource{log: os.Getenv(childMintLogVar)})
	if err != nil {
		return fail(err)
	}
	sum := sha256.Sum256([]byte(token.Value))
	fmt.Println(hex.EncodeToString(sum[:]))
	return 0
}

func fail(err error) int {
	_, _ = fmt.Fprintln(os.Stderr, err)
	return 1
}

// loggingSource records each mint in a shared log and issues a token unique
// to its process, so that 20 mints can never look like one.
type loggingSource struct{ log string }

func (s loggingSource) Name() string         { return "fake" }
func (s loggingSource) Host() string         { return "git.agent-id.test" }
func (s loggingSource) TokenSubject() string { return testSubject }

func (s loggingSource) Token(context.Context) (provider.Token, error) {
	f, err := os.OpenFile(s.log, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return provider.Token{}, err
	}
	_, err = fmt.Fprintln(f, os.Getpid())
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return provider.Token{}, err
	}
	time.Sleep(200 * time.Millisecond)
	return provider.Token{Value: testenv.FakeToken("proc_" + strconv.Itoa(os.Getpid())), ExpiresAt: time.Now().Add(time.Hour)}, nil
}
