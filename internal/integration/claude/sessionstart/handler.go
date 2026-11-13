package sessionstart

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cultofthecode/agent-id/internal/activation"
	"github.com/cultofthecode/agent-id/internal/integration/claude"
)

// Handler runs session-start.
type Handler struct {
	Deps activation.Deps
	// Binary returns the absolute path of the running agent-id binary.
	Binary func() (string, error)
	// PrivateGroup reports whether a group is private to the current user
	// (UserPrivateGroup in production).
	PrivateGroup func(gid uint32) bool
	// Write appends b to the file (the file's Write in production); tests
	// inject a short write.
	Write func(f *os.File, b []byte) (int, error)
	// CheckSyntax parses content with the named shell without running it.
	CheckSyntax func(shell string, content []byte) error
}

// Handle appends the guarded block of the profile to the env file once, or a
// refusal line when activation fails. Nil is success; an *ExitError carries
// exit status 3 (a refusal line was written) or 4 (no safe or parseable file).
func (h *Handler) Handle(ctx context.Context, in Input) error {
	if in.EnvFile == "" {
		return noFile(ErrNotAHook)
	}
	if !filepath.IsAbs(in.EnvFile) {
		return noFile(errors.New("CLAUDE_ENV_FILE is not an absolute path"))
	}
	f, err := openEnvFile(in.EnvFile, h.privateGroup())
	if err != nil {
		return noFile(err)
	}
	defer func() { _ = f.Close() }()
	return h.handleOpen(ctx, f, in)
}

func (h *Handler) handleOpen(ctx context.Context, f *os.File, in Input) error {
	info, err := f.Stat()
	if err != nil {
		return noFile(err)
	}
	size := info.Size()
	existing, err := io.ReadAll(io.NewSectionReader(f, 0, size))
	if err != nil {
		return noFile(fmt.Errorf("cannot read CLAUDE_ENV_FILE: %w", err))
	}
	shell := shellOf(in.Parent)
	if err := h.checkSyntax(shell, existing); err != nil {
		return noFile(errors.New("CLAUDE_ENV_FILE does not parse; refusing to append to it: " + err.Error()))
	}
	block, reason := h.render(ctx, string(existing), in)
	if reason != nil {
		return h.refuse(f, shell, size, existing, in.Profile, reason)
	}
	if claude.HasBlock(string(existing), block) {
		return nil
	}
	return h.appendBlock(f, shell, size, in.Profile, block)
}

func (h *Handler) render(ctx context.Context, existing string, in Input) (string, error) {
	if !claude.ValidProfile(in.Profile) {
		return "", errors.New("the profile name is not valid")
	}
	cwd, err := parseHook(in.HookInput)
	if err != nil {
		return "", err
	}
	if other := otherProfile(claude.ProfilesIn(existing), in.Profile); other != "" {
		return "", fmt.Errorf("the env file already activates profile %s; this hook asks for %s", display(other), in.Profile)
	}
	binary, err := h.Binary()
	if err != nil {
		return "", err
	}
	act, err := activation.Activate(ctx, h.Deps, activation.Request{
		Profile: in.Profile, Mechanism: activation.MechanismEnv, Binary: binary, Dir: cwd, Parent: in.Parent,
	})
	if err != nil {
		return "", err
	}
	text, err := activation.Render(in.Parent, act)
	if err != nil {
		return "", err
	}
	return claude.GuardedBlock(text, claude.GuardRefusal(in.Profile)), nil
}

func display(profile string) string {
	if profile == "?" {
		return "(an invalid name)"
	}
	return profile
}

func otherProfile(profiles []string, want string) string {
	for _, p := range profiles {
		if p != want {
			return p
		}
	}
	return ""
}

type hookInput struct {
	Event string `json:"hook_event_name"`
	Cwd   any    `json:"cwd"`
}

