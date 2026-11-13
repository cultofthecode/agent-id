package credential

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func TestCheckKeyFileAcceptsPrivateFiles(t *testing.T) {
	for _, mode := range []fs.FileMode{0o600, 0o400, 0o700, 0o200} {
		state := fileState{path: "/k/app.pem", mode: mode, owner: 501, ownerKnown: true, user: 501}
		if err := checkKeyFile(state); err != nil {
			t.Errorf("mode %04o: %v", mode, err)
		}
	}
}

func TestCheckKeyFileRejectsUnsafeFiles(t *testing.T) {
	cases := map[string]struct {
		state fileState
		want  []string
	}{
		"world-readable": {
			fileState{path: "/k/app.pem", mode: 0o644, owner: 501, ownerKnown: true, user: 501},
			[]string{"private key /k/app.pem:", "mode 0644", "run chmod 600 /k/app.pem"},
		},
		"group-readable": {
			fileState{path: "/k/app.pem", mode: 0o640, owner: 501, ownerKnown: true, user: 501},
			[]string{"mode 0640", "chmod 600 /k/app.pem"},
		},
		"group-writable only": {
			fileState{path: "/k/app.pem", mode: 0o620, owner: 501, ownerKnown: true, user: 501},
			[]string{"mode 0620"},
		},
		"other-executable only": {
			fileState{path: "/k/app.pem", mode: 0o601, owner: 501, ownerKnown: true, user: 501},
			[]string{"mode 0601"},
		},
		"symlink to world-readable": {
			fileState{path: "/k/link.pem", target: "/real/app.pem", mode: 0o644, owner: 501, ownerKnown: true, user: 501},
			[]string{"private key /k/link.pem (symlink to /real/app.pem):", "run chmod 600 /real/app.pem"},
		},
		"other owner": {
			fileState{path: "/k/app.pem", mode: 0o600, owner: 0, ownerKnown: true, user: 501},
			[]string{"owned by uid 0", "current user (uid 501)"},
		},
		"unknown owner": {
			fileState{path: "/k/app.pem", mode: 0o600, user: 501},
			[]string{"owner of the file cannot be determined"},
		},
		"directory": {
			fileState{path: "/k", mode: fs.ModeDir | 0o700, owner: 501, ownerKnown: true, user: 501},
			[]string{"not a regular file (directory)"},
		},
		"named pipe": {
			fileState{path: "/k/fifo", mode: fs.ModeNamedPipe | 0o600, owner: 501, ownerKnown: true, user: 501},
			[]string{"not a regular file (named pipe)"},
		},
		"device": {
			fileState{path: "/dev/zero", mode: fs.ModeDevice | fs.ModeCharDevice | 0o666, owner: 0, ownerKnown: true, user: 501},
			[]string{"not a regular file (device)"},
		},
		"socket": {
			fileState{path: "/k/sock", mode: fs.ModeSocket | 0o600, owner: 501, ownerKnown: true, user: 501},
			[]string{"not a regular file (socket)"},
		},
		"symlink mode": {
			fileState{path: "/k/link", mode: fs.ModeSymlink | 0o777, owner: 501, ownerKnown: true, user: 501},
			[]string{"not a regular file (symlink)"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := checkKeyFile(tc.state)
			if !errors.Is(err, ErrInsecureKeyFile) {
				t.Fatalf("error = %v, want ErrInsecureKeyFile", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	accepted := map[string]struct{ path, home, want string }{
		"absolute":       {"/keys/app.pem", "/home/dev", "/keys/app.pem"},
		"cleaned":        {"/keys/../keys//app.pem", "/home/dev", "/keys/app.pem"},
		"home":           {"~/keys/app.pem", "/home/dev", "/home/dev/keys/app.pem"},
		"home, cleaned":  {"~/a/../app.pem", "/home/dev/", "/home/dev/app.pem"},
		"home ignored":   {"/keys/app.pem", "", "/keys/app.pem"},
		"tilde in path":  {"/keys/~/app.pem", "/home/dev", "/keys/~/app.pem"},
		"space in path":  {"/my keys/app.pem", "/home/dev", "/my keys/app.pem"},
		"dots then home": {"~/../other/app.pem", "/home/dev", "/home/other/app.pem"},
	}
	for name, tc := range accepted {
		t.Run(name, func(t *testing.T) {
			got, err := resolvePath(tc.path, tc.home)
			if err != nil || got != tc.want {
				t.Fatalf("resolvePath(%q, %q) = %q, %v; want %q", tc.path, tc.home, got, err, tc.want)
			}
		})
	}
	rejected := map[string]struct{ path, home string }{
		"empty":         {"", "/home/dev"},
		"relative":      {"keys/app.pem", "/home/dev"},
		"dot relative":  {"./app.pem", "/home/dev"},
		"other user":    {"~dev/app.pem", "/home/dev"},
		"bare tilde":    {"~", "/home/dev"},
		"home itself":   {"~/", "/home/dev"},
		"HOME unset":    {"~/app.pem", ""},
		"HOME relative": {"~/app.pem", "home/dev"},
		"NUL byte":      {"/keys/app.pem\x00.txt", "/home/dev"},
		"newline":       {"/keys/app.pem\nforged line", "/home/dev"},
		"escape":        {"/keys/\x1b[2Japp.pem", "/home/dev"},
		"DEL":           {"/keys/app\x7f.pem", "/home/dev"},
		"scheme kept":   {"file:/keys/app.pem", "/home/dev"},
		"keyring":       {"keyring:app", "/home/dev"},
	}
	for name, tc := range rejected {
		t.Run(name, func(t *testing.T) {
			got, err := resolvePath(tc.path, tc.home)
			if !errors.Is(err, ErrInvalidReference) || got != "" {
				t.Fatalf("resolvePath(%q, %q) = %q, %v; want ErrInvalidReference", tc.path, tc.home, got, err)
			}
		})
	}
}
