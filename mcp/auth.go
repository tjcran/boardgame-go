package mcp

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TokenVerifier validates a bearer token and returns the verified user
// subject. Hosted mode uses JWTVerifier; tests and single-tenant
// deployments may use NoVerifier.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (subject string, err error)
}

// NoVerifier returns Subject for every request — single-tenant mode.
type NoVerifier struct{ Subject string }

// Verify implements TokenVerifier.
func (n NoVerifier) Verify(_ context.Context, _ string) (string, error) {
	if n.Subject == "" {
		return "anonymous", nil
	}
	return n.Subject, nil
}

// JWTVerifier verifies bearer JWTs against a JWKS endpoint.
//
// Only RS256 and ES256 are accepted. HS-family (HMAC) algorithms are
// rejected unconditionally: JWKS keys are public, and accepting a
// symmetric algorithm against a public key is a classic key-confusion
// vulnerability. "none" is rejected too.
type JWTVerifier struct {
	// JWKSURL is the well-known JWKS endpoint of the issuer.
	JWKSURL string
	// Issuer, if non-empty, must equal the token's iss claim.
	Issuer string
	// Audience, if non-empty, must appear in the token's aud claim.
	Audience string
	// HTTPClient overrides http.DefaultClient (10s timeout default).
	HTTPClient *http.Client
	// Skew is the allowed clock skew on exp/nbf (default 30s).
	Skew time.Duration
	// CacheTTL is how long parsed JWKS keys live before refresh (default 15m).
	CacheTTL time.Duration
	// Now overrides time.Now for tests.
	Now func() time.Time

	mu        sync.Mutex
	keys      map[string]any
	fetchedAt time.Time
	lastFetch time.Time // for refresh rate-limiting
}

func (v *JWTVerifier) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// Verify implements TokenVerifier. Returns the sub claim on success.
func (v *JWTVerifier) Verify(ctx context.Context, token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("invalid jwt: want 3 segments")
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decode header: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return "", fmt.Errorf("parse header: %w", err)
	}
	switch header.Alg {
	case "RS256", "ES256":
		// allowed
	case "", "none":
		return "", errors.New("unsigned tokens not accepted")
	default:
		// HS256/HS384/HS512 caught here too — never accept symmetric
		// algorithms when the verifier only knows public keys.
		return "", fmt.Errorf("unsupported alg %q (allowed: RS256, ES256)", header.Alg)
	}
	if header.Kid == "" {
		return "", errors.New("token missing kid in header")
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}
	var claims struct {
		Iss string  `json:"iss"`
		Sub string  `json:"sub"`
		Aud audClaim `json:"aud"`
		Exp int64   `json:"exp"`
		Nbf int64   `json:"nbf"`
	}
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return "", fmt.Errorf("parse claims: %w", err)
	}

	if v.Issuer != "" && claims.Iss != v.Issuer {
		return "", fmt.Errorf("issuer mismatch: got %q, want %q", claims.Iss, v.Issuer)
	}
	if v.Audience != "" && !claims.Aud.contains(v.Audience) {
		return "", fmt.Errorf("audience %q not in token", v.Audience)
	}

	now := v.now()
	skew := v.Skew
	if skew == 0 {
		skew = 30 * time.Second
	}
	if claims.Exp != 0 && now.After(time.Unix(claims.Exp, 0).Add(skew)) {
		return "", errors.New("token expired")
	}
	if claims.Nbf != 0 && now.Add(skew).Before(time.Unix(claims.Nbf, 0)) {
		return "", errors.New("token not yet valid")
	}
	if claims.Sub == "" {
		return "", errors.New("token missing sub")
	}

	key, err := v.lookupKey(ctx, header.Kid)
	if err != nil {
		return "", err
	}
	signingInput := []byte(parts[0] + "." + parts[1])
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("decode signature: %w", err)
	}
	if err := verifySignature(header.Alg, key, signingInput, sig); err != nil {
		return "", fmt.Errorf("signature verification: %w", err)
	}
	return claims.Sub, nil
}

// audClaim accepts either a string or a []string in the aud claim
// (RFC 7519 §4.1.3 allows both shapes).
type audClaim []string

func (a *audClaim) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*a = []string{s}
		return nil
	}
	var ss []string
	if err := json.Unmarshal(data, &ss); err == nil {
		*a = ss
		return nil
	}
	if string(data) == "null" {
		return nil
	}
	return errors.New("aud claim is neither string nor []string")
}

