// Package diagnose is the core of agent-id whoami and agent-id doctor
// (spec .ai/specs/2026-11-13-whoami-doctor-output.md): the typed reports, the
// pure checks over a Snapshot, the redaction of every string, the guard that
// scans the rendered output, and the text and JSON renderers.
//
// No report field can hold a secret. Every string of a report passes Redact,
// and the rendered bytes pass Guard; output that still matches a secret
// pattern is withheld. The package knows no forge: token prefixes come from
// the profile registry, and labels and hosts from the provider.
package diagnose

import (
	"errors"
	"fmt"
)

// Exit codes of whoami and doctor (spec, API Contracts).
const (
	ExitOK       = 0
	ExitFailure  = 1
	ExitUsage    = 2
	ExitWithheld = 3
)

// ErrWithheld means that the rendered output matched a secret pattern and was
// not written.
var ErrWithheld = errors.New("diagnose: output withheld: it matched a secret pattern")

// WithheldMessage is the line that a command writes to stderr when it
// withholds its output.
const WithheldMessage = "output withheld: it matched a secret pattern (this is a bug; please report it without the output)"

// ExitError is the error of a diagnostic command that sets the exit status.
// Message, when not empty, is the text for stderr; it has already passed
// Redact and Guard, so the composition root prints it as it is.
type ExitError struct {
	Code    int
	Message string
}

// Error implements error.
func (e *ExitError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Message
}

// Status is the outcome of one check.
type Status string

// The statuses of a check. Fail means exec refuses, or agent mode would fail
// open or not work; warn a conflict that agent mode neutralizes or a
// documented bypass risk; info is context; skip means a precondition is
// missing.
const (
	StatusOK   Status = "ok"
	StatusInfo Status = "info"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Result is the outcome of one check. None of its fields holds a secret; the
// renderers redact them anyway.
type Result struct {
	ID     string `json:"id"`
	Status Status `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Source string `json:"source"`
	Fix    string `json:"fix"`
}

// Summary counts the results of a report by status.
type Summary struct {
	OK   int `json:"ok"`
	Info int `json:"info"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Skip int `json:"skip"`
}

// DoctorReport is the result of agent-id doctor.
type DoctorReport struct {
	Profile  string
	Provider string
	Host     string
	Results  []Result
}

// Summary counts the results by status.
func (r DoctorReport) Summary() Summary {
	var s Summary
	for _, res := range r.Results {
		switch res.Status {
		case StatusOK:
			s.OK++
		case StatusInfo:
			s.Info++
		case StatusWarn:
			s.Warn++
		case StatusFail:
			s.Fail++
		case StatusSkip:
			s.Skip++
		}
	}
	return s
}

// ExitCode is 1 when at least one check failed, otherwise 0.
func (r DoctorReport) ExitCode() int {
	if r.Summary().Fail > 0 {
		return ExitFailure
	}
	return ExitOK
}
