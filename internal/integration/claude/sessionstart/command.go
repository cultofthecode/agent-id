package sessionstart

import (
	"io"
	"os"

	"github.com/spf13/cobra"
)

// NewCommand returns the session-start command of h. It reads the hook input
// from the command's stdin and CLAUDE_ENV_FILE from the environment.
func NewCommand(h *Handler) *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "session-start --profile <name>",
		Short: "Claude Code SessionStart hook: activate the profile for every Bash command",
		Long:  "Run from a Claude Code SessionStart command hook. Appends the guarded activation block of the profile to CLAUDE_ENV_FILE once, or a refusal line when activation fails, so that every Bash command of the session either runs as the agent or stops with exit status 125. Exit status 0: block written or already present; 3: a refusal line was written; 4: no safe or parseable file to write.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in := Input{Profile: profile, EnvFile: os.Getenv("CLAUDE_ENV_FILE"), Parent: os.Environ()}
			if in.EnvFile != "" {
				in.HookInput, _ = io.ReadAll(io.LimitReader(cmd.InOrStdin(), MaxHookInput+1))
			}
			return h.Handle(cmd.Context(), in)
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "agent profile to activate (required)")
	_ = cmd.MarkFlagRequired("profile")
	return cmd
}