func parseHook(data []byte) (string, error) {
	if len(data) > MaxHookInput {
		return "", errors.New("the hook input is larger than 64 KiB")
	}
	var h hookInput
	if err := json.Unmarshal(data, &h); err != nil {
		return "", errors.New("the hook input is not a JSON object")
	}
	if h.Event != "SessionStart" {
		return "", errors.New("the hook input is not a SessionStart event")
	}
	cwd, ok := h.Cwd.(string)
	if !ok || !filepath.IsAbs(cwd) {
		return "", errors.New("the hook input has no absolute cwd")
	}
	return cwd, nil
}

func (h *Handler) appendBlock(f *os.File, shell string, size int64, profile, block string) error {
	n, err := h.write(f, []byte(block))
	if err == nil && n == len(block) {
		all, rerr := io.ReadAll(io.NewSectionReader(f, 0, size+int64(n)))
		if rerr == nil && h.checkSyntax(shell, all) == nil {
			return nil
		}
		err = errors.New("the env file does not parse after the append")
	} else if err == nil {
		err = errors.New("short write")
	}
	return h.rollback(f, shell, size, profile, err)
}

func (h *Handler) rollback(f *os.File, shell string, size int64, profile string, cause error) error {
	if terr := f.Truncate(size); terr != nil {
		return noFile(fmt.Errorf("cannot write CLAUDE_ENV_FILE (%v) and cannot roll back: %v", cause, terr))
	}
	existing, rerr := io.ReadAll(io.NewSectionReader(f, 0, size))
	if rerr != nil {
		return noFile(fmt.Errorf("cannot write CLAUDE_ENV_FILE: %v", cause))
	}
	return h.refuse(f, shell, size, existing, profile, fmt.Errorf("cannot write the activation block: %v", cause))
}

func (h *Handler) refuse(f *os.File, shell string, size int64, existing []byte, profile string, reason error) error {
	line := claude.RefusalLine(reason.Error(), profile)
	if bytes.Contains(existing, []byte(line)) {
		return refused(reason)
	}
	prefix := ""
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		prefix = "\n"
	}
	text := []byte(prefix + line)
	n, err := h.write(f, text)
	if err != nil || n != len(text) || !h.parses(f, shell, size+int64(len(text))) {
		_ = f.Truncate(size)
		return noFile(fmt.Errorf("cannot write the refusal line (%v)", reason))
	}
	return refused(reason)
}

func (h *Handler) parses(f *os.File, shell string, size int64) bool {
	all, err := io.ReadAll(io.NewSectionReader(f, 0, size))
	return err == nil && h.checkSyntax(shell, all) == nil
}

func (h *Handler) write(f *os.File, b []byte) (int, error) {
	if h.Write != nil {
		return h.Write(f, b)
	}
	return f.Write(b)
}

func (h *Handler) privateGroup() func(uint32) bool {
	if h.PrivateGroup != nil {
		return h.PrivateGroup
	}
	return UserPrivateGroup
}

func (h *Handler) checkSyntax(shell string, content []byte) error {
	if h.CheckSyntax != nil {
		return h.CheckSyntax(shell, content)
	}
	return ParseWith(shell, content)
}

// shellOf is the shell that will run the env file: basename($SHELL) when that
// is bash or zsh, and bash otherwise.
func shellOf(parent []string) string {
	for _, kv := range slices.Backward(parent) {
		if v, ok := strings.CutPrefix(kv, "SHELL="); ok {
			if b := filepath.Base(v); b == "bash" || b == "zsh" {
				return b
			}
			break
		}
	}
	return "bash"
}

// ParseWith checks content with `<shell> -n`, which parses and runs nothing.
func ParseWith(shell string, content []byte) error {
	path, err := exec.LookPath(shell)
	if err != nil {
		return fmt.Errorf("%s is not installed: cannot check the file", shell)
	}
	args := []string{"-n"}
	if shell == "zsh" {
		args = []string{"-f", "-n"}
	}
	cmd := exec.Command(path, args...)
	cmd.Stdin = bytes.NewReader(content)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s -n: %s", shell, firstLine(string(out)))
	}
	return nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}
