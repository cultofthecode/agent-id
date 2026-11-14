package diagnose

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxStartFile bounds how much of one start file the scan reads.
const maxStartFile = 1 << 20

// ShellName is a command name that a start file defines as a function or
// alias.
type ShellName struct {
	Name string
	// Kind is "function" or "alias".
	Kind string
	// File is the start file that holds the definition.
	File string
}

// ShellState is the result of the static scan of the developer's shell start
// files (#135). Scanned is false when nobody looked.
type ShellState struct {
	Scanned bool
	Found   []ShellName
}

// ShellScan looks for definitions in the start files of the current
// environment.
type ShellScan func() ShellState

// ScanShellFiles returns a ShellScan over the start files that environ
// implies. It only reads. It starts no shell, so it cannot run any code of the
// developer, and it follows no source or include line: a definition in a file
// that a start file sources is not seen. Inside an agent context it reads the
// developer's own values that exec saved, never agent-id's wrappers.
func ScanShellFiles(environ, names []string) ShellScan {
	return func() ShellState {
		var found []ShellName
		seen := map[ShellName]bool{}
		for _, file := range startFiles(environ) {
			for _, f := range scanFile(file, names) {
				if !seen[f] {
					seen[f] = true
					found = append(found, f)
				}
			}
		}
		return ShellState{Scanned: true, Found: found}
	}
}

// startFiles lists the start files of zsh and bash: the user's own ZDOTDIR
// and BASH_ENV, as saved by exec when present.
func startFiles(environ []string) []string {
	env := map[string]string{}
	for _, e := range environ {
		if k, v, ok := strings.Cut(e, "="); ok {
			env[k] = v
		}
	}
	home := env["HOME"]
	zdot, bashEnv := env["ZDOTDIR"], env["BASH_ENV"]
	if env["AGENT_ID_CONTEXT"] != "" {
		zdot, bashEnv = env["AGENT_ID_USER_ZDOTDIR"], env["AGENT_ID_USER_BASH_ENV"]
	}
	if zdot == "" {
		zdot = home
	}
	var files []string
	if filepath.IsAbs(zdot) {
		for _, n := range []string{".zshenv", ".zprofile", ".zshrc", ".zlogin"} {
			files = append(files, filepath.Join(zdot, n))
		}
	}
	if filepath.IsAbs(home) {
		for _, n := range []string{".bashrc", ".bash_profile", ".bash_login", ".profile"} {
			files = append(files, filepath.Join(home, n))
		}
	}
	if filepath.IsAbs(bashEnv) {
		files = append(files, bashEnv)
	}
	return files
}

func definitionPatterns(names []string) (fn, alias *regexp.Regexp) {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	any := "(" + strings.Join(quoted, "|") + ")"
	start := `(?:^|;|&&|\|\|)\s*`
	fn = regexp.MustCompile(start + `(?:function\s+` + any + `(?:\s|\(|\{|;|$)|` + any + `\s*\(\s*\))`)
	alias = regexp.MustCompile(start + `alias\s+(?:-[A-Za-z]+\s+)*` + any + `=`)
	return fn, alias
}

func scanFile(path string, names []string) []ShellName {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxStartFile))
	if err != nil {
		return nil
	}
	fn, alias := definitionPatterns(names)
	var found []ShellName
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		for _, m := range fn.FindAllStringSubmatch(trimmed, -1) {
			found = append(found, ShellName{Name: firstNonEmpty(m[1:]), Kind: "function", File: path})
		}
		for _, m := range alias.FindAllStringSubmatch(trimmed, -1) {
			found = append(found, ShellName{Name: m[1], Kind: "alias", File: path})
		}
	}
	return found
}

func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// ShellWarnings returns one line for each definition of s, for whoami.
func ShellWarnings(s ShellState) []string {
	var out []string
	for _, f := range s.Found {
		out = append(out, shellWarning(f))
	}
	return out
}

func shellWarning(f ShellName) string {
	return "a start file defines a shell " + f.Kind + " named " + f.Name + " (" + f.File + ")"
}
