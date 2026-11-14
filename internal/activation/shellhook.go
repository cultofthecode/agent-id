package activation

import (
	"path/filepath"
	"slices"
	"strings"
)

// Variables that carry the developer's own shell start settings through the
// shell hook (#135). Both are agent-id owned, and a plan may set them.
const (
	VarUserZDotDir = "AGENT_ID_USER_ZDOTDIR"
	VarUserBashEnv = "AGENT_ID_USER_BASH_ENV"
	// VarUserClaudeEnvFile holds the CLAUDE_ENV_FILE that the developer
	// had before exec replaced it.
	VarUserClaudeEnvFile = "AGENT_ID_USER_CLAUDE_ENV_FILE"
)

// claudeEnvFile is the name of the file that CLAUDE_ENV_FILE names. Claude
// Code sources it before each command, after the snapshot of the developer's
// functions that it takes once per session, so it is the one hook that runs
// late enough. Each exec has its own copy in sessions/<pid>.<random>/, because
// a process that follows the documented ">> $CLAUDE_ENV_FILE" pattern appends
// to it and nothing removes the lines when the session ends.
const claudeEnvFile = "claude-env.sh"

// sessionsDir is the directory in the shell hook directory that holds one
// directory for each exec.
const sessionsDir = "sessions"

// zshFiles are the zsh start files that the ZDOTDIR wrapper replaces.
var zshFiles = []string{".zshenv", ".zprofile", ".zshrc", ".zlogin"}

// bashHookFile is the file that BASH_ENV names.
const bashHookFile = "bash-env.sh"

// ShellHookFiles returns the content of every file of the shell hook, by file
// name: one wrapper for each zsh start file and the BASH_ENV script. Each
// file runs the developer's own file first and then removes the functions and
// aliases named in names. names must be plain command names. The
// CLAUDE_ENV_FILE script is not shared: see ClaudeEnvScript.
func ShellHookFiles(names []string) map[string]string {
	list := strings.Join(names, " ")
	clean := "unset -f " + list + " 2>/dev/null\nunalias " + list + " 2>/dev/null\n"
	files := map[string]string{
		bashHookFile: "# agent-id shell hook (#135): bash reads this file through BASH_ENV.\n" +
			"if [ -n \"${" + VarUserBashEnv + ":-}\" ] && [ -r \"$" + VarUserBashEnv + "\" ]; then\n" +
			"  . \"$" + VarUserBashEnv + "\"\n" +
			"fi\n" + clean + "true\n",
	}
	for _, f := range zshFiles {
		files[f] = zshWrapper(f, clean)
	}
	return files
}

// ClaudeEnvScript returns the content of the CLAUDE_ENV_FILE script of one
// exec. It sources the file named user first (the developer's own file, or the
// file of the outer exec in a nested exec) and then removes the functions and
// aliases named in names. The path is written into the script as a quoted
// literal, not read from a variable, so a nested exec cannot make a file source
// itself. Lines that a process appends after these lines can define the
// functions again, in this session only.
func ClaudeEnvScript(user string, names []string) string {
	list := strings.Join(names, " ")
	out := "# agent-id shell hook (#135): Claude Code sources this file before each command.\n"
	if user != "" {
		q := shellQuote(user)
		out += "if [ -r " + q + " ]; then\n  . " + q + "\nfi\n"
	}
	return out + "unset -f " + list + " 2>/dev/null\nunalias " + list + " 2>/dev/null\ntrue\n"
}

// userClaudeEnvFile returns the file that the CLAUDE_ENV_FILE script of a new
// exec sources. In a fresh start it is the developer's CLAUDE_ENV_FILE. In a
// nested exec it is the outer exec's own session file, so the outer session's
// appended lines still run, and the outer file sources the developer's file. A
// CLAUDE_ENV_FILE outside the sessions directory is not trusted as an outer
// file: the recorded developer value is used instead.
func userClaudeEnvFile(parent []string, shellDir string) string {
	env := parseEnv(parent)
	if env[VarContext] == "" {
		return env["CLAUDE_ENV_FILE"]
	}
	outer := filepath.Clean(env["CLAUDE_ENV_FILE"])
	if strings.HasPrefix(outer, filepath.Join(shellDir, sessionsDir)+string(filepath.Separator)) {
		return outer
	}
	return env[VarUserClaudeEnvFile]
}

// pathFirst puts the directories of the plan first on PATH again. A login
// shell can reorder PATH (macOS runs path_helper in /etc/zprofile) or a start
// file of the developer can, and then a real gh in a later directory would win
// over the launcher.
const pathFirst = "__aid_dirs=(${(s/:/)" + VarPathPrepend + "})\n" +
	"path=($__aid_dirs ${path:|__aid_dirs})\n" +
	"unset __aid_dirs\n"

func zshWrapper(file, clean string) string {
	history := ""
	if file == ".zshrc" {
		history = "case \"$HISTFILE\" in \"$__aid_wrapper\"/*) HISTFILE=\"$__aid_user/${HISTFILE##*/}\" ;; esac\n"
	}
	return "# agent-id shell hook (#135): zsh reads this file through ZDOTDIR.\n" +
		"__aid_wrapper=\"$ZDOTDIR\"\n" +
		"__aid_user=\"${" + VarUserZDotDir + ":-$HOME}\"\n" +
		"if [ -r \"$__aid_user/" + file + "\" ]; then\n" +
		"  ZDOTDIR=\"$__aid_user\"\n" +
		"  . \"$__aid_user/" + file + "\"\n" +
		"  __aid_user=\"${ZDOTDIR:-$HOME}\"\n" +
		"fi\n" +
		"ZDOTDIR=\"$__aid_wrapper\"\n" +
		VarUserZDotDir + "=\"$__aid_user\"\n" +
		history +
		"unset __aid_wrapper __aid_user\n" +
		pathFirst +
		clean
}

// shellHookSet returns the variables that point bash and zsh at the hook in
// dir, and Claude Code at sessionFile, and that keep the developer's own
// values. On re-entry (the parent already holds a context) the ZDOTDIR and
// BASH_ENV values come from the record, so the developer's settings survive
// nesting. An empty sessionFile leaves CLAUDE_ENV_FILE alone.
func shellHookSet(parent []string, dir, sessionFile string) map[string]string {
	env := parseEnv(parent)
	user := map[string]string{
		VarUserZDotDir: env["ZDOTDIR"], VarUserBashEnv: env["BASH_ENV"],
		VarUserClaudeEnvFile: userClaudeEnvFile(parent, dir),
	}
	if env[VarContext] != "" {
		user[VarUserZDotDir], user[VarUserBashEnv] = env[VarUserZDotDir], env[VarUserBashEnv]
	}
	user["ZDOTDIR"] = dir
	user["BASH_ENV"] = dir + "/" + bashHookFile
	if sessionFile != "" {
		user["CLAUDE_ENV_FILE"] = sessionFile
	}
	return user
}

// hookNames returns the command names that the hook cleans, sorted and
// without duplicates.
func hookNames(names ...string) []string {
	out := slices.Clone(names)
	slices.Sort(out)
	return slices.Compact(out)
}
