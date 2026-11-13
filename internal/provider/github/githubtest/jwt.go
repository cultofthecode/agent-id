package githubtest

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// GitHub's limits for an App JWT: it expires at most ten minutes after it was
// issued, and its issue time may be at most a minute ahead of the server's
// clock.
const (
	maxJWTLifetime = 10 * time.Minute
	maxClockSkew   = time.Minute
)

// Reasons why the fake rejects an App JWT. The messages follow GitHub's.
var (
	errJWTMalformed = errors.New("a JSON web token could not be decoded")
	errJWTAlgorithm = errors.New("'alg' must be RS256")
	errJWTSignature = errors.New("a JSON web token could not be decoded: signature mismatch")
	errJWTIssuer    = errors.New("'Issuer' claim ('iss') must be the App ID")
	errJWTExpired   = errors.New("'Expiration time' claim ('exp') must be a numeric value representing the future time at which the assertion expires")
	errJWTIssuedAt  = errors.New("'Issued at' claim ('iat') must be an Integer representing the time that the assertion was issued")
	errJWTLifetime  = errors.New("'Expiration time' claim ('exp') is too far in the future")
)

// jwtClaims are the claims GitHub reads from an App JWT. iss may be a number
// or a string.
type jwtClaims struct {
	Issuer    json.RawMessage `json:"iss"`
	IssuedAt  *int64          `json:"iat"`
	ExpiresAt *int64          `json:"exp"`
}

// verifyJWT checks token like GitHub checks an App JWT: RS256 signed by key,
// issued by appID, not expired at now, issued no later than now plus the
// clock skew, and valid for at most ten minutes.
func verifyJWT(token string, key *rsa.PublicKey, appID int64, now time.Time) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errJWTMalformed
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return err
	}
	if header.Algorithm != "RS256" {
		return errJWTAlgorithm
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return errJWTMalformed
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature) != nil {
		return errJWTSignature
	}
	var claims jwtClaims
	if err := decodeSegment(parts[1], &claims); err != nil {
		return err
	}
	return checkClaims(claims, appID, now)
}

// checkClaims checks the issuer and the time claims of an App JWT.
func checkClaims(claims jwtClaims, appID int64, now time.Time) error {
	if !issuedBy(claims.Issuer, appID) {
		return errJWTIssuer
	}
	if claims.IssuedAt == nil || time.Unix(*claims.IssuedAt, 0).After(now.Add(maxClockSkew)) {
		return errJWTIssuedAt
	}
	if claims.ExpiresAt == nil || !now.Before(time.Unix(*claims.ExpiresAt, 0)) {
		return errJWTExpired
	}
	if time.Unix(*claims.ExpiresAt, 0).Sub(time.Unix(*claims.IssuedAt, 0)) > maxJWTLifetime {
		return errJWTLifetime
	}
	return nil
}

// issuedBy reports whether the raw iss claim is appID, as a number or a
// string.
func issuedBy(raw json.RawMessage, appID int64) bool {
	want := strconv.FormatInt(appID, 10)
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text == want
	}
	return string(bytes.TrimSpace(raw)) == want
}

// decodeSegment decodes one base64url segment of a JWT as JSON into v.
func decodeSegment(segment string, v any) error {
	data, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return errJWTMalformed
	}
	if err := json.Unmarshal(data, v); err != nil {
		return errJWTMalformed
	}
	return nil
}
