package testenv

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMain(m *testing.M) { Main(m) }

func TestPersonalConfigPaths(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{
			"defaults",
			map[string]string{"HOME": "/home/dev"},
			[]string{"/home/dev/.config/gh", "/home/dev/.config/ghapp", "/home/dev/.config/git", "/home/dev/.gitconfig"},
		},
		{
			"XDG and overrides",
			map[string]string{"HOME": "/home/dev", "XDG_CONFIG_HOME": "/xdg", "GIT_CONFIG_GLOBAL": "/etc/dev.gitconfig", "GH_CONFIG_DIR": "/gh"},
			[]string{"/etc/dev.gitconfig", "/gh", "/home/dev/.config/gh", "/home/dev/.config/ghapp", "/home/dev/.gitconfig", "/xdg/gh", "/xdg/ghapp", "/xdg/git"},
		},
		{
			"override equal to a default",
			map[string]string{"HOME": "/home/dev", "GIT_CONFIG_GLOBAL": "/home/dev/.gitconfig"},
			[]string{"/home/dev/.config/gh", "/home/dev/.config/ghapp", "/home/dev/.config/git", "/home/dev/.gitconfig"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := personalConfigPaths(func(name string) string { return tt.env[name] })
			if !slices.Equal(got, tt.want) {
				t.Errorf("personalConfigPaths = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSnapshotDetectsEveryChange(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, gitconfig, ghDir string)
		want   []string
	}{
		{"no change", func(*testing.T, string, string) {}, nil},
		{"content", func(t *testing.T, gitconfig, _ string) {
			write(t, gitconfig, "[user]\n\tname = leaked\n")
		}, []string{".gitconfig"}},
		{"mode", func(t *testing.T, gitconfig, _ string) {
			if err := os.Chmod(gitconfig, 0o644); err != nil {
				t.Fatal(err)
			}
		}, []string{".gitconfig"}},
		{"new file", func(t *testing.T, _, ghDir string) {
			write(t, filepath.Join(ghDir, "hosts.yml"), "github.com: {}\n")
		}, []string{".config/gh/hosts.yml"}},
		{"removed directory", func(t *testing.T, _, ghDir string) {
			if err := os.RemoveAll(ghDir); err != nil {
				t.Fatal(err)
			}
		}, []string{".config/gh", ".config/gh/config.yml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := Isolate(t)
			gitconfig := filepath.Join(env.Home, ".gitconfig")
			ghDir := filepath.Join(env.Home, ".config", "gh")
			write(t, filepath.Join(ghDir, "config.yml"), "version: 1\n")
			paths := personalConfigPaths(func(name string) string { return map[string]string{"HOME": env.Home}[name] })
			before := snapshot(paths)
			tt.change(t, gitconfig, ghDir)
			var got []string
			for _, path := range changedPaths(before, snapshot(paths)) {
				rel, err := filepath.Rel(env.Home, path)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, filepath.ToSlash(rel))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("changed = %q, want %q", got, tt.want)
			}
		})
	}
}

// write creates the file at path, and its directory, with content.
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotWatchesGhappByMetadataWithoutReadingContent(t *testing.T) {
	env := Isolate(t)
	dir := filepath.Join(env.Home, ".config", "ghapp")
	key := filepath.Join(dir, "key.pem")
	write(t, key, "secret")
	paths := personalConfigPaths(func(name string) string { return map[string]string{"HOME": env.Home}[name] })
	if !slices.Contains(paths, dir) {
		t.Fatalf("paths %q lack %s", paths, dir)
	}
	if err := os.Chmod(key, 0o000); err != nil {
		t.Fatal(err)
	}
	before := snapshot(paths)
	if got := before[key]; got == "unreadable" || got == "absent" {
		t.Fatalf("fingerprint of an unreadable file = %q, want metadata without opening it", got)
	}
	if err := os.Chmod(key, 0o600); err != nil {
		t.Fatal(err)
	}
	if changed := changedPaths(before, snapshot(paths)); !slices.Equal(changed, []string{key}) {
		t.Errorf("mode change: changed = %q, want the key", changed)
	}
	before = snapshot(paths)
	write(t, key, "secret-longer")
	if changed := changedPaths(before, snapshot(paths)); !slices.Equal(changed, []string{key}) {
		t.Errorf("size change: changed = %q, want the key", changed)
	}
}
