package credentialhelper

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewCommand returns the git-credential command of h. Git starts it as
// `agent-id git-credential --profile <name> <operation>`. It is hidden: a
// person never runs it.
func NewCommand(h *Handler) *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:          "git-credential --profile <name> get|store|erase",
		Short:        "Git credential helper of an agent context",
		Long:         "The Git credential helper that agent-id installs in an agent context. It answers get for the profile's host over HTTPS and for nothing else. Git runs it; do not run it by hand.",
		Hidden:       true,
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if profile == "" {
				return fmt.Errorf("git-credential: --profile is required")
			}
			out, err := h.Handle(cmd.Context(), Input{Profile: profile, Operation: args[0], Stdin: cmd.InOrStdin()})
			if _, werr := fmt.Fprint(cmd.OutOrStdout(), out.Stdout); werr != nil && err == nil {
				return fmt.Errorf("git-credential: cannot write the answer")
			}
			return err
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "agent profile whose host the helper serves (required)")
	return cmd
}
