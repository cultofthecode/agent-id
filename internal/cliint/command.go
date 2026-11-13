package cliint

import (
	"os"

	"github.com/spf13/cobra"
)

// NewCommand returns the hidden command that the launcher starts:
// agent-id cli <binary> [--] [args…]. Flag parsing is off, so every argument
// after the binary reaches the CLI as the agent typed it.
func NewCommand(h *Handler) *cobra.Command {
	return &cobra.Command{
		Use:                "cli <binary> [--] [args…]",
		Short:              "Run the provider's CLI with a fresh token (used by the launcher)",
		Hidden:             true,
		DisableFlagParsing: true,
		SilenceUsage:       true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return refuse("", kindContext, "no CLI named; this command is for the launcher that \"agent-id exec\" prepares")
			}
			binary, rest := args[0], args[1:]
			if len(rest) > 0 && rest[0] == "--" {
				rest = rest[1:]
			}
			return h.Handle(cmd.Context(), Input{Binary: binary, Args: rest, Parent: os.Environ()})
		},
	}
}
