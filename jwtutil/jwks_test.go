package jwtutil

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jwksBody renders the JWKS document authn would serve for the given keys.
func jwksBody(t *testing.T, keys map[string]*rsa.PublicKey) string {
	t.Helper()

	doc := jwksDocument{}
	for kid, pub := range keys {
		doc.Keys = append(doc.Keys, jwksKey{
			Kty: "RSA",
			Use: "sig",
			Alg: "RS256",
			Kid: kid,
			N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		})
	}

	body, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(body)
}

func newToken(t *testing.T, key *rsa.PrivateKey, kid string, d time.Duration) string {
	t.Helper()

	token, err := CreateToken(CreateTokenOptions{
		Username:   "alice",
		Groups:     []string{"admins"},
		Duration:   d,
		KeyID:      kid,
		PrivateKey: key,
	})
	require.NoError(t, err)
	return token
}

// TestJWKSKeySourceValidatesRealToken is the end-to-end check: a token signed
// the way authn signs it must validate against a key fetched from a JWKS
// endpoint, exercising the n/e round trip and the kid lookup.
func TestJWKSKeySourceValidatesRealToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	const kid = "krateo-authn-key-1"
	body := jwksBody(t, map[string]*rsa.PublicKey{kid: &key.PublicKey})

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		assert.Equal(t, DefaultJWKSPath, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	src := NewJWKSKeySource(JWKSURL(srv.URL))

	// Nothing is fetched at construction.
	assert.Equal(t, int64(0), atomic.LoadInt64(&hits))

	info, err := ValidateWithKeySource(src, newToken(t, key, kid, time.Minute))
	require.NoError(t, err)
	assert.Equal(t, "alice", info.Username)
	assert.ElementsMatch(t, []string{"admins"}, info.Groups)
	assert.Equal(t, int64(1), atomic.LoadInt64(&hits), "first validation triggers the fetch")

	// Second validation is served from cache.
	_, err = ValidateWithKeySource(src, newToken(t, key, kid, time.Minute))
	require.NoError(t, err)
	assert.Equal(t, int64(1), atomic.LoadInt64(&hits), "cached key set must not refetch")
}

// TestJWKSKeySourceRefetchesAfterTTL pins the cache-expiry behaviour the chart's
// TTL value controls.
func TestJWKSKeySourceRefetchesAfterTTL(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	const kid = "kid-1"
	body := jwksBody(t, map[string]*rsa.PublicKey{kid: &key.PublicKey})

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	now := time.Now()
	src := NewJWKSKeySource(JWKSURL(srv.URL),
		WithJWKSCacheTTL(time.Minute),
		WithJWKSMinRefreshInterval(time.Second))
	src.now = func() time.Time { return now }

	_, err = src.PublicKey(kid)
	require.NoError(t, err)
	assert.Equal(t, int64(1), atomic.LoadInt64(&hits))

	// Inside the TTL: no refetch.
	now = now.Add(30 * time.Second)
	_, err = src.PublicKey(kid)
	require.NoError(t, err)
	assert.Equal(t, int64(1), atomic.LoadInt64(&hits))

	// Past the TTL: refetch.
	now = now.Add(31 * time.Second)
	_, err = src.PublicKey(kid)
	require.NoError(t, err)
	assert.Equal(t, int64(2), atomic.LoadInt64(&hits))
}

// TestJWKSKeySourceUnknownKidIsThrottled guards the stampede case: a flood of
// tokens naming a kid authn never published must not become a fetch per request.
func TestJWKSKeySourceUnknownKidIsThrottled(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	body := jwksBody(t, map[string]*rsa.PublicKey{"known": &key.PublicKey})

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	now := time.Now()
	src := NewJWKSKeySource(JWKSURL(srv.URL), WithJWKSMinRefreshInterval(30*time.Second))
	src.now = func() time.Time { return now }

	for i := 0; i < 20; i++ {
		_, err := src.PublicKey("rotated-away")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrKeyUnavailable)
	}
	assert.Equal(t, int64(1), atomic.LoadInt64(&hits), "unknown kid must not refetch per request")

	// Past the refresh floor, exactly one more attempt is allowed.
	now = now.Add(31 * time.Second)
	_, err = src.PublicKey("rotated-away")
	require.Error(t, err)
	assert.Equal(t, int64(2), atomic.LoadInt64(&hits))
}

