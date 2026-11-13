package env

import (
	"errors"
	"os"

	"github.com/spf13/cobra"
)

// NewCommand returns the env command of h. It prints the block only on
// success.
func NewCommand(h *Handler) *cobra.Command {
	var profile, shell string
	cmd := &cobra.Command{
		Use:   "env --profile <name> --shell bash|zsh",
		Short: "Print the agent environment of a profile as a shell block",
		Long:  "Print the child environment of agent-id exec as one block of shell statements, chained so that the block succeeds or fails as a whole. It contains no secret. Evaluate it in your own terminal with eval \"$(agent-id env --profile <name> --shell zsh)\". Exit status 125: activation failed; nothing is printed.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return failure(errors.New("unexpected argument; usage: agent-id env --profile <name> --shell bash|zsh"))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, _ := os.Getwd()
			block, err := h.Handle(cmd.Context(), Input{Profile: profile, Shell: shell, Dir: dir, Parent: os.Environ()})
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write([]byte(block))
			return err
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "agent profile (required)")
	cmd.Flags().StringVar(&shell, "shell", "", "shell syntax: bash or zsh (required)")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return failure(err) })
	return cmd
}
