package cliint

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	binaryPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	jwtShape      = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.`)
	envNamePat    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

const (
	withheld   = "details withheld"
	maxReason  = 300
	pemMarker  = "-----BEGIN"
	agentIDPre = "AGENT_ID_"
)

// Scrub returns reason when it is safe to show: no token prefix, no PEM
// marker, no JWT shape, no control character, at most 300 bytes. Otherwise it
// returns "details withheld". It is the last guard on text that comes from a
// lower layer.
func Scrub(reason string, tokenPrefixes []string) string {
	if reason == "" || len(reason) > maxReason || strings.Contains(reason, pemMarker) || jwtShape.MatchString(reason) {
		return withheld
	}
	for _, p := range tokenPrefixes {
		if p != "" && strings.Contains(reason, p) {
			return withheld
		}
	}
	for _, r := range reason {
		if unicode.IsControl(r) || !unicode.IsPrint(r) {
			return withheld
		}
	}
	return reason
}