// TestJWKSKeySourcePicksUpRotatedKid covers rotation: a kid absent from the
// cache triggers a refetch that discovers it.
func TestJWKSKeySourcePicksUpRotatedKid(t *testing.T) {
	oldKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	newKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	var mu sync.Mutex
	body := jwksBody(t, map[string]*rsa.PublicKey{"old": &oldKey.PublicKey})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	src := NewJWKSKeySource(JWKSURL(srv.URL), WithJWKSMinRefreshInterval(time.Nanosecond))

	_, err = ValidateWithKeySource(src, newToken(t, oldKey, "old", time.Minute))
	require.NoError(t, err)

	// authn rotates its keypair.
	mu.Lock()
	body = jwksBody(t, map[string]*rsa.PublicKey{"new": &newKey.PublicKey})
	mu.Unlock()

	info, err := ValidateWithKeySource(src, newToken(t, newKey, "new", time.Minute))
	require.NoError(t, err, "an unseen kid must trigger a refetch")
	assert.Equal(t, "alice", info.Username)
}

// TestJWKSKeySourceServesStaleKeyWhenEndpointDown is the availability
// guarantee: an authn blip must not invalidate tokens we could already verify.
func TestJWKSKeySourceServesStaleKeyWhenEndpointDown(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	const kid = "kid-1"
	body := jwksBody(t, map[string]*rsa.PublicKey{kid: &key.PublicKey})

	var down atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if down.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	now := time.Now()
	src := NewJWKSKeySource(JWKSURL(srv.URL),
		WithJWKSCacheTTL(time.Minute),
		WithJWKSMinRefreshInterval(time.Nanosecond))
	src.now = func() time.Time { return now }

	_, err = src.PublicKey(kid)
	require.NoError(t, err)

	// authn goes down and the cache goes stale.
	down.Store(true)
	now = now.Add(2 * time.Minute)

	got, err := src.PublicKey(kid)
	require.NoError(t, err, "a stale-but-known key must still be served")
	assert.Equal(t, &key.PublicKey, got)
}

// TestJWKSKeySourceUnreachableIsKeyUnavailable ensures a cold cache plus a dead
// endpoint is reported as our fault (503-worthy), not as an invalid token.
func TestJWKSKeySourceUnreachableIsKeyUnavailable(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	srv.Close() // nothing is listening

	src := NewJWKSKeySource(JWKSURL(srv.URL))

	_, err = ValidateWithKeySource(src, newToken(t, key, "kid-1", time.Minute))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyUnavailable)
	assert.NotErrorIs(t, err, ErrTokenInvalid,
		"an unreachable JWKS must not be reported as a bad token")
}

// TestJWKSKeySourceRejectsWrongKey confirms a genuinely bad signature is still
// ErrTokenInvalid even though the key now arrives over the network.
func TestJWKSKeySourceRejectsWrongKey(t *testing.T) {
	served, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	attacker, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	const kid = "kid-1"
	body := jwksBody(t, map[string]*rsa.PublicKey{kid: &served.PublicKey})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	src := NewJWKSKeySource(JWKSURL(srv.URL))

	_, err = ValidateWithKeySource(src, newToken(t, attacker, kid, time.Minute))
	assert.ErrorIs(t, err, ErrTokenInvalid)
}

// TestJWKSKeySourceExpiredTokenStillExpired keeps the expiry signal distinct
// from both invalid-token and key-unavailable.
func TestJWKSKeySourceExpiredTokenStillExpired(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	const kid = "kid-1"
	body := jwksBody(t, map[string]*rsa.PublicKey{kid: &key.PublicKey})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	src := NewJWKSKeySource(JWKSURL(srv.URL))

	_, err = ValidateWithKeySource(src, newToken(t, key, kid, -time.Hour))
	assert.ErrorIs(t, err, ErrTokenExpired)
}

