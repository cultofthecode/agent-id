#!/bin/sh
# Reproduces every row of 2026-11-12-git-precedence-matrix.md.
#
# Usage, from the repository root:  sh .ai/analysis/2026-11-12-git-precedence-matrix.sh
#
# The script builds a throwaway isolated environment under $TMPDIR/agent-id/
# (HOME, XDG directories, GIT_CONFIG_GLOBAL, GIT_CONFIG_NOSYSTEM=1, GH_CONFIG_DIR,
# TMPDIR), removes inherited tokens, identity and Git configuration variables,
# and deletes everything it created on exit. It never reads or writes the real
# ~/.gitconfig, ~/.config/git, ~/.config/gh, the ghapp configuration, or a
# keychain: credential helpers, the SSH client, the signer and askpass are fake
# scripts that only log their invocation, "osxkeychain" is simulated by a fake
# helper and never invoked, and every token is fake (prefix ghs_aidtest_).
# The only network use is a header-capture listener that the script builds
# with Go and binds on 127.0.0.1 with a port in 18000-18999.
#
# Optional: AIDTEST_GH_STORE=1 also runs row gh2, where the real gh reads a
# token from an isolated hosts.yml without GH_TOKEN. Use it only where gh has
# no OS keyring (the Linux container of the analysis): without GH_TOKEN, gh
# may consult the keyring, and on macOS that is the real login keychain.
# AIDTEST_CAPTURE_BIN=<path> uses a prebuilt capture server instead of Go.

set -u

tmp_base=${TMPDIR:-/tmp}
base=${tmp_base%/}/agent-id
mkdir -p "$base"
root=$(mktemp -d "$base/git-matrix.XXXXXX")
server_pid=""
srv=""
cleanup() {
	if [ -n "$server_pid" ]; then kill "$server_pid" 2>/dev/null; fi
	rm -rf "$root"
}
trap cleanup EXIT
trap 'exit 130' INT TERM


isolate() {
	for v in GH_TOKEN GITHUB_TOKEN GH_ENTERPRISE_TOKEN GITHUB_ENTERPRISE_TOKEN GH_HOST GH_REPO \
		GIT_ASKPASS SSH_ASKPASS SSH_AUTH_SOCK GIT_SSH GIT_SSH_COMMAND \
		GIT_CONFIG GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS GIT_CONFIG_SYSTEM \
		GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_AUTHOR_DATE GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL GIT_COMMITTER_DATE \
		GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_TRACE GIT_TRACE_CURL GIT_CURL_VERBOSE \
		BASH_ENV ENV ZDOTDIR CLAUDE_ENV_FILE; do
		unset "$v"
	done
	for v in $(env | awk -F= '/^(AIDENTITY_|GHAPP_|GIT_CONFIG_KEY_|GIT_CONFIG_VALUE_)/ {print $1}'); do
		unset "$v"
	done
	export HOME="$root/home"
	export XDG_CONFIG_HOME="$HOME/.config" XDG_CACHE_HOME="$HOME/.cache"
	export XDG_STATE_HOME="$HOME/.local/state" XDG_DATA_HOME="$HOME/.local/share"
	export GIT_CONFIG_GLOBAL="$HOME/.gitconfig" GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0
	export GCM_INTERACTIVE=never GH_CONFIG_DIR="$XDG_CONFIG_HOME/gh" GH_PROMPT_DISABLED=1 GH_NO_UPDATE_NOTIFIER=1
	export TMPDIR="$root/tmp"
	export AIDTEST_LOG="$root/calls.log" SYSCFG="$root/system.gitconfig"
	mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$XDG_STATE_HOME" "$XDG_DATA_HOME" "$GH_CONFIG_DIR" "$TMPDIR" "$root/bin"
	: >"$GIT_CONFIG_GLOBAL"
	: >"$SYSCFG"
	: >"$AIDTEST_LOG"
}

write_fakes() {
	cat >"$root/bin/helper" <<'EOF'
#!/bin/sh
name=$1 mode=$2 op=$3
attrs=$(grep -v '^password=' | tr '\n' ' ')
printf '%s %s %s\n' "$name" "$op" "$attrs" >>"$AIDTEST_LOG"
[ "$op" = get ] || exit 0
case $mode in
answer) printf 'username=%s\npassword=ghs_aidtest_%s\n' "$name" "$name" ;;
quit) printf 'quit=1\n' ;;
esac
EOF
	cat >"$root/bin/logcall" <<'EOF'
#!/bin/sh
name=$1
shift
printf '%s %s\n' "$name" "$*" >>"$AIDTEST_LOG"
case $name in
askpass*) printf '%s\n' "$name" ;;
*) exit 1 ;;
esac
EOF
	for n in askpass-env askpass-config askpass-ssh signer ssh-personal; do
		printf '#!/bin/sh\nexec "%s" %s "$@"\n' "$root/bin/logcall" "$n" >"$root/bin/$n"
	done
	chmod +x "$root/bin/"*
}

