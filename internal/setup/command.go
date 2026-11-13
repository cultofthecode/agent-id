package setup

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

// maxListedRepositories is the number of repositories the summary lists.
const maxListedRepositories = 20

// labelWidth is the width of the label column of the summary.
const labelWidth = 15

// NewCommand returns the setup command with one subcommand per onboarder of
// h, whose flags are --profile and the onboarder's setup fields.
func NewCommand(h *Handler) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Create or update an agent profile from an existing provider app",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	for _, o := range h.Onboarders {
		cmd.AddCommand(newProviderCommand(h, o.Name()))
	}
	return cmd
}

// newProviderCommand returns the setup subcommand of the provider name.
func newProviderCommand(h *Handler, name string) *cobra.Command {
	onboarder, _ := h.onboarder(name)
	in := Input{Provider: name, Values: map[string]string{}}
	values := map[string]*string{}
	cmd := &cobra.Command{
		Use:   name + " --profile <name> " + flagSynopsis(h, name),
		Short: "Verify a " + name + " app and write the agent profile",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for field, value := range values {
				in.Values[field] = *value
			}
			dir, err := os.Getwd()
			if err == nil {
				in.Dir = dir
			}
			out, err := h.Handle(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("setup %s: %w", name, err)
			}
			return Render(cmd.OutOrStdout(), out)
		},
	}
	cmd.Flags().StringVar(&in.Profile, "profile", "", profileUsage)
	for _, field := range onboarder.SetupFields() {
		values[field.Name] = cmd.Flags().String(field.Name, "", field.Usage)
	}
	return cmd
}

// flagSynopsis returns the provider flags of the usage line.
func flagSynopsis(h *Handler, name string) string {
	onboarder, _ := h.onboarder(name)
	var parts []string
	for _, field := range onboarder.SetupFields() {
		parts = append(parts, "--"+field.Name+" <value>")
	}
	return strings.Join(parts, " ")
}

// Render writes the summary of out to w. Every value goes through clean, so
// no control or format character reaches the terminal.
func Render(w io.Writer, out Output) error {
	var b strings.Builder
	state := "updated"
	if out.Created {
		state = "created"
	}
	line(&b, "Profile:", out.Profile+" ("+state+")")
	line(&b, "Provider:", out.Provider+" ("+out.Host+")")
	for _, detail := range out.Details {
		line(&b, detail.Label+":", detail.Value)
	}
	line(&b, "Identity:", out.Account)
	line(&b, "Commit email:", out.Identity.Email)
	repositories(&b, out.Repositories)
	line(&b, "Config:", out.ConfigPath)
	b.WriteString("\nNext: agent-id exec --profile " + clean(out.Profile) + " -- <command>\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// repositories writes the repository count and up to maxListedRepositories
// names.
func repositories(b *strings.Builder, names []string) {
	if len(names) == 0 {
		line(b, "Repositories:", "0 (the installation can access no repository; add repositories in the installation's settings)")
		return
	}
	line(b, "Repositories:", strconv.Itoa(len(names)))
	for i, name := range names {
		if i == maxListedRepositories {
			line(b, "", "… and "+strconv.Itoa(len(names)-maxListedRepositories)+" more")
			return
		}
		line(b, "", name)
	}
}

// line writes one summary line with label padded to labelWidth.
func line(b *strings.Builder, label, value string) {
	label = clean(label)
	b.WriteString(label)
	b.WriteString(strings.Repeat(" ", max(labelWidth-len([]rune(label)), 1)))
	b.WriteString(clean(value))
	b.WriteByte('\n')
}

// clean replaces control and Unicode format characters in s with "?".
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == unicode.ReplacementChar {
			return '?'
		}
		return r
	}, s)
}