func (a audClaim) contains(target string) bool {
	for _, v := range a {
		if v == target {
			return true
		}
	}
	return false
}

func verifySignature(alg string, key any, signingInput, sig []byte) error {
	hash := sha256.Sum256(signingInput)
	switch alg {
	case "RS256":
		rk, ok := key.(*rsa.PublicKey)
		if !ok {
			return errors.New("key kid claimed RSA but JWKS entry isn't")
		}
		return rsa.VerifyPKCS1v15(rk, crypto.SHA256, hash[:], sig)
	case "ES256":
		ek, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return errors.New("key kid claimed EC but JWKS entry isn't")
		}
		if len(sig) != 64 {
			return fmt.Errorf("ES256 signature length = %d, want 64", len(sig))
		}
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		if !ecdsa.Verify(ek, hash[:], r, s) {
			return errors.New("ECDSA verification failed")
		}
		return nil
	}
	return fmt.Errorf("unsupported alg %q", alg)
}

func (v *JWTVerifier) lookupKey(ctx context.Context, kid string) (any, error) {
	v.mu.Lock()
	key, ok := v.keys[kid]
	cacheStale := v.fetchedAt.IsZero() || v.now().Sub(v.fetchedAt) > v.cacheTTL()
	v.mu.Unlock()

	if ok && !cacheStale {
		return key, nil
	}

	// Unknown kid or stale cache — refresh, but rate-limit to once per minute.
	if err := v.refreshJWKS(ctx); err != nil {
		if ok {
			// Serve stale rather than fail an otherwise-valid token if the
			// JWKS endpoint is briefly unreachable.
			return key, nil
		}
		return nil, fmt.Errorf("jwks refresh: %w", err)
	}

	v.mu.Lock()
	key, ok = v.keys[kid]
	v.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown kid %q after refresh", kid)
	}
	return key, nil
}

func (v *JWTVerifier) cacheTTL() time.Duration {
	if v.CacheTTL == 0 {
		return 15 * time.Minute
	}
	return v.CacheTTL
}

func (v *JWTVerifier) refreshJWKS(ctx context.Context) error {
	v.mu.Lock()
	if !v.lastFetch.IsZero() && v.now().Sub(v.lastFetch) < time.Minute {
		v.mu.Unlock()
		return errors.New("jwks refresh rate-limited (last fetch <1m ago)")
	}
	v.lastFetch = v.now()
	v.mu.Unlock()

	hc := v.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.JWKSURL, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}
	parsed := map[string]any{}
	for _, raw := range doc.Keys {
		var k jwk
		if err := json.Unmarshal(raw, &k); err != nil {
			continue
		}
		pk, err := k.toPublicKey()
		if err != nil || pk == nil {
			continue
		}
		parsed[k.Kid] = pk
	}
	if len(parsed) == 0 {
		return errors.New("jwks contained no usable keys")
	}

	v.mu.Lock()
	v.keys = parsed
	v.fetchedAt = v.now()
	v.mu.Unlock()
	return nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	// RSA fields
	N string `json:"n"`
	E string `json:"e"`
	// EC fields
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (k jwk) toPublicKey() (any, error) {
	if k.Use != "" && k.Use != "sig" {
		return nil, nil // skip encryption keys silently
	}
	switch k.Kty {
	case "RSA":
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, fmt.Errorf("rsa n: %w", err)
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, fmt.Errorf("rsa e: %w", err)
		}
		e := 0
		for _, b := range eBytes {
			e = e<<8 + int(b)
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, nil
	case "EC":
		if k.Crv != "P-256" {
			return nil, fmt.Errorf("unsupported curve %q (only P-256)", k.Crv)
		}
		xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			return nil, fmt.Errorf("ec x: %w", err)
		}
		yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil {
			return nil, fmt.Errorf("ec y: %w", err)
		}
		return &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(xBytes),
			Y:     new(big.Int).SetBytes(yBytes),
		}, nil
	}
	return nil, fmt.Errorf("unsupported kty %q", k.Kty)
}

// AuthMiddleware extracts a Bearer token from Authorization, verifies it
// via the supplied TokenVerifier, and attaches the verified subject to
// the request context as the MCP userID.
//
// Wraps any http.Handler — typically the Server.HTTPHandler.
func AuthMiddleware(v TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(auth, "Bearer ")
			subject, err := v.Verify(r.Context(), token)
			if err != nil {
				http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
				return
			}
			ctx := WithUserID(r.Context(), subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
