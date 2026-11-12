package testenv

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// Recorder is a testing.TB that records the errors a helper reports instead of
// failing the test. Negative tests pass it to a fake or a guard to prove that
// the helper would have failed the test. Every other method, including Fatal
// and Setenv, acts on the wrapped test.
type Recorder struct {
	testing.TB

	mu     sync.Mutex
	errors []string
}

// NewRecorder returns a Recorder that wraps t.
func NewRecorder(t testing.TB) *Recorder {
	return &Recorder{TB: t}
}

// Error records the error instead of failing the test.
func (r *Recorder) Error(args ...any) {
	r.record(fmt.Sprint(args...))
}

// Errorf records the error instead of failing the test.
func (r *Recorder) Errorf(format string, args ...any) {
	r.record(fmt.Sprintf(format, args...))
}

// Errors returns the recorded errors in order.
func (r *Recorder) Errors() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.errors...)
}

// Reported reports whether an error that contains text was recorded.
func (r *Recorder) Reported(text string) bool {
	for _, e := range r.Errors() {
		if strings.Contains(e, text) {
			return true
		}
	}
	return false
}

// record appends message to the recorded errors.
func (r *Recorder) record(message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors = append(r.errors, message)
}