// TestJWKSKeySourceEmptyKidResolvesSingleKey covers the no-kid token against a
// single-key set.
func TestJWKSKeySourceEmptyKidResolvesSingleKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	body := jwksBody(t, map[string]*rsa.PublicKey{"only": &key.PublicKey})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	src := NewJWKSKeySource(JWKSURL(srv.URL))

	got, err := src.PublicKey("")
	require.NoError(t, err)
	assert.Equal(t, &key.PublicKey, got)
}

// TestJWKSKeySourceSingleFlightsConcurrentMisses documents that a burst of
// concurrent cold-cache validations collapses into one fetch.
func TestJWKSKeySourceSingleFlightsConcurrentMisses(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	const kid = "kid-1"
	body := jwksBody(t, map[string]*rsa.PublicKey{kid: &key.PublicKey})

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	src := NewJWKSKeySource(JWKSURL(srv.URL))

	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := src.PublicKey(kid)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(1), atomic.LoadInt64(&hits))
}

// TestJWKSKeySourceSkipsUnusableKeys checks that non-RS256 / non-RSA entries are
// ignored rather than poisoning the whole set.
func TestJWKSKeySourceSkipsUnusableKeys(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	doc := jwksDocument{Keys: []jwksKey{
		{Kty: "EC", Kid: "ec-key", Use: "sig", N: "x", E: "AQAB"},
		{Kty: "RSA", Kid: "enc-key", Use: "enc", N: "x", E: "AQAB"},
		{
			Kty: "RSA", Use: "sig", Alg: "RS256", Kid: "good",
			N: base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		},
	}}
	raw, err := json.Marshal(doc)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(raw)
	}))
	defer srv.Close()

	src := NewJWKSKeySource(JWKSURL(srv.URL))

	got, err := src.PublicKey("good")
	require.NoError(t, err)
	assert.Equal(t, &key.PublicKey, got)

	_, err = src.PublicKey("ec-key")
	assert.ErrorIs(t, err, ErrKeyUnavailable)
}

// TestJWKSKeySourceEmptySetIsError ensures an empty/garbage document does not
// install an empty cache that silently rejects everything as "invalid token".
func TestJWKSKeySourceEmptySetIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"keys":[]}`)
	}))
	defer srv.Close()

	src := NewJWKSKeySource(JWKSURL(srv.URL))

	_, err := src.PublicKey("kid-1")
	assert.ErrorIs(t, err, ErrKeyUnavailable)
}

func TestJWKSURL(t *testing.T) {
	assert.Equal(t, "http://authn:8082"+DefaultJWKSPath, JWKSURL("http://authn:8082"))
	assert.Equal(t, "http://authn:8082"+DefaultJWKSPath, JWKSURL("http://authn:8082/"))
	assert.Equal(t, "http://authn:8082"+DefaultJWKSPath, JWKSURL("  http://authn:8082  "))
	assert.Equal(t, "https://x/custom.json", JWKSURL("https://x/custom.json"))
	assert.Equal(t, "", JWKSURL(""))
}

// TestValidateStillAcceptsStaticKey pins backwards compatibility of the
// single-key Validate form.
func TestValidateStillAcceptsStaticKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	info, err := Validate(&key.PublicKey, newToken(t, key, "kid-1", time.Minute))
	require.NoError(t, err)
	assert.Equal(t, "alice", info.Username)
}

func TestDecodeBase64URLAcceptsPadding(t *testing.T) {
	raw := []byte{0x01, 0x02, 0x03, 0x04, 0x05}

	unpadded, err := decodeBase64URL(base64.RawURLEncoding.EncodeToString(raw))
	require.NoError(t, err)
	assert.Equal(t, raw, unpadded)

	padded, err := decodeBase64URL(base64.URLEncoding.EncodeToString(raw))
	require.NoError(t, err)
	assert.Equal(t, raw, padded)
}