build_capture_server() {
	if [ -n "${AIDTEST_CAPTURE_BIN:-}" ]; then
		capture_bin=$AIDTEST_CAPTURE_BIN
		return
	fi
	mkdir -p "$root/capture"
	cat >"$root/capture/main.go" <<'EOF'
// Command capture answers every request with 401 and logs the Authorization
// headers it received, decoded to the user name or the fake bearer token.
package main

import (
	"encoding/base64"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

func main() {
	logPath, portPath := os.Args[1], os.Args[2]
	var l net.Listener
	for i := 0; i < 1000 && l == nil; i++ {
		port := 18000 + rand.IntN(1000)
		l, _ = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	}
	if l == nil {
		os.Exit(1)
	}
	var mu sync.Mutex
	handler := func(w http.ResponseWriter, r *http.Request) {
		var seen []string
		for _, v := range r.Header.Values("Authorization") {
			seen = append(seen, describe(v))
		}
		mu.Lock()
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(f, "server %s auth=[%s]\n", r.URL.Path, strings.Join(seen, ","))
			_ = f.Close()
		}
		mu.Unlock()
		w.Header().Set("WWW-Authenticate", `Basic realm="aidtest"`)
		w.WriteHeader(http.StatusUnauthorized)
	}
	_ = os.WriteFile(portPath, []byte(fmt.Sprint(l.Addr().(*net.TCPAddr).Port)), 0o600)
	_ = http.Serve(l, http.HandlerFunc(handler))
}

func describe(v string) string {
	scheme, value, _ := strings.Cut(v, " ")
	if strings.EqualFold(scheme, "basic") {
		raw, err := base64.StdEncoding.DecodeString(value)
		if err == nil {
			user, _, _ := strings.Cut(string(raw), ":")
			return "basic:" + user
		}
	}
	return strings.ToLower(scheme) + ":" + value
}
EOF
	printf 'module capture\n\ngo 1.22\n' >"$root/capture/go.mod"
	capture_bin="$root/bin/capture"
	(cd "$root/capture" && GOCACHE="$root/gocache" GOFLAGS=-mod=mod GOTOOLCHAIN=local go build -o "$capture_bin" .) || {
		echo "cannot build the capture server; rows h1-h6 need Go or AIDTEST_CAPTURE_BIN" >&2
		capture_bin=""
	}
}

start_capture_server() {
	[ -n "$capture_bin" ] || return 1
	"$capture_bin" "$AIDTEST_LOG" "$root/port" &
	server_pid=$!
	i=0
	while [ ! -s "$root/port" ] && [ $i -lt 100 ]; do
		sleep 0.1
		i=$((i + 1))
	done
	port=$(cat "$root/port")
	srv="http://127.0.0.1:$port"
}

row() { printf '%-5s %-64s %s\n' "$1" "$2" "$3"; }

helper() { printf "!'%s' %s %s" "$root/bin/helper" "$1" "${2:-answer}"; }

param_helper() { printf '!%s %s answer' "$root/bin/helper" "$1"; }

reset_case() {
	: >"$GIT_CONFIG_GLOBAL"
	: >"$SYSCFG"
	: >"$AIDTEST_LOG"
	rm -f "$HOME/.gitconfig-inc"
}

newrepo() {
	d=$(mktemp -d "$root/repo.XXXXXX")
	git init -q -b main "$d"
	cd "$d" || exit 1
	git remote add origin "${1:-https://github.com/o/r.git}"
}

gset() { git config --file "$GIT_CONFIG_GLOBAL" "$@"; }
sysset() { git config --file "$SYSCFG" "$@"; }
use_system() { export GIT_CONFIG_NOSYSTEM=0 GIT_CONFIG_SYSTEM="$SYSCFG"; }

cfg_append() {
	n=${GIT_CONFIG_COUNT:-0}
	while [ $# -ge 2 ]; do
		export "GIT_CONFIG_KEY_$n=$1" "GIT_CONFIG_VALUE_$n=$2"
		n=$((n + 1))
		shift 2
	done
	export GIT_CONFIG_COUNT=$n
}

aid_generic() { cfg_append credential.helper "" credential.helper "$(helper aid "${1:-answer}")"; }
aid_scoped() { cfg_append credential.https://github.com.helper "" credential.https://github.com.helper "$(helper aid "${1:-answer}")"; }
aid_both() {
	cfg_append credential.helper "" credential.https://github.com.helper "" \
		credential.https://github.com.helper "$(helper aid "${1:-answer}")"
}
aid_identity() {
	export GIT_AUTHOR_NAME='aidtest-bot[bot]' GIT_AUTHOR_EMAIL='1+aidtest-bot[bot]@users.noreply.github.com'
	export GIT_COMMITTER_NAME='aidtest-bot[bot]' GIT_COMMITTER_EMAIL='1+aidtest-bot[bot]@users.noreply.github.com'
}

calls() {
	awk -v op="$1" '$2 == op || op == "" {print $1}' "$AIDTEST_LOG" | tr '\n' ',' | sed 's/,$//'
}

fill() { fillwith git "$@"; }

fillwith() {
	: >"$AIDTEST_LOG"
	out=$(printf 'protocol=https\nhost=github.com\npath=%s\n\n' "${2:-o/r.git}" | "$1" credential fill 2>/dev/null)
	user=$(printf '%s\n' "$out" | sed -n 's/^username=//p')
	pass=$(printf '%s\n' "$out" | sed -n 's/^password=//p')
	case $pass in ghs_aidtest_*) ;; '') pass=none ;; *) pass='<redacted>' ;; esac
	echo "answer=${user:-none} password=$pass called=[$(calls get)]"
}

