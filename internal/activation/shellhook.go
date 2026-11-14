package activation

import (
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

// claudeEnvFile is the file that CLAUDE_ENV_FILE names. Claude Code sources
// it before each command, after the snapshot of the developer's functions
// that it takes once per session, so it is the one hook that runs late enough.
const claudeEnvFile = "claude-env.sh"

// zshFiles are the zsh start files that the ZDOTDIR wrapper replaces.
var zshFiles = []string{".zshenv", ".zprofile", ".zshrc", ".zlogin"}

// bashHookFile is the file that BASH_ENV names.
const bashHookFile = "bash-env.sh"

// ShellHookFiles returns the content of every file of the shell hook, by file
// name: one wrapper for each zsh start file, the BASH_ENV script, and the CLAUDE_ENV_FILE script. Each
// file runs the developer's own file first and then removes the functions and
// aliases named in names. names must be plain command names.
func ShellHookFiles(names []string) map[string]string {
	list := strings.Join(names, " ")
	clean := "unset -f " + list + " 2>/dev/null\nunalias " + list + " 2>/dev/null\n"
	files := map[string]string{
		bashHookFile: "# agent-id shell hook (#135): bash reads this file through BASH_ENV.\n" +
			"if [ -n \"${" + VarUserBashEnv + ":-}\" ] && [ -r \"$" + VarUserBashEnv + "\" ]; then\n" +
			"  . \"$" + VarUserBashEnv + "\"\n" +
			"fi\n" + clean + "true\n",
	}
	files[claudeEnvFile] = "# agent-id shell hook (#135): Claude Code sources this file before each command.\n" +
		"if [ -n \"${" + VarUserClaudeEnvFile + ":-}\" ] && [ -r \"$" + VarUserClaudeEnvFile + "\" ]; then\n" +
		"  . \"$" + VarUserClaudeEnvFile + "\"\n" +
		"fi\n" + clean + "true\n"
	for _, f := range zshFiles {
		files[f] = zshWrapper(f, clean)
	}
	return files
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
// dir, and that keep the developer's own values. On re-entry (the parent
// already holds a context) it takes the recorded values, so the developer's
// settings survive nesting.
func shellHookSet(parent []string, dir string) map[string]string {
	env := parseEnv(parent)
	user := map[string]string{
		VarUserZDotDir: env["ZDOTDIR"], VarUserBashEnv: env["BASH_ENV"], VarUserClaudeEnvFile: env["CLAUDE_ENV_FILE"],
	}
	if env[VarContext] != "" {
		user = map[string]string{
			VarUserZDotDir: env[VarUserZDotDir], VarUserBashEnv: env[VarUserBashEnv], VarUserClaudeEnvFile: env[VarUserClaudeEnvFile],
		}
	}
	user["ZDOTDIR"] = dir
	user["BASH_ENV"] = dir + "/" + bashHookFile
	user["CLAUDE_ENV_FILE"] = dir + "/" + claudeEnvFile
	return user
}

// hookNames returns the command names that the hook cleans, sorted and
// without duplicates.
func hookNames(names ...string) []string {
	out := slices.Clone(names)
	slices.Sort(out)
	return slices.Compact(out)
}
