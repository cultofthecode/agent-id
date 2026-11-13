package exec

import (
	"os"

	"github.com/spf13/cobra"
)

// NewCommand returns the exec command of h. Flag parsing stops at the first
// non-flag argument, so the command's own flags are never read as agent-id's.
func NewCommand(h *Handler) *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "exec --profile <name> [--] <command> [args…]",
		Short: "Run a command, and only its process tree, as an agent profile",
		Long:  "Run a command with the agent environment of a profile. agent-id replaces itself with the command, so its exit status and signals are the command's. Exit status 125: agent-id failed before starting the command; 126: the command cannot be executed; 127: not found.",
		// An empty argument list is checked by the handler for a clear error.
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: false,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _ := os.Getwd()
			return h.Handle(cmd.Context(), Input{Profile: profile, Argv: args, Dir: dir, Parent: os.Environ()})
		},
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().StringVar(&profile, "profile", "", "agent profile to run the command as (required)")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return failure(ExitFailure, err)
	})
	return cmd
}