urls() { echo "$(git ls-remote --get-url origin) | $(git remote get-url --push origin)"; }

who() { git log -1 --format='author=%an <%ae> committer=%cn <%ce>'; }

signed() { if grep -q '^signer' "$AIDTEST_LOG"; then echo "signer invoked"; else echo "signer not invoked"; fi; }

probe() {
	: >"$AIDTEST_LOG"
	git ls-remote origin >/dev/null 2>&1
	echo "requests: $(awk '$1 == "server" {print $3}' "$AIDTEST_LOG" | tr '\n' ' ')helpers=[$(calls get)]"
}

section() { printf '\n## %s\n\n' "$1"; }

precedence() {
	section "P: precedence of configuration sources (lowest first)"
	(
		reset_case
		use_system
		newrepo
		sysset aidtest.k system
		gset aidtest.k global
		gset "includeIf.gitdir:$(pwd -P)/.path" "$HOME/.gitconfig-inc"
		git config --file "$HOME/.gitconfig-inc" aidtest.k include-gitdir
		git config aidtest.k local
		cfg_append aidtest.k count
		export GIT_CONFIG_PARAMETERS="'aidtest.k'='parameters'"
		order=$(git -c aidtest.k=dash-c config --show-scope --get-all aidtest.k | awk '{print $1 ":" $2}' | tr '\n' ' ')
		row P1 "system, global, includeIf, local, GIT_CONFIG_COUNT, PARAMETERS, -c" "order: $order"
		row P2 "single-valued read (last one wins)" "git config --get: $(git -c aidtest.k=dash-c config --get aidtest.k)"
		unset GIT_CONFIG_PARAMETERS
		row P3 "GIT_CONFIG_COUNT vs local without PARAMETERS" "git config --get: $(git config --get aidtest.k)"
	)
}

