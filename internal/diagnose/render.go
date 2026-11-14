package diagnose

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Output renders the reports of a command. For each report every string
// passes AbbreviateHome and Redact, the sanitized strings pass Guard, and
// the rendered bytes pass Guard again. A match returns ErrWithheld and no
// bytes.
type Output struct {
	Patterns Patterns
	Home     string
	// redact replaces Patterns.Redact; only tests that prove the guard set
	// it.
	redact func(string) string
}

// sanitize returns s cleaned for output.
func (o Output) sanitize(s string) string {
	redact := o.redact
	if redact == nil {
		redact = o.Patterns.Redact
	}
	return redact(AbbreviateHome(s, o.Home))
}

// finish guards the rendered bytes.
func (o Output) finish(b []byte) ([]byte, error) {
	if err := o.Patterns.Guard(b); err != nil {
		return nil, err
	}
	return b, nil
}

// guardStrings guards every sanitized string.
func (o Output) guardStrings(ss ...string) error {
	for _, s := range ss {
		if err := o.Patterns.Guard([]byte(s)); err != nil {
			return err
		}
	}
	return nil
}

// whoamiLine is one line of whoami: the label and a colon padded to 15
// columns, then the value.
func whoamiLine(label, value string) string {
	return fmt.Sprintf("%-15s%s\n", label+":", value)
}

// Whoami renders r as text, or as JSON when asJSON is set.
func (o Output) Whoami(r WhoamiReport, asJSON bool) ([]byte, error) {
	clean := o.cleanWhoami(r)
	if err := o.guardStrings(clean.contextLine(), clean.Profile, clean.Identity, clean.tokenLine()); err != nil {
		return nil, err
	}
	for _, d := range clean.Details {
		if err := o.guardStrings(d.Label, d.Value); err != nil {
			return nil, err
		}
	}
	if err := o.guardStrings(clean.Warnings...); err != nil {
		return nil, err
	}
	if !asJSON {
		return o.finish([]byte(whoamiText(clean)))
	}
	out, err := whoamiJSON(clean)
	if err != nil {
		return nil, err
	}
	return o.finish(out)
}

// cleanWhoami returns r with every string sanitized.
func (o Output) cleanWhoami(r WhoamiReport) WhoamiReport {
	r.Context = o.sanitize(r.Context)
	r.Problem = o.sanitize(r.Problem)
	r.Profile = o.sanitize(r.Profile)
	r.ProfileNote = o.sanitize(r.ProfileNote)
	r.Provider = o.sanitize(r.Provider)
	r.Host = o.sanitize(r.Host)
	r.Identity = o.sanitize(r.Identity)
	r.Activation = o.sanitize(r.Activation)
	r.Token.Class = o.sanitize(r.Token.Class)
	r.Token.Reason = o.sanitize(r.Token.Reason)
	details := make([]Detail, len(r.Details))
	for i, d := range r.Details {
		details[i] = Detail{Label: o.sanitize(d.Label), Value: o.sanitize(d.Value)}
	}
	r.Details = details
	warnings := make([]string, len(r.Warnings))
	for i, w := range r.Warnings {
		warnings[i] = o.sanitize(w)
	}
	r.Warnings = warnings
	if len(warnings) == 0 {
		r.Warnings = nil
	}
	return r
}

// whoamiText renders the stable text of whoami.
func whoamiText(r WhoamiReport) string {
	var b strings.Builder
	b.WriteString(whoamiLine("Context", r.contextLine()))
	if r.Context == ContextPersonal {
		return b.String()
	}
	if r.Profile != "" {
		profile := r.Profile
		if r.ProfileNote != "" {
			profile += " (" + r.ProfileNote + ")"
		}
		b.WriteString(whoamiLine("Profile", profile))
	}
	if r.Provider != "" {
		b.WriteString(whoamiLine("Provider", r.Provider+" ("+r.Host+")"))
		b.WriteString(whoamiLine("Identity", identityLine(r)))
		for _, d := range r.Details {
			b.WriteString(whoamiLine(d.Label, d.Value))
		}
	}
	b.WriteString(whoamiLine("Token status", r.tokenLine()))
	if r.Activation != "" {
		b.WriteString(whoamiLine("Activation", r.Activation))
	}
	for _, w := range r.Warnings {
		b.WriteString(whoamiLine("Warning", w))
	}
	return b.String()
}

// identityLine is the value of the Identity label.
func identityLine(r WhoamiReport) string {
	if r.Identity == "" {
		return "not resolved (run agent-id doctor --profile " + r.Profile + ")"
	}
	return r.Identity
}

// jsonToken is the "token" member of the whoami JSON.
type jsonToken struct {
	Status    string  `json:"status"`
	ExpiresAt *string `json:"expires_at"`
	Class     *string `json:"class"`
	Reason    *string `json:"reason"`
}

