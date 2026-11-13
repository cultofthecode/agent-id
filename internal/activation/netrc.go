package activation

import (
	"errors"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// NetrcHosts returns the machines that a netrc file names, and whether it has
// a default entry. A keyword that it does not know is an error. It never
// returns a login or a password.
func NetrcHosts(content string) (machines []string, hasDefault bool, err error) {
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		tokens := strings.Fields(lines[i])
		for j := 0; j < len(tokens); j++ {
			switch tokens[j] {
			case "machine":
				if j+1 >= len(tokens) {
					return nil, false, errors.New("machine without a host")
				}
				machines = append(machines, strings.Trim(tokens[j+1], `"`))
				j++
			case "default":
				hasDefault = true
			case "login", "password", "account", "port":
				j++
			case "macdef":
				for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
					i++
				}
				j = len(tokens)
			default:
				if strings.HasPrefix(tokens[j], "#") {
					j = len(tokens)
					continue
				}
				return nil, false, errors.New("unknown keyword")
			}
		}
	}
	return machines, hasDefault, nil
}

// netrcFiles returns the netrc files that Git's HTTP transport could read for
// the child environment: $HOME/.netrc, $HOME/_netrc, and $NETRC.
func netrcFiles(child []string) []string {
	env := parseEnv(child)
	home := env["HOME"]
	if home == "" {
		if u, err := user.Current(); err == nil {
			home = u.HomeDir
		}
	}
	var files []string
	if home != "" {
		files = append(files, filepath.Join(home, ".netrc"), filepath.Join(home, "_netrc"))
	}
	if n := env["NETRC"]; n != "" {
		files = append(files, n)
	}
	return files
}

// checkNetrc refuses a netrc file that has a machine entry for one of hosts
// or a default entry.
func checkNetrc(child []string, hosts []string) error {
	for _, path := range netrcFiles(child) {
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return failf(ErrNetrc, "%s cannot be read; move it aside while the agent runs", path)
		}
		machines, hasDefault, err := NetrcHosts(string(data))
		if err != nil {
			return failf(ErrNetrc, "%s cannot be parsed; move it aside while the agent runs", path)
		}
		if hasDefault {
			return failf(ErrNetrc, "%s has a default entry, which Git would send for the agent; move it aside while the agent runs", path)
		}
		for _, m := range machines {
			for _, h := range hosts {
				if strings.EqualFold(m, h) {
					return failf(ErrNetrc, "%s has credentials for %s, which Git would send for the agent; move it aside while the agent runs", path, h)
				}
			}
		}
	}
	return nil
}
