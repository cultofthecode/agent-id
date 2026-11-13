package whoami

import (
	"github.com/spf13/cobra"

	"github.com/cultofthecode/agent-id/internal/diagnose"
)

// NewCommand returns the whoami command. It prints to the command's output
// and reports the exit status as a *diagnose.ExitError; flag errors exit 2
// with their text redacted.
func NewCommand(h *Handler) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the agent context, identity and credential source of this shell",
		Long:  "whoami prints the context, profile, provider, bot identity, installation, credential source, token status, and activation mechanism of this shell. Outside an agent context it prints personal/unmanaged and reads no configuration. It never prints a token.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return diagnose.UsageError(h.Output.Patterns, h.Output.Home, err)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, code, err := h.Handle(cmd.Context(), Input{JSON: asJSON})
			if err != nil {
				return err
			}
			if _, werr := cmd.OutOrStdout().Write(out); werr != nil {
				return werr
			}
			if code != diagnose.ExitOK {
				return &diagnose.ExitError{Code: code}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the report as JSON (schema 1)")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return diagnose.UsageError(h.Output.Patterns, h.Output.Home, err)
	})
	return cmd
}
