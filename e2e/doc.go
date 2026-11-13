// Package e2e holds the end-to-end validation of agent-id against a real
// GitHub repository (issue #27, brief section 12).
//
// The tests live behind the build tag "e2e" and skip unless the environment
// variable AGENT_ID_E2E is exactly "1". The owner runs them by hand with the
// steps of docs/runbooks/e2e.md. CI never runs them: go test ./... compiles
// no file of this package, because every test file carries the tag.
package e2e
