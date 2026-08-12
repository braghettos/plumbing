package jwtutil_test

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/krateo-platformops/plumbing/jwtutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserInfo(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	const keyID = "test-key-id"

	tests := []struct {
		title            string
		prepare          func() string
		expectErr        bool
		expectedUsername string
		expectedGrp      []string
	}{
		{
			title: "valid token",
			prepare: func() string {
				token, _ := jwtutil.CreateToken(jwtutil.CreateTokenOptions{
					Username:   "alice",
					Groups:     []string{"admin", "dev"},
					Duration:   time.Minute,
					KeyID:      keyID,
					PrivateKey: privateKey,
				})
				return token
			},
			expectErr:        false,
			expectedUsername: "alice",
			expectedGrp:      []string{"admin", "dev"},
		},
		{
			title: "expired token",
			prepare: func() string {
				token, _ := jwtutil.CreateToken(jwtutil.CreateTokenOptions{
					Username:   "bob",
					Groups:     []string{"users"},
					Duration:   -time.Minute,
					KeyID:      keyID,
					PrivateKey: privateKey,
				})
				return token
			},
			expectErr: true,
		},
		{
			title: "malformed token",
			prepare: func() string {
				return "not-a-real-token"
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			token := tc.prepare()

			user, err := jwtutil.Validate(&privateKey.PublicKey, token)

			if tc.expectErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tc.expectedUsername, user.Username)
			assert.ElementsMatch(t, tc.expectedGrp, user.Groups)
		})
	}
}

// TestValidateRejectsWrongKey ensures a token signed by one keypair does not
// validate against a different public key.
func TestValidateRejectsWrongKey(t *testing.T) {
	signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	token, err := jwtutil.CreateToken(jwtutil.CreateTokenOptions{
		Username:   "alice",
		Groups:     []string{"admin"},
		Duration:   time.Minute,
		KeyID:      "kid",
		PrivateKey: signingKey,
	})
	require.NoError(t, err)

	_, err = jwtutil.Validate(&otherKey.PublicKey, token)
	assert.ErrorIs(t, err, jwtutil.ErrTokenInvalid)
}
