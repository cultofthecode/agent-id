package token

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestCheckCacheDir(t *testing.T) {
	env := testenv.Isolate(t)
	base := filepath.Join(env.Home, "checks")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(base, "private")
	open := filepath.Join(base, "open")
	for dir, mode := range map[string]os.FileMode{private: 0o700, open: 0o755} {
		if err := os.Mkdir(dir, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		dir        string
		wantExists bool
		wantErr    error
	}{
		{"private directory", private, true, nil},
		{"missing directory", filepath.Join(base, "missing"), false, nil},
		{"missing parent", filepath.Join(base, "missing", "child"), false, nil},
		{"group-readable directory", open, false, ErrInsecureCache},
		{"symbolic link", link, false, ErrInsecureCache},
		{"regular file", file, false, ErrInsecureCache},
		{"relative path", "tokens", false, ErrNoCacheDir},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exists, err := CheckCacheDir(tt.dir)
			if exists != tt.wantExists || !errors.Is(err, tt.wantErr) {
				t.Errorf("CheckCacheDir(%q) = %v, %v; want %v, %v", tt.dir, exists, err, tt.wantExists, tt.wantErr)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(base, "missing")); !os.IsNotExist(err) {
		t.Error("CheckCacheDir created a directory")
	}
}
