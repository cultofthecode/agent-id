package credential

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestCheckKeyFileRejectsExtendedACL(t *testing.T) {
	state := fileState{path: "~/app.pem", mode: 0o600, owner: 501, ownerKnown: true, user: 501, acl: true}
	err := checkKeyFile(state)
	if !errors.Is(err, ErrInsecureKeyFile) {
		t.Fatalf("error = %v, want %v", err, ErrInsecureKeyFile)
	}
	for _, w := range []string{"private key ~/app.pem:", "extended ACL", "run chmod -N ~/app.pem"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error = %q, want it to contain %q", err, w)
		}
	}
	state.target = "/real/app.pem"
	if err := checkKeyFile(state); err == nil || !strings.Contains(err.Error(), "chmod -N /real/app.pem") {
		t.Errorf("symlink: error = %v, want the fix to name the target", err)
	}
}

func TestCheckKeyFileFailsClosedWhenACLIsUnknown(t *testing.T) {
	state := fileState{path: "~/app.pem", mode: 0o600, owner: 501, ownerKnown: true, user: 501, aclErr: errors.New("boom")}
	err := checkKeyFile(state)
	if !errors.Is(err, ErrInsecureKeyFile) || !strings.Contains(err.Error(), "cannot be determined") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %q echoes the system error", err)
	}
}

func addACL(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("extended ACLs through chmod +a exist on macOS only")
	}
	out, err := exec.Command("/bin/chmod", "+a", "everyone allow read", path).CombinedOutput()
	if err != nil {
		t.Skipf("chmod +a is not available here: %v: %s", err, out)
	}
}

func TestFileRejectsKeyWithExtendedACL(t *testing.T) {
	env := testenv.Isolate(t)
	path := filepath.Join(env.Home, "app.pem")
	writeKey(t, path, pkcs1PEM(t), 0o600)
	addACL(t, path)
	key, err := File{Path: path}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInsecureKeyFile, "private key ~/app.pem:", "extended ACL", "run chmod -N ~/app.pem")
	if out, err := exec.Command("/bin/chmod", "-N", path).CombinedOutput(); err != nil {
		t.Fatalf("chmod -N: %v: %s", err, out)
	}
	if key, err := (File{Path: path}).PrivateKeyPEM(); err != nil || key == nil {
		t.Fatalf("after chmod -N: %v", err)
	}
}

func TestFileRejectsSymlinkToKeyWithExtendedACL(t *testing.T) {
	env := testenv.Isolate(t)
	target := filepath.Join(env.Root, "dotfiles", "app.pem")
	writeKey(t, target, pkcs1PEM(t), 0o600)
	addACL(t, target)
	link := filepath.Join(env.Home, "app.pem")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	key, err := File{Path: link}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInsecureKeyFile, "extended ACL", "symlink to")
}
