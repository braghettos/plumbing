package jwtutil

import (
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultISS = "krateo.io"
)

type UserInfo struct {
	Username string   `json:"username"`
	Groups   []string `json:"groups"`
}

type KrateoClaims struct {
	UserInfo
	jwt.RegisteredClaims
}

type CreateTokenOptions struct {
	Username   string
	Groups     []string
	Duration   time.Duration
	KeyID      string
	PrivateKey *rsa.PrivateKey
}

// CreateToken generates a signed JWT token using the provided
// username, group list, and expiration duration.
// The token is signed asymmetrically using the RS256 algorithm and the
// supplied RSA private key, and carries a "kid" header identifying the key
// so that JWKS-based validators (e.g. agentgateway) can select the matching
// public key. The corresponding public key must be published in the JWKS
// under the same KeyID.
func CreateToken(opts CreateTokenOptions) (string, error) {
	if opts.PrivateKey == nil {
		return "", fmt.Errorf("private key cannot be nil")
	}

	if opts.KeyID == "" {
		return "", fmt.Errorf("key ID cannot be empty")
	}

	if opts.Username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}

	now := time.Now()

	claims := KrateoClaims{
		UserInfo: UserInfo{
			Username: opts.Username,
			Groups:   opts.Groups,
		},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    defaultISS,
			ExpiresAt: jwt.NewNumericDate(now.Add(opts.Duration)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Subject:   opts.Username,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = opts.KeyID

	return token.SignedString(opts.PrivateKey)
}
