package profile

import (
	"regexp"
	"strconv"
	"strings"
)

// pemMarker starts every PEM block, such as a pasted private key.
const pemMarker = "-----BEGIN"

// jwtShape matches a JSON Web Token: three base64url segments separated by
// dots, the first starting with the encoding of `{"`. The header starts at a
// token boundary, so a word that merely contains "eyJ", such as
// "MoneyJar.prod.pem", does not match. The header has at least 15
// characters (the encoding of `{"alg":"x"}`) and the payload at least 2; the
// signature may be empty, as in an unsecured JWT.
var jwtShape = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])eyJ[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{2,}\.[A-Za-z0-9_-]*`)

// scanSecrets returns ErrInlineSecret for the first string value or member
// name of the document, in document order, that looks like token or key
// material: a registered token prefix, a PEM marker, a JWT shape, a line
// break, or a NUL byte. The error names the field path, never the value; for
// a member name it names the object that holds it.
func scanSecrets(root *node, prefixes []string) error {
	return scanNode(root, "", prefixes)
}

// scanNode scans n, found at path, and its descendants.
func scanNode(n *node, path string, prefixes []string) error {
	switch n.kind {
	case kindString:
		if looksSecret(n.text, prefixes) {
			return inlineSecret(path)
		}
	case kindObject:
		for _, m := range n.members {
			if looksSecret(m.name, prefixes) {
				return inlineSecret(path)
			}
			if err := scanNode(m.value, child(path, m.name), prefixes); err != nil {
				return err
			}
		}
	case kindArray:
		for i, e := range n.elems {
			if err := scanNode(e, path+"["+strconv.Itoa(i)+"]", prefixes); err != nil {
				return err
			}
		}
	}
	return nil
}

// looksSecret reports whether s contains token or key material.
func looksSecret(s string, prefixes []string) bool {
	if strings.ContainsAny(s, "\n\r\x00") || strings.Contains(s, pemMarker) || jwtShape.MatchString(s) {
		return true
	}
	for _, prefix := range prefixes {
		if strings.Contains(s, prefix) {
			return true
		}
	}
	return false
}

// inlineSecret is the error for a secret at path.
func inlineSecret(path string) error {
	if path == "" {
		path = "(top level)"
	}
	return Errorf(ErrInlineSecret, "%s: contains what looks like a token or private key; the config holds references only — remove the value and revoke it if it was real", path)
}

// child returns the path of the member name of the object at path. The name
// is sanitized, so a path is always safe to echo.
func child(path, name string) string {
	if path == "" {
		return sanitize(name)
	}
	return path + "." + sanitize(name)
}
