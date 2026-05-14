package mcp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testIssuer is a stand-in for an OIDC provider: it generates a keypair,
// serves the JWKS, and signs JWTs.
type testIssuer struct {
	priv   *rsa.PrivateKey
	kid    string
	server *httptest.Server
	issuer string
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa gen: %v", err)
	}
	kid := "test-key-1"

	pub := priv.Public().(*rsa.PublicKey)
	nB64 := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	eBytes := []byte{byte(pub.E >> 16), byte(pub.E >> 8), byte(pub.E)}
	for len(eBytes) > 1 && eBytes[0] == 0 {
		eBytes = eBytes[1:]
	}
	eB64 := base64.RawURLEncoding.EncodeToString(eBytes)
	jwks, _ := json.Marshal(map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA", "kid": kid, "alg": "RS256", "use": "sig",
			"n": nB64, "e": eB64,
		}},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(srv.Close)

	return &testIssuer{priv: priv, kid: kid, server: srv, issuer: "https://test.example/"}
}

// sign produces a JWT with the given claims signed with this issuer's
// RSA key. alg defaults to RS256; pass "none" or "HS256" to test rejection.
func (i *testIssuer) sign(t *testing.T, claims map[string]any, alg string) string {
	t.Helper()
	if alg == "" {
		alg = "RS256"
	}
	header := map[string]any{"alg": alg, "kid": i.kid, "typ": "JWT"}
	hJSON, _ := json.Marshal(header)
	pJSON, _ := json.Marshal(claims)
	hB := base64.RawURLEncoding.EncodeToString(hJSON)
	pB := base64.RawURLEncoding.EncodeToString(pJSON)
	signingInput := hB + "." + pB

	if alg == "none" {
		return signingInput + "."
	}
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, i.priv, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (i *testIssuer) verifier() *JWTVerifier {
	return &JWTVerifier{
		JWKSURL:  i.server.URL,
		Issuer:   i.issuer,
		Audience: "boardgame-mcp",
	}
}

func validClaims(iss *testIssuer, sub string) map[string]any {
	return map[string]any{
		"iss": iss.issuer,
		"aud": "boardgame-mcp",
		"sub": sub,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
}

func TestJWT_ValidToken(t *testing.T) {
	iss := newTestIssuer(t)
	tok := iss.sign(t, validClaims(iss, "user-123"), "RS256")
	sub, err := iss.verifier().Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if sub != "user-123" {
		t.Errorf("sub = %q, want user-123", sub)
	}
}

func TestJWT_RejectsHS256(t *testing.T) {
	iss := newTestIssuer(t)
	tok := iss.sign(t, validClaims(iss, "user"), "HS256")
	_, err := iss.verifier().Verify(context.Background(), tok)
	if err == nil || !strings.Contains(err.Error(), "unsupported alg") {
		t.Errorf("expected HS256 rejection, got %v", err)
	}
}

func TestJWT_RejectsNoneAlg(t *testing.T) {
	iss := newTestIssuer(t)
	tok := iss.sign(t, validClaims(iss, "user"), "none")
	_, err := iss.verifier().Verify(context.Background(), tok)
	if err == nil || !strings.Contains(err.Error(), "unsigned") {
		t.Errorf("expected unsigned rejection, got %v", err)
	}
}

func TestJWT_RejectsExpired(t *testing.T) {
	iss := newTestIssuer(t)
	claims := validClaims(iss, "user")
	claims["exp"] = time.Now().Add(-time.Hour).Unix()
	tok := iss.sign(t, claims, "RS256")
	_, err := iss.verifier().Verify(context.Background(), tok)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected expired error, got %v", err)
	}
}

func TestJWT_RejectsWrongIssuer(t *testing.T) {
	iss := newTestIssuer(t)
	claims := validClaims(iss, "user")
	claims["iss"] = "https://attacker.example/"
	tok := iss.sign(t, claims, "RS256")
	_, err := iss.verifier().Verify(context.Background(), tok)
	if err == nil || !strings.Contains(err.Error(), "issuer mismatch") {
		t.Errorf("expected issuer mismatch, got %v", err)
	}
}

func TestJWT_RejectsWrongAudience(t *testing.T) {
	iss := newTestIssuer(t)
	claims := validClaims(iss, "user")
	claims["aud"] = "some-other-service"
	tok := iss.sign(t, claims, "RS256")
	_, err := iss.verifier().Verify(context.Background(), tok)
	if err == nil || !strings.Contains(err.Error(), "audience") {
		t.Errorf("expected audience error, got %v", err)
	}
}

func TestJWT_AcceptsAudienceList(t *testing.T) {
	iss := newTestIssuer(t)
	claims := validClaims(iss, "user")
	claims["aud"] = []string{"some-other-service", "boardgame-mcp"}
	tok := iss.sign(t, claims, "RS256")
	if _, err := iss.verifier().Verify(context.Background(), tok); err != nil {
		t.Errorf("expected acceptance of audience array, got %v", err)
	}
}

func TestJWT_RejectsMissingSubject(t *testing.T) {
	iss := newTestIssuer(t)
	claims := validClaims(iss, "")
	delete(claims, "sub")
	tok := iss.sign(t, claims, "RS256")
	_, err := iss.verifier().Verify(context.Background(), tok)
	if err == nil || !strings.Contains(err.Error(), "sub") {
		t.Errorf("expected missing-sub error, got %v", err)
	}
}

func TestJWT_RejectsTamperedSignature(t *testing.T) {
	iss := newTestIssuer(t)
	tok := iss.sign(t, validClaims(iss, "user"), "RS256")
	// Tamper with the last char of the signature.
	tampered := tok[:len(tok)-1] + "A"
	if tampered == tok {
		tampered = tok[:len(tok)-1] + "B"
	}
	_, err := iss.verifier().Verify(context.Background(), tampered)
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Errorf("expected signature error, got %v", err)
	}
}

func TestAuthMiddleware_PassesThroughOnValidToken(t *testing.T) {
	iss := newTestIssuer(t)
	tok := iss.sign(t, validClaims(iss, "user-7"), "RS256")

	var seenUser string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUser = UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	mw := AuthMiddleware(iss.verifier())(next)
	ts := httptest.NewServer(mw)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if seenUser != "user-7" {
		t.Errorf("middleware did not attach userID; saw %q", seenUser)
	}
}

func TestAuthMiddleware_RejectsMissingBearer(t *testing.T) {
	iss := newTestIssuer(t)
	mw := AuthMiddleware(iss.verifier())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("next handler should not be invoked")
	}))
	ts := httptest.NewServer(mw)
	defer ts.Close()
	resp, _ := http.Get(ts.URL)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestNoVerifier_ReturnsConfiguredSubject(t *testing.T) {
	v := NoVerifier{Subject: "local"}
	sub, err := v.Verify(context.Background(), "any token")
	if err != nil {
		t.Fatal(err)
	}
	if sub != "local" {
		t.Errorf("sub = %q, want local", sub)
	}
}
