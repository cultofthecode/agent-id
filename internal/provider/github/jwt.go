package github

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// The time claims of an App JWT: issued a minute in the past to absorb clock
// skew, and expiring nine minutes from now, under GitHub's ten-minute limit.
const (
	jwtBackdate = 60 * time.Second
	jwtLifetime = 9 * time.Minute
)

// jwtHeader is the fixed header of an App JWT.
var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

// jwtClaims are the claims of an App JWT.
type jwtClaims struct {
	IssuedAt  int64 `json:"iat"`
	ExpiresAt int64 `json:"exp"`
	Issuer    int64 `json:"iss"`
}

// signJWT returns an RS256 App JWT for appID, signed with key, valid from a
// minute before now until nine minutes after now.
func signJWT(key *rsa.PrivateKey, appID int64, now time.Time) (string, error) {
	claims, err := json.Marshal(jwtClaims{
		IssuedAt:  now.Add(-jwtBackdate).Unix(),
		ExpiresAt: now.Add(jwtLifetime).Unix(),
		Issuer:    appID,
	})
	if err != nil {
		return "", fmt.Errorf("encode the App JWT claims: %w", err)
	}
	signingInput := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign the App JWT: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
