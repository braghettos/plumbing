package jwtutil_test

import (
	"testing"
	"time"

	"github.com/krateo-platformops/plumbing/jwtutil"
	"github.com/stretchr/testify/assert"
)

func TestGetUserInfo(t *testing.T) {
	const (
		secret = "test-secret"
	)

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
					SigningKey: secret,
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
					SigningKey: secret,
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

			user, err := jwtutil.Validate(secret, token)

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
