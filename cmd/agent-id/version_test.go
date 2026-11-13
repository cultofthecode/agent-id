package main

import (
	"runtime/debug"
	"testing"
)

func TestVersionText(t *testing.T) {
	noInfo := func() (*debug.BuildInfo, bool) { return nil, false }
	withInfo := func(v string) buildInfoReader {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: v}}, true
		}
	}
	tests := []struct {
		name   string
		linked string
		commit string
		reader buildInfoReader
		want   string
	}{
		{"linked version wins", "v1.2.3", "", withInfo("v9.9.9"), "1.2.3"},
		{"linked version with commit", "1.2.3", "abc123", noInfo, "1.2.3 (commit abc123)"},
		{"module version from build info", "", "", withInfo("v0.4.0"), "0.4.0"},
		{"devel build is dev", "", "", withInfo("(devel)"), "dev"},
		{"empty build info is dev", "", "", withInfo(""), "dev"},
		{"no build info is dev", "", "", noInfo, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionText(tt.linked, tt.commit, tt.reader); got != tt.want {
				t.Fatalf("versionText(%q, %q) = %q, want %q", tt.linked, tt.commit, got, tt.want)
			}
		})
	}
}