// jsonDetail is one member of "details".
type jsonDetail struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// whoamiAgentJSON is the whoami JSON inside an agent context, schema 1.
type whoamiAgentJSON struct {
	Schema         int          `json:"schema"`
	Context        string       `json:"context"`
	ContextProblem *string      `json:"context_problem"`
	Profile        *string      `json:"profile"`
	Provider       *string      `json:"provider"`
	Host           *string      `json:"host"`
	Identity       *string      `json:"identity"`
	Details        []jsonDetail `json:"details"`
	Token          jsonToken    `json:"token"`
	Activation     *string      `json:"activation"`
	Warnings       []string     `json:"warnings,omitempty"`
}

// orNil returns nil for an empty string.
func orNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// whoamiJSON renders the JSON contract of whoami, schema 1.
func whoamiJSON(r WhoamiReport) ([]byte, error) {
	if r.Context == ContextPersonal {
		return jsonLine(map[string]any{"schema": 1, "context": ContextPersonal})
	}
	token := jsonToken{Status: r.Token.Status, Class: orNil(r.Token.Class)}
	if !r.Token.ExpiresAt.IsZero() {
		at := r.Token.ExpiresAt.UTC().Format(time.RFC3339)
		token.ExpiresAt = &at
	}
	if r.Token.Status != TokenValid {
		token.Reason = orNil(r.Token.Reason)
	}
	details := []jsonDetail{}
	for _, d := range r.Details {
		details = append(details, jsonDetail(d))
	}
	return jsonLine(whoamiAgentJSON{
		Schema:         1,
		Context:        r.Context,
		ContextProblem: orNil(r.Problem),
		Profile:        orNil(r.Profile),
		Provider:       orNil(r.Provider),
		Host:           orNil(r.Host),
		Identity:       orNil(r.Identity),
		Details:        details,
		Token:          token,
		Activation:     orNil(r.Activation),
		Warnings:       r.Warnings,
	})
}

// jsonLine encodes v as one JSON line without HTML escaping.
func jsonLine(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("diagnose: encode the report: %w", err)
	}
	return buf.Bytes(), nil
}

// Doctor renders r as text, or as JSON when asJSON is set.
func (o Output) Doctor(r DoctorReport, asJSON bool) ([]byte, error) {
	clean := o.cleanDoctor(r)
	for _, res := range clean.Results {
		if err := o.guardStrings(res.Title, res.Detail, res.Source, res.Fix); err != nil {
			return nil, err
		}
	}
	if !asJSON {
		return o.finish([]byte(doctorText(clean)))
	}
	out, err := doctorJSON(clean)
	if err != nil {
		return nil, err
	}
	return o.finish(out)
}

// cleanDoctor returns r with every string sanitized.
func (o Output) cleanDoctor(r DoctorReport) DoctorReport {
	r.Profile = o.sanitize(r.Profile)
	r.Provider = o.sanitize(r.Provider)
	r.Host = o.sanitize(r.Host)
	results := make([]Result, len(r.Results))
	for i, res := range r.Results {
		results[i] = Result{
			ID:     o.sanitize(res.ID),
			Status: Status(o.sanitize(string(res.Status))),
			Title:  o.sanitize(res.Title),
			Detail: o.sanitize(res.Detail),
			Source: o.sanitize(res.Source),
			Fix:    o.sanitize(res.Fix),
		}
	}
	r.Results = results
	return r
}

// statusLabel is the status column of the text format.
func statusLabel(s Status) string {
	if s == StatusFail {
		return "FAIL"
	}
	return string(s)
}

// doctorText renders the human text of doctor.
func doctorText(r DoctorReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "agent-id doctor — profile %s%s\n\n", r.Profile, providerSuffix(r))
	width := 0
	for _, res := range r.Results {
		width = max(width, len(res.ID))
	}
	for _, res := range r.Results {
		text := res.Title
		if res.Detail != "" {
			text += ": " + res.Detail
		}
		if res.Source != "" {
			text += " (" + res.Source + ")"
		}
		fmt.Fprintf(&b, "%-5s %-*s  %s\n", statusLabel(res.Status), width, res.ID, text)
		if res.Fix != "" {
			fmt.Fprintf(&b, "      fix: %s\n", res.Fix)
		}
	}
	s := r.Summary()
	fmt.Fprintf(&b, "\n%s, %s, %d ok\n", plural(s.Fail, "failure"), plural(s.Warn, "warning"), s.OK)
	return b.String()
}

// providerSuffix is " (provider, host)" once the profile is resolved.
func providerSuffix(r DoctorReport) string {
	if r.Provider == "" {
		return ""
	}
	return " (" + r.Provider + ", " + r.Host + ")"
}

// plural returns "1 failure" or "2 failures".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// doctorJSONDoc is the doctor JSON contract, schema 1.
type doctorJSONDoc struct {
	Schema   int      `json:"schema"`
	Profile  string   `json:"profile"`
	Provider string   `json:"provider"`
	Host     string   `json:"host"`
	Results  []Result `json:"results"`
	Summary  Summary  `json:"summary"`
}

// doctorJSON renders the JSON contract of doctor.
func doctorJSON(r DoctorReport) ([]byte, error) {
	results := r.Results
	if results == nil {
		results = []Result{}
	}
	return jsonLine(doctorJSONDoc{Schema: 1, Profile: r.Profile, Provider: r.Provider, Host: r.Host, Results: results, Summary: r.Summary()})
}
