package doctor

import (
	"slices"
	"testing"
)

func TestRepositoriesOn(t *testing.T) {
	tests := []struct {
		name string
		urls []string
		want []string
	}{
		{"https", []string{"https://github.com/o/r.git"}, []string{"o/r"}},
		{"https without suffix", []string{"https://github.com/o/r"}, []string{"o/r"}},
		{"https with credentials", []string{"https://user:secret@github.com/o/r"}, []string{"o/r"}},
		{"ssh url", []string{"ssh://git@github.com/o/r.git"}, []string{"o/r"}},
		{"scp-like", []string{"git@github.com:o/r.git"}, []string{"o/r"}},
		{"host case", []string{"https://GitHub.com/o/r"}, []string{"o/r"}},
		{"other host", []string{"https://gitlab.com/o/r", "git@gitlab.com:o/r"}, nil},
		{"host in the path", []string{"https://evil.test/github.com/o/r"}, nil},
		{"user named like the host", []string{"https://github.com@evil.test/o/r"}, nil},
		{"too many segments", []string{"https://github.com/o/r/x"}, nil},
		{"too few segments", []string{"https://github.com/o"}, nil},
		{"local path", []string{"/srv/repo.git", "../x"}, nil},
		{"duplicates", []string{"https://github.com/o/r", "git@github.com:o/r.git", "https://github.com/O/R"}, []string{"o/r"}},
		{"garbage", []string{"", "://", "%zz"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := repositoriesOn(tt.urls, "github.com"); !slices.Equal(got, tt.want) {
				t.Errorf("repositoriesOn(%q) = %v, want %v", tt.urls, got, tt.want)
			}
		})
	}
}
