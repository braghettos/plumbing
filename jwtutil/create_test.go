package jwtutil_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/krateo-platformops/plumbing/jwtutil"
	"github.com/stretchr/testify/assert"
)

func TestCreateToken(t *testing.T) {
	tests := []struct {
		name     string
		username string
		groups   []string
		duration time.Duration
		secret   string
	}{
		{
			name:     "with environment secret",
			username: "alice",
			groups:   []string{"admin", "dev"},
			duration: time.Minute * 30,
			secret:   "envSecret123",
		},
		{
			name:     "with default secret fallback",
			username: "bob",
			groups:   []string{},
			duration: time.Minute * 15,
			secret:   "abbracadabra!",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := jwtutil.CreateTokenOptions{
				Username:   tc.username,
				Groups:     tc.groups,
				Duration:   tc.duration,
				SigningKey: tc.secret,
			}

			tokenStr, err := jwtutil.CreateToken(opts)
			assert.NoError(t, err)
			assert.NotEmpty(t, tokenStr)

			// Parse and validate the token
			token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
				return []byte(tc.secret), nil
			})
			assert.NoError(t, err)
			assert.True(t, token.Valid)

			claims, ok := token.Claims.(jwt.MapClaims)
			assert.True(t, ok)

			assert.Equal(t, tc.username, claims["sub"])
			assert.Equal(t, tc.username, claims["username"])
			assert.ElementsMatch(t, tc.groups, claims["groups"])
		})
	}
}
