// Copyright 2026 Lemuria Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/org/lemuria/internal/config"
)

// mockOIDCServer creates a test server that mimics an OIDC provider.
func mockOIDCServer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	mux := http.NewServeMux()

	// Discovery endpoint
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		issuer := "http://" + r.Host
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/jwks",
			"userinfo_endpoint":                     issuer + "/userinfo",
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
		})
	})

	// JWKS endpoint
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		n := privateKey.N
		e := privateKey.E

		// Encode modulus
		nBytes := n.Bytes()
		nB64 := base64.RawURLEncoding.EncodeToString(nBytes)

		// Encode exponent
		eBytes := big.NewInt(int64(e)).Bytes()
		eB64 := base64.RawURLEncoding.EncodeToString(eBytes)

		jwks := map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA",
					"kid": "test-key-1",
					"alg": "RS256",
					"use": "sig",
					"n":   nB64,
					"e":   eB64,
				},
			},
		}
		_ = json.NewEncoder(w).Encode(jwks)
	})

	// Token endpoint
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		issuer := "http://" + r.Host

		signer, err := jose.NewSigner(jose.SigningKey{
			Algorithm: jose.RS256,
			Key:       privateKey,
		}, (&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), "test-key-1"))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		now := time.Now()
		claims := jwt.Claims{
			Issuer:   issuer,
			Subject:  "oidc-user-123",
			Audience: jwt.Audience{"test-client-id"},
			IssuedAt: jwt.NewNumericDate(now),
			Expiry:   jwt.NewNumericDate(now.Add(time.Hour)),
		}
		extraClaims := map[string]any{
			"preferred_username": "oidcuser",
			"email":              "oidc@example.com",
			"name":               "OIDC User",
			"picture":            "https://example.com/avatar.png",
			"groups":             []string{"admin", "developers"},
		}

		builder := jwt.Signed(signer).Claims(claims).Claims(extraClaims)
		rawToken, err := builder.Serialize()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"id_token":     rawToken,
		})
	})

	server := httptest.NewServer(mux)
	return server, privateKey
}

func TestNewOIDCProvider(t *testing.T) {
	server, _ := mockOIDCServer(t)
	defer server.Close()

	cfg := &config.OIDCConfig{
		Name:           "Test SSO",
		IssuerURL:      server.URL,
		ClientID:       "test-client-id",
		ClientSecret:   "test-client-secret",
		UsernameClaim:  "preferred_username",
		EmailClaim:     "email",
		GroupsClaim:    "groups",
		AllowedDomains: []string{"example.com"},
	}

	provider, err := NewOIDCProvider(context.Background(), cfg, "https://app.example.com")
	if err != nil {
		t.Fatalf("NewOIDCProvider() error: %v", err)
	}
	if provider == nil {
		t.Fatal("NewOIDCProvider() returned nil")
	}
	if provider.Name() != "oidc" {
		t.Errorf("Name() = %q, want %q", provider.Name(), "oidc")
	}
	if provider.DisplayName() != "Test SSO" {
		t.Errorf("DisplayName() = %q, want %q", provider.DisplayName(), "Test SSO")
	}
	if provider.usernameClaim != "preferred_username" {
		t.Errorf("usernameClaim = %q, want %q", provider.usernameClaim, "preferred_username")
	}
	if provider.emailClaim != "email" {
		t.Errorf("emailClaim = %q, want %q", provider.emailClaim, "email")
	}
	if provider.groupsClaim != "groups" {
		t.Errorf("groupsClaim = %q, want %q", provider.groupsClaim, "groups")
	}

	// AuthURL should work
	authURL := provider.AuthURL("test-state")
	if authURL == "" {
		t.Error("AuthURL() returned empty string")
	}
	if !strings.Contains(authURL, "test-state") {
		t.Errorf("AuthURL() = %q, expected to contain state", authURL)
	}
}

func TestNewOIDCProvider_Defaults(t *testing.T) {
	server, _ := mockOIDCServer(t)
	defer server.Close()

	cfg := &config.OIDCConfig{
		IssuerURL:    server.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}

	provider, err := NewOIDCProvider(context.Background(), cfg, "https://app.example.com")
	if err != nil {
		t.Fatalf("NewOIDCProvider() error: %v", err)
	}
	if provider.usernameClaim != "preferred_username" {
		t.Errorf("default usernameClaim = %q, want %q", provider.usernameClaim, "preferred_username")
	}
	if provider.emailClaim != "email" {
		t.Errorf("default emailClaim = %q, want %q", provider.emailClaim, "email")
	}
	if provider.DisplayName() != "SSO" {
		t.Errorf("default DisplayName() = %q, want %q", provider.DisplayName(), "SSO")
	}
}

func TestNewOIDCProvider_InvalidIssuer(t *testing.T) {
	cfg := &config.OIDCConfig{
		IssuerURL:    "https://nonexistent.invalid.example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
	}

	_, err := NewOIDCProvider(context.Background(), cfg, "https://app.example.com")
	if err == nil {
		t.Error("NewOIDCProvider() should return error for invalid issuer")
	}
}

func TestOIDCProvider_Exchange(t *testing.T) {
	server, _ := mockOIDCServer(t)
	defer server.Close()

	cfg := &config.OIDCConfig{
		IssuerURL:    server.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		GroupsClaim:  "groups",
	}

	provider, err := NewOIDCProvider(context.Background(), cfg, "https://app.example.com")
	if err != nil {
		t.Fatalf("NewOIDCProvider() error: %v", err)
	}

	// Override the token endpoint to point to our mock server
	provider.config.Endpoint.TokenURL = server.URL + "/token"

	user, err := provider.Exchange(context.Background(), "test-auth-code")
	if err != nil {
		t.Fatalf("Exchange() error: %v", err)
	}
	if user == nil {
		t.Fatal("Exchange() returned nil user")
	}
	if user.ID != "oidc:oidc-user-123" {
		t.Errorf("ID = %q, want %q", user.ID, "oidc:oidc-user-123")
	}
	if user.Login != "oidcuser" {
		t.Errorf("Login = %q, want %q", user.Login, "oidcuser")
	}
	if user.Email != "oidc@example.com" {
		t.Errorf("Email = %q, want %q", user.Email, "oidc@example.com")
	}
	if user.Name != "OIDC User" {
		t.Errorf("Name = %q, want %q", user.Name, "OIDC User")
	}
	if user.Provider != "oidc" {
		t.Errorf("Provider = %q, want %q", user.Provider, "oidc")
	}
}

func TestOIDCProvider_Exchange_DomainRestriction(t *testing.T) {
	server, _ := mockOIDCServer(t)
	defer server.Close()

	cfg := &config.OIDCConfig{
		IssuerURL:      server.URL,
		ClientID:       "test-client-id",
		ClientSecret:   "test-client-secret",
		AllowedDomains: []string{"restricted.com"},
	}

	provider, err := NewOIDCProvider(context.Background(), cfg, "https://app.example.com")
	if err != nil {
		t.Fatalf("NewOIDCProvider() error: %v", err)
	}
	provider.config.Endpoint.TokenURL = server.URL + "/token"

	// The mock token returns email "oidc@example.com" which is not in "restricted.com"
	_, err = provider.Exchange(context.Background(), "test-auth-code")
	if err == nil {
		t.Error("Exchange() should fail when email domain is not allowed")
	}
}
