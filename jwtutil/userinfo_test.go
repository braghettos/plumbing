package jwtutil_test

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/krateo-platformops/plumbing/jwtutil"
	"github.com/stretchr/testify/require"
)

func TestExtractUserInfo(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	const keyID = "test-key-id"

	tests := []struct {
		name           string
		creatTokenFunc func() string
		want           jwtutil.UserInfo
		expectErr      bool
	}{
		{
			name: "valid token with sub and groups",
			creatTokenFunc: func() string {
				token, _ := jwtutil.CreateToken(jwtutil.CreateTokenOptions{
					Username:   "alice",
					Groups:     []string{"admin", "dev"},
					Duration:   time.Minute,
					KeyID:      keyID,
					PrivateKey: privateKey,
				})
				return token
			},
			want: jwtutil.UserInfo{
				Username: "alice",
				Groups:   []string{"admin", "dev"},
			},
			expectErr: false,
		},
		{
			name: "missing sub claim",
			creatTokenFunc: func() string {
				token, _ := jwtutil.CreateToken(jwtutil.CreateTokenOptions{
					Groups:     []string{"admin", "dev"},
					Duration:   time.Minute,
					KeyID:      keyID,
					PrivateKey: privateKey,
				})
				return token
			},
			expectErr: true,
		},
		{
			name: "malformed token",
			creatTokenFunc: func() string {
				return "thisisnot.valid.jwt"
			},
			expectErr: true,
		},
		{
			name: "no groups claim",
			creatTokenFunc: func() string {
				token, _ := jwtutil.CreateToken(jwtutil.CreateTokenOptions{
					Username:   "bob",
					Duration:   time.Minute,
					KeyID:      keyID,
					PrivateKey: privateKey,
				})
				return token
			},
			want: jwtutil.UserInfo{
				Username: "bob",
				Groups:   []string{},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jwtutil.ExtractUserInfo(tt.creatTokenFunc())
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}