credential_helpers() {
	section "C: credential helper lists (git credential fill, host github.com)"
	(
		reset_case
		use_system
		newrepo
		sysset credential.helper "$(helper osxkeychain-sim)"
		row C1 "system helper (Apple Git: osxkeychain), no aidentity" "$(fill)"
		(aid_generic && row C2 "system helper + aid generic reset" "$(fill)")
		(aid_scoped && row C3 "system helper + aid github.com-scoped reset" "$(fill)")
	)
	(
		reset_case
		newrepo
		gset credential.helper "$(helper personal-global)"
		git config credential.helper "$(helper personal-repo)"
		row C4 "global + repo-local generic helpers, no aidentity" "$(fill)"
		(aid_generic && row C5 "global + repo-local generic helpers + aid generic reset" "$(fill)")
		(aid_scoped && row C6 "global + repo-local generic helpers + aid scoped reset" "$(fill)")
	)
	(
		reset_case
		newrepo
		gset credential.https://github.com.helper "$(helper gh-setup-git)"
		row C7 "global credential.https://github.com.helper (gh auth setup-git)" "$(fill)"
		(aid_generic && row C8 "  + aid generic reset" "$(fill)")
		(aid_scoped && row C9 "  + aid scoped reset" "$(fill)")
	)
	(
		reset_case
		newrepo
		gset credential.https://github.com/myorg.helper "$(helper personal-path)"
		row C10 "global path-scoped helper github.com/myorg, request myorg/r" "$(fill myorg/r.git)"
		(aid_scoped && row C11 "  + aid scoped reset" "$(fill myorg/r.git)")
		(aid_both && cfg_append credential.https://github.com.useHttpPath true &&
			row C12 "  + aid both resets + useHttpPath=true" "$(fill myorg/r.git)")
	)
	(
		reset_case
		newrepo
		gset "includeIf.gitdir:$(pwd -P)/.path" "$HOME/.gitconfig-inc"
		git config --file "$HOME/.gitconfig-inc" credential.https://github.com.helper "$(helper personal-include)"
		row C13 "includeIf gitdir: adds a scoped helper, no aidentity" "$(fill)"
		(aid_both && row C14 "  + aid both resets" "$(fill)")
	)
	(
		reset_case
		newrepo
		gset "includeIf.hasconfig:remote.*.url:https://github.com/**.path" "$HOME/.gitconfig-inc"
		git config --file "$HOME/.gitconfig-inc" credential.helper "$(helper personal-hasconfig)"
		row C15 "includeIf hasconfig:remote.*.url adds a helper, no aidentity" "$(fill)"
		(aid_both && row C16 "  + aid both resets" "$(fill)")
	)
	(
		reset_case
		newrepo
		cfg_append credential.https://github.com.helper "$(helper inherited-count)"
		row C17 "inherited GIT_CONFIG_COUNT helper, no aidentity" "$(fill)"
		aid_both
		row C18 "  + aid both resets appended after it" "$(fill)"
	)
	(
		reset_case
		newrepo
		aid_both
		export GIT_CONFIG_PARAMETERS="'credential.https://github.com.helper'='$(param_helper inherited-parameters)'"
		row C19 "inherited GIT_CONFIG_PARAMETERS adds a helper + aid both resets" "$(fill)"
		export GIT_CONFIG_PARAMETERS="'credential.helper'='' 'credential.https://github.com.helper'='' 'credential.https://github.com.helper'='$(param_helper inherited-parameters)'"
		row C19b "inherited GIT_CONFIG_PARAMETERS resets and adds + aid both resets" "$(fill)"
	)
	(
		reset_case
		newrepo
		aid_both
		agent_adds() { git -c credential.helper="$(helper agent-dash-c)" "$@"; }
		agent_resets() { git -c credential.https://github.com.helper= -c credential.https://github.com.helper="$(helper agent-dash-c)" "$@"; }
		row C20 "agent runs git -c credential.helper=X + aid both resets" "$(fillwith agent_adds)"
		row C20b "agent runs git -c <scoped helper>= -c <scoped helper>=X" "$(fillwith agent_resets)"
	)
	(
		reset_case
		newrepo
		aid_both
		: >"$AIDTEST_LOG"
		out=$(printf 'url=https://personal:ghs_aidtest_embedded@github.com/o/r.git\n\n' | git credential fill 2>/dev/null)
		row C21 "URL with embedded user:token + aid both resets" "answer=$(printf '%s\n' "$out" | sed -n 's/^username=//p') called=[$(calls get)]"
	)
	(
		reset_case
		newrepo
		gset credential.https://github.com.useHttpPath false
		aid_both
		cfg_append credential.useHttpPath true
		fill o/r.git >/dev/null
		row C22 "global scoped useHttpPath=false + aid generic useHttpPath=true" "aid saw: $(awk '$1 == "aid" && $2 == "get" {for (i = 3; i <= NF; i++) printf "%s ", $i}' "$AIDTEST_LOG")"
	)
	(
		reset_case
		newrepo
		aid_both
		fill o/r.git >/dev/null
		row C24 "aid both resets, no useHttpPath (control)" "aid saw: $(awk '$1 == "aid" && $2 == "get" {for (i = 3; i <= NF; i++) printf "%s ", $i}' "$AIDTEST_LOG")"
		cfg_append credential.useHttpPath true credential.https://github.com.useHttpPath false
		fill o/r.git >/dev/null
		row C25 "aid generic useHttpPath=true then scoped false (order check)" "aid saw: $(awk '$1 == "aid" && $2 == "get" {for (i = 3; i <= NF; i++) printf "%s ", $i}' "$AIDTEST_LOG")"
	)
	(
		reset_case
		newrepo
		aid_both
		cfg_append credential.https://github.com.useHttpPath true
		fill o/r.git >/dev/null
		row C23 "aid scoped useHttpPath=true" "aid saw: $(awk '$1 == "aid" && $2 == "get" {for (i = 3; i <= NF; i++) printf "%s ", $i}' "$AIDTEST_LOG")"
	)
}

askpass() {
	section "A: fallback after the helper list (askpass and prompts)"
	(
		reset_case
		newrepo
		aid_both empty
		export GIT_ASKPASS="$root/bin/askpass-env"
		row A1 "aid helper answers nothing, GIT_ASKPASS set (e.g. an IDE)" "$(fill)"
		unset GIT_ASKPASS
		gset core.askPass "$root/bin/askpass-config"
		row A2 "aid helper answers nothing, core.askPass in global config" "$(fill)"
		cfg_append core.askPass ""
		row A3 "  + aid core.askPass= (empty)" "$(fill)"
	)
	(
		reset_case
		newrepo
		aid_both empty
		export SSH_ASKPASS="$root/bin/askpass-ssh"
		row A4 "aid helper answers nothing, SSH_ASKPASS set" "$(fill)"
	)
	(
		reset_case
		newrepo
		aid_both quit
		export GIT_ASKPASS="$root/bin/askpass-env"
		gset credential.helper "$(helper personal-global)"
		row A5 "aid helper answers quit=1, GIT_ASKPASS set" "$(fill)"
	)
	(
		reset_case
		newrepo
		aid_both empty
		cfg_append credential.interactive false
		export GIT_ASKPASS="$root/bin/askpass-env"
		row A6 "aid helper answers nothing + credential.interactive=false" "$(fill)"
	)
}

rewrites() {
	section "U: remote URLs and rewrites (fetch URL | push URL)"
	aid_rewrite() {
		cfg_append url.https://github.com/.insteadOf git@github.com: \
			url.https://github.com/.insteadOf ssh://git@github.com/
	}
	for remote in git@github.com:o/r.git ssh://git@github.com/o/r.git github.com:o/r.git \
		ssh://github.com/o/r.git ssh://git@ssh.github.com:443/o/r.git git+ssh://git@github.com/o/r.git; do
		(
			reset_case
			newrepo "$remote"
			aid_rewrite
			row U1 "remote $remote + aid ssh->https rewrites" "$(urls)"
		)
	done
	(
		reset_case
		newrepo https://github.com/o/r.git
		gset url.git@github.com:.insteadOf https://github.com/
		row U2 "personal https->ssh insteadOf, https remote, no aidentity" "$(urls)"
		aid_rewrite
		row U3 "  + aid ssh->https rewrites" "$(urls)"
		cfg_append url.https://github.com/.insteadOf https://github.com/
		row U4 "  + aid identity rewrite https://github.com/ -> itself" "$(urls)"
	)
	(
		reset_case
		newrepo https://github.com/myorg/r.git
		gset url.git@github.com:myorg/.insteadOf https://github.com/myorg/
		aid_rewrite
		cfg_append url.https://github.com/.insteadOf https://github.com/
		row U5 "personal longer-prefix https->ssh rule + aid rewrites" "$(urls)"
	)
	(
		reset_case
		newrepo https://github.com/o/r.git
		gset url.git@github.com:.pushInsteadOf https://github.com/
		row U6 "personal pushInsteadOf https->ssh, no aidentity" "$(urls)"
		aid_rewrite
		row U7 "  + aid ssh->https insteadOf rewrites" "$(urls)"
		cfg_append url.https://github.com/.pushInsteadOf https://github.com/
		row U8 "  + aid pushInsteadOf https://github.com/ -> itself" "$(urls)"
	)
	(
		reset_case
		newrepo https://github.com/o/r.git
		git config remote.origin.pushurl git@github.com:o/r.git
		row U9 "repo remote.origin.pushurl=git@github.com:, no aidentity" "$(urls)"
		aid_rewrite
		row U10 "  + aid ssh->https insteadOf rewrites" "$(urls)"
	)
	(
		reset_case
		newrepo https://github.com/o/r.git
		git config remote.origin.pushurl https://personal:ghs_aidtest_embedded@github.com/o/r.git
		aid_rewrite
		row U11 "repo pushurl with embedded user:token + aid rewrites" "push URL has credentials: $(git remote get-url --push origin | grep -q '@github.com' && echo yes || echo no)"
	)
	for remote in github.com:o/r.git ssh://github.com/o/r.git ssh://git@ssh.github.com:443/o/r.git \
		git+ssh://git@github.com/o/r.git ssh+git://git@github.com/o/r.git git://github.com/o/r.git; do
		(
			reset_case
			newrepo "$remote"
			aid_rewrite
			cfg_append url.https://github.com/.insteadOf github.com: \
				url.https://github.com/.insteadOf ssh://github.com/ \
				url.https://github.com/.insteadOf ssh://git@ssh.github.com:443/ \
				url.https://github.com/.insteadOf git+ssh://git@github.com/ \
				url.https://github.com/.insteadOf ssh+git://git@github.com/ \
				url.https://github.com/.insteadOf git://github.com/
			row U13 "remote $remote + extended aid rewrite set" "$(urls)"
		)
	done
	(
		reset_case
		newrepo github-personal:o/r.git
		aid_rewrite
		row U12 "remote through an ~/.ssh/config Host alias + aid rewrites" "$(urls)"
	)
}

ssh_transport() {
	section "K: SSH transport (fake ssh; logs which command Git runs)"
	(
		reset_case
		newrepo github.com:o/r.git
		aid_identity
		git commit -q --allow-empty -m x
		gset core.sshCommand "$root/bin/ssh-personal"
		cfg_append url.https://github.com/.insteadOf git@github.com: url.https://github.com/.insteadOf ssh://git@github.com/
		git push -q origin HEAD 2>/dev/null
		row K1 "remote github.com:o/r (not rewritten), core.sshCommand personal" "ran: $(awk '{print $1, $2}' "$AIDTEST_LOG" | head -1)"
	)
	(
		reset_case
		newrepo git@github.com:o/r.git
		aid_identity
		git commit -q --allow-empty -m x
		gset core.sshCommand "$root/bin/ssh-personal"
		git push -q origin HEAD 2>/dev/null
		row K2 "remote git@github.com:o/r, core.sshCommand personal, no aidentity" "ran: $(awk '{print $1, $2}' "$AIDTEST_LOG" | head -1)"
	)
	(
		reset_case
		newrepo github.com:o/r.git
		aid_identity
		git commit -q --allow-empty -m x
		gset core.sshCommand "$root/bin/ssh-personal"
		cfg_append core.sshCommand false
		git push -q origin HEAD 2>/dev/null
		row K3 "  core.sshCommand personal + aid core.sshCommand=false" "ran: [$(awk '{print $1}' "$AIDTEST_LOG" | head -1)] exit=$(git push -q origin HEAD >/dev/null 2>&1; echo $?)"
		export GIT_SSH_COMMAND="$root/bin/ssh-personal"
		: >"$AIDTEST_LOG"
		git push -q origin HEAD 2>/dev/null
		row K4 "  + inherited GIT_SSH_COMMAND" "ran: $(awk '{print $1}' "$AIDTEST_LOG" | head -1)"
	)
}

headers() {
	section "H: HTTP headers on a real transport (capture server on 127.0.0.1)"
	if ! start_capture_server; then
		row H- "capture server unavailable" "skipped"
		return
	fi
	(
		reset_case
		newrepo "$srv/o/r.git"
		gset http.extraHeader "Authorization: Bearer ghs_aidtest_personal-global"
		row H1 "global http.extraHeader Authorization, no aidentity" "$(probe)"
		cfg_append http.extraHeader ""
		row H2 "  + aid http.extraHeader= (generic reset)" "$(probe)"
	)
	(
		reset_case
		newrepo "$srv/o/r.git"
		gset http.extraHeader "Authorization: Bearer ghs_aidtest_personal-global"
		cfg_append "http.$srv/.extraHeader" ""
		row H8 "global generic http.extraHeader + aid url-scoped reset only" "$(probe)"
	)
	(
		reset_case
		newrepo "$srv/o/r.git"
		git config "http.$srv/.extraheader" "AUTHORIZATION: basic $(printf 'x-access-token:ghs_aidtest_checkout' | base64)"
		row H3 "repo http.<url>.extraheader (actions/checkout), no aidentity" "$(probe)"
		cfg_append http.extraHeader ""
		row H4 "  + aid generic http.extraHeader= reset" "$(probe)"
		cfg_append "http.$srv/.extraHeader" ""
		row H5 "  + aid url-scoped http.<url>.extraHeader= reset" "$(probe)"
	)
	(
		reset_case
		newrepo "$srv/o/r.git"
		git config "http.$srv/o/r.git.extraheader" "Authorization: Bearer ghs_aidtest_path-scoped"
		cfg_append http.extraHeader "" "http.$srv/.extraHeader" ""
		row H7 "repo path-scoped http.<url>/o/r.git.extraheader + aid resets" "$(probe)"
	)
	(
		reset_case
		newrepo "$srv/o/r.git"
		gset credential.helper "$(helper personal-global)"
		cfg_append "credential.$srv.helper" "" "credential.$srv.helper" "$(helper aid)"
		row H6 "real transport: personal generic helper + aid url-scoped reset" "$(probe)"
	)
}

identity() {
	section "I: author and committer identity"
	(
		reset_case
		newrepo
		gset user.name Personal
		gset user.email personal@example.com
		aid_identity
		git commit -q --allow-empty -m x
		row I1 "global user.name/email + aid author/committer variables" "$(who)"
		gset author.email author-personal@example.com
		gset committer.email committer-personal@example.com
		git commit -q --allow-empty -m x
		row I2 "  + global author.email/committer.email" "$(who)"
		git -c user.email=dash-c@example.com commit -q --allow-empty -m x
		row I3 "  agent runs git -c user.email=..." "$(who)"
		git commit -q --allow-empty --author='Personal <personal@example.com>' -m x
		row I4 "  agent runs git commit --author=..." "$(who)"
	)
	(
		reset_case
		newrepo
		gset user.useConfigOnly true
		git commit -q --allow-empty -m x 2>/dev/null
		row I5 "user.useConfigOnly=true, no user.email, no aidentity" "commit exit=$?"
		aid_identity
		git commit -q --allow-empty -m x 2>/dev/null
		row I6 "user.useConfigOnly=true, no user.email + aid variables" "commit exit=$? $(who)"
	)
}

signing() {
	section "S: commit and tag signing (fake signer)"
	(
		reset_case
		newrepo
		aid_identity
		gset gpg.format ssh
		gset user.signingKey "$root/fake-key.pub"
		gset gpg.ssh.program "$root/bin/signer"
		gset commit.gpgSign true
		git commit -q --allow-empty -m x 2>/dev/null
		row S1 "global commit.gpgSign=true, gpg.format=ssh, no aidentity" "commit exit=$? $(signed)"
		cfg_append commit.gpgSign false
		: >"$AIDTEST_LOG"
		git commit -q --allow-empty -m x 2>/dev/null
		row S2 "  + aid commit.gpgSign=false" "commit exit=$? $(signed)"
		: >"$AIDTEST_LOG"
		git commit -q --allow-empty -S -m x 2>/dev/null
		row S3 "  agent runs git commit -S" "commit exit=$? $(signed)"
		git config commit.gpgSign true
		: >"$AIDTEST_LOG"
		git commit -q --allow-empty -m x 2>/dev/null
		row S4 "  repo-local commit.gpgSign=true + aid commit.gpgSign=false" "commit exit=$? $(signed)"
	)
	(
		reset_case
		newrepo
		aid_identity
		git commit -q --allow-empty -m x
		gset gpg.program "$root/bin/signer"
		gset tag.gpgSign true
		: >"$AIDTEST_LOG"
		git tag -a -m x v1 2>/dev/null
		row S5 "global tag.gpgSign=true (openpgp), no aidentity" "tag exit=$? $(signed)"
		cfg_append tag.gpgSign false
		: >"$AIDTEST_LOG"
		git tag -a -m x v2 2>/dev/null
		row S6 "  + aid tag.gpgSign=false" "tag exit=$? $(signed)"
	)
	(
		reset_case
		newrepo
		aid_identity
		git commit -q --allow-empty -m x
		gset gpg.program "$root/bin/signer"
		gset tag.forceSignAnnotated true
		cfg_append tag.gpgSign false
		: >"$AIDTEST_LOG"
		git tag -a -m x v1 2>/dev/null
		row S7 "global tag.forceSignAnnotated=true + aid tag.gpgSign=false" "tag exit=$? $(signed)"
		cfg_append tag.forceSignAnnotated false
		: >"$AIDTEST_LOG"
		git tag -a -m x v2 2>/dev/null
		row S8 "  + aid tag.forceSignAnnotated=false" "tag exit=$? $(signed)"
	)
}

config_count() {
	section "G: inherited GIT_CONFIG_COUNT and GIT_CONFIG_PARAMETERS"
	(
		reset_case
		newrepo
		cfg_append safe.directory /inherited aidtest.inherited yes
		aid_both
		row G1 "inherited COUNT=2 + aid appended (COUNT=$GIT_CONFIG_COUNT)" "aidtest.inherited=$(git config --get aidtest.inherited) helper=$(fill)"
	)
	(
		reset_case
		newrepo
		export GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=aidtest.k GIT_CONFIG_VALUE_0=v
		row G2 "COUNT=2 but KEY_1/VALUE_1 missing" "$(git status 2>&1 | head -1)"
	)
	(
		reset_case
		newrepo
		export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=aidtest.k
		row G3 "COUNT=1, KEY_0 set, VALUE_0 missing" "$(git status 2>&1 | head -1)"
	)
	(
		reset_case
		newrepo
		export GIT_CONFIG_COUNT=x
		row G4 "COUNT not a number" "$(git status 2>&1 | head -1)"
	)
	(
		reset_case
		newrepo
		cfg_append aidtest.k from-count
		out=$(git -c aidtest.k=dash-c -c alias.probe='!git config --show-scope --get-all aidtest.k' probe | awk '{print $1 ":" $2}' | tr '\n' ' ')
		row G5 "child of git -c (alias, hook): what the child sees" "$out"
	)
}

shells() {
	section "R: shell startup files of the agent's shell"
	for f in .bashrc .bash_profile .bashenv .zshenv .zshrc .zprofile .profile; do
		printf 'AIDTEST_RC="${AIDTEST_RC:-}%s "; export AIDTEST_RC\n' "$f" >"$HOME/$f"
	done
	printf 'gh() { :; }\n' >>"$HOME/.zshrc"
	printf 'gh() { :; }\n' >>"$HOME/.bashrc"
	probe_shell() { "$@" 'printf "%s" "${AIDTEST_RC:-none}"' 2>/dev/null; }
	row R1 "bash -c" "read: $(probe_shell bash -c)"
	row R2 "bash -ic (interactive)" "read: $(probe_shell bash -ic)"
	row R3 "bash -lc (login)" "read: $(probe_shell bash -lc)"
	row R4 "bash -c with inherited BASH_ENV=~/.bashenv" "read: $(BASH_ENV="$HOME/.bashenv" probe_shell bash -c)"
	if command -v zsh >/dev/null 2>&1; then
		row R5 "zsh -c" "read: $(probe_shell zsh -c)"
		row R6 "zsh -ic (interactive)" "read: $(probe_shell zsh -ic)"
		row R7 "zsh -lc (login)" "read: $(probe_shell zsh -lc)"
		printf 'unset GIT_CONFIG_COUNT\n' >>"$HOME/.zshenv"
		(
			newrepo
			gset credential.helper "$(helper personal-global)"
			aid_both
			row R8 "zsh -c, ~/.zshenv unsets GIT_CONFIG_COUNT" "$(zsh -c "$(command -v git) config --get-all credential.helper | sed 's/.*helper.\\(.*\\) answer/\\1/' | tr '\\n' ' '")"
		)
		row R9 "zsh -ic, ~/.zshrc defines a gh function" "$(zsh -ic 'whence -w gh' 2>/dev/null)"
	else
		row R5 "zsh not installed" "skipped"
	fi
	row R10 "bash -ic, ~/.bashrc defines a gh function" "gh: $(bash -ic 'type -t gh' 2>/dev/null)"
}

gh_helper() {
	section "GH: gh auth git-credential as the inherited helper"
	gh_bin=$(command -v gh 2>/dev/null || true)
	case $gh_bin in /*) ;; *) gh_bin="" ;; esac
	if [ -z "$gh_bin" ]; then
		row GH- "gh not installed" "skipped"
		return
	fi
	(
		reset_case
		newrepo
		gset credential.https://github.com.helper "!'$gh_bin' auth git-credential"
		export GH_TOKEN=ghs_aidtest_aid-gh-token
		row GH1 "global helper gh auth git-credential, GH_TOKEN set (aid token)" "$(fill)"
	)
	if [ "${AIDTEST_GH_STORE:-}" = 1 ]; then
		(
			reset_case
			newrepo
			gset credential.https://github.com.helper "!'$gh_bin' auth git-credential"
			printf 'github.com:\n    oauth_token: ghs_aidtest_personal-hosts\n    user: personal\n    git_protocol: https\n' >"$GH_CONFIG_DIR/hosts.yml"
			row GH2 "same helper, no GH_TOKEN, personal token in hosts.yml" "$(fill)"
		)
	else
		row GH2 "personal token in hosts.yml without GH_TOKEN" "skipped (set AIDTEST_GH_STORE=1 where gh has no OS keyring)"
	fi
}

aid_recommended() {
	b=$1
	cfg_append "credential.$b.helper" "" "credential.$b.helper" "$(helper aid "${2:-answer}")" \
		"credential.$b.useHttpPath" true "http.$b/.extraHeader" "" \
		url.https://github.com/.insteadOf git@github.com: \
		url.https://github.com/.insteadOf ssh://git@github.com/ \
		url.https://github.com/.insteadOf github.com: \
		url.https://github.com/.insteadOf ssh://github.com/ \
		url.https://github.com/.insteadOf ssh://git@ssh.github.com:443/ \
		url.https://github.com/.insteadOf git+ssh://git@github.com/ \
		url.https://github.com/.insteadOf ssh+git://git@github.com/ \
		commit.gpgSign false tag.gpgSign false tag.forceSignAnnotated false core.askPass ""
}

hostile() {
	use_system
	sysset credential.helper "$(helper osxkeychain-sim)"
	gset credential.helper "$(helper personal-global)"
	gset credential.https://github.com.helper "$(helper gh-setup-git)"
	gset credential.https://github.com/o.helper "$(helper personal-path)"
	gset http.extraHeader "Authorization: Bearer ghs_aidtest_personal-global"
	gset core.askPass "$root/bin/askpass-config"
	gset user.name Personal
	gset user.email personal@example.com
	gset gpg.format ssh
	gset user.signingKey "$root/fake-key.pub"
	gset gpg.ssh.program "$root/bin/signer"
	gset commit.gpgSign true
	gset tag.gpgSign true
	gset tag.forceSignAnnotated true
	git config credential.helper "$(helper personal-repo)"
	export GIT_ASKPASS="$root/bin/askpass-env"
}

recommended() {
	section "X: the recommended set against a combined hostile configuration"
	(
		reset_case
		newrepo git@github.com:o/r.git
		hostile
		cfg_append safe.directory /inherited
		aid_recommended https://github.com
		aid_identity
		row X1 "credential fill for github.com/o/r" "$(fill o/r.git)"
		row X2 "remote git@github.com:o/r.git" "$(urls)"
		: >"$AIDTEST_LOG"
		git commit -q --allow-empty -m x 2>/dev/null
		git tag -a -m x v1 2>/dev/null
		row X3 "commit and annotated tag" "exit=$? $(signed) $(who)"
		row X4 "inherited GIT_CONFIG_COUNT entry kept" "safe.directory=$(git config --get safe.directory)"
	)
	(
		reset_case
		newrepo
		hostile
		aid_recommended https://github.com quit
		row X5 "aid refuses with quit=1; GIT_ASKPASS and core.askPass set" "$(fill o/r.git)"
	)
	if [ -n "$srv" ]; then
		(
			reset_case
			newrepo "$srv/o/r.git"
			hostile
			git config "http.$srv/.extraheader" "AUTHORIZATION: basic $(printf 'x-access-token:ghs_aidtest_checkout' | base64)"
			aid_recommended "$srv"
			row X6 "real transport; the set built for the capture server URL" "$(probe)"
		)
	fi
}

main() {
	isolate
	write_fakes
	build_capture_server
	echo "# git precedence matrix"
	echo "git: $(git --version)  os: $(uname -sm)  bash: ${BASH_VERSION:-$(bash -c 'echo $BASH_VERSION')}  zsh: $(zsh --version 2>/dev/null || echo none)"
	precedence
	credential_helpers
	askpass
	rewrites
	ssh_transport
	headers
	identity
	signing
	config_count
	shells
	gh_helper
	recommended
}

main
