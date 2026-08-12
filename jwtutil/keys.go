package jwtutil

import (
	"crypto/rsa"

	"github.com/golang-jwt/jwt/v5"
)

// ParseRSAPrivateKeyFromPEM parses a PEM-encoded RSA private key, as produced
// by e.g. `openssl genrsa`. It is the key type expected by CreateToken.
func ParseRSAPrivateKeyFromPEM(pemBytes []byte) (*rsa.PrivateKey, error) {
	return jwt.ParseRSAPrivateKeyFromPEM(pemBytes)
}

// ParseRSAPublicKeyFromPEM parses a PEM-encoded RSA public key. It is the key
// type expected by Validate.
func ParseRSAPublicKeyFromPEM(pemBytes []byte) (*rsa.PublicKey, error) {
	return jwt.ParseRSAPublicKeyFromPEM(pemBytes)
}
