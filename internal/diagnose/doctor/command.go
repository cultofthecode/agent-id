package doctor

import (
	"github.com/spf13/cobra"

	"github.com/cultofthecode/agent-id/internal/diagnose"
)

// NewCommand returns the doctor command. It prints to the command's output
// and reports the exit status as a *diagnose.ExitError; flag and argument
// errors exit 2 with their text redacted.
func NewCommand(h *Handler) *cobra.Command {
	var in Input
	cmd := &cobra.Command{
		Use:   "doctor --profile <name>",
		Short: "Check a profile's configuration, credentials, and conflicts",
		Long:  "doctor runs a fixed catalog of checks for one profile and explains each finding with a fix. It mints a token, resolves the bot identity, and checks repository access, and it refreshes the cached identity of the profile when the host reports another bot. It never prints a token, a key, or a credential.",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return diagnose.UsageError(h.Output.Patterns, h.Output.Home, err)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if in.Profile == "" {
				return &diagnose.ExitError{Code: diagnose.ExitUsage, Message: "--profile is required: doctor checks one explicit profile"}
			}
			out, code, err := h.Handle(cmd.Context(), in)
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
	cmd.Flags().StringVar(&in.Profile, "profile", "", "the profile to check (required)")
	cmd.Flags().BoolVar(&in.JSON, "json", false, "print the report as JSON (schema 1)")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return diagnose.UsageError(h.Output.Patterns, h.Output.Home, err)
	})
	return cmd
}
