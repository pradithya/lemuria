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
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// OIDCProvider implements authentication via a generic OIDC provider.
type OIDCProvider struct {
	name           string
	provider       *oidc.Provider
	config         *oauth2.Config
	verifier       *oidc.IDTokenVerifier
	usernameClaim  string
	emailClaim     string
	groupsClaim    string
	allowedDomains []string
}

// NewOIDCProvider creates a new OIDC provider.
func NewOIDCProvider(ctx context.Context, cfg *config.OIDCConfig, baseURL string) (*OIDCProvider, error) {
	// Discover OIDC provider
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discovering OIDC provider: %w", err)
	}

	// Set default scopes
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	// Create OAuth2 config
	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  baseURL + "/auth/oidc/callback",
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}

	// Create ID token verifier
	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	// Set default claim names
	usernameClaim := cfg.UsernameClaim
	if usernameClaim == "" {
		usernameClaim = "preferred_username"
	}

	emailClaim := cfg.EmailClaim
	if emailClaim == "" {
		emailClaim = "email"
	}

	name := cfg.Name
	if name == "" {
		name = "SSO"
	}

	return &OIDCProvider{
		name:           name,
		provider:       provider,
		config:         oauth2Config,
		verifier:       verifier,
		usernameClaim:  usernameClaim,
		emailClaim:     emailClaim,
		groupsClaim:    cfg.GroupsClaim,
		allowedDomains: cfg.AllowedDomains,
	}, nil
}

// Name returns the provider identifier.
func (p *OIDCProvider) Name() string {
	return "oidc"
}

// DisplayName returns the human-readable name for UI.
func (p *OIDCProvider) DisplayName() string {
	return p.name
}

// AuthURL returns the URL to redirect users for authentication.
func (p *OIDCProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state)
}

// Exchange exchanges the authorization code for user information.
func (p *OIDCProvider) Exchange(ctx context.Context, code string) (*models.User, error) {
	// Exchange code for token
	token, err := p.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchanging code: %w", err)
	}

	// Extract ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	// Verify ID token
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verifying ID token: %w", err)
	}

	// Extract claims
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("extracting claims: %w", err)
	}

	// Build user from claims
	user := p.buildUser(idToken.Subject, claims)

	// Validate email domain if restrictions are configured
	if len(p.allowedDomains) > 0 {
		if user.Email == "" {
			return nil, fmt.Errorf("email is required but not provided by identity provider")
		}
		if !p.isEmailDomainAllowed(user.Email) {
			return nil, fmt.Errorf("email domain not allowed")
		}
	}

	return user, nil
}

// isEmailDomainAllowed checks if the email domain is in the allowed list.
func (p *OIDCProvider) isEmailDomainAllowed(email string) bool {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	domain := strings.ToLower(parts[1])

	for _, allowed := range p.allowedDomains {
		if strings.ToLower(allowed) == domain {
			return true
		}
	}
	return false
}

// buildUser creates a User from OIDC claims.
func (p *OIDCProvider) buildUser(subject string, claims map[string]any) *models.User {
	now := time.Now()
	user := &models.User{
		ID:          "oidc:" + subject,
		Provider:    "oidc",
		CreatedAt:   now,
		LastLoginAt: now,
	}

	// Extract username
	if username, ok := claims[p.usernameClaim].(string); ok {
		user.Login = username
	} else if sub, ok := claims["sub"].(string); ok {
		user.Login = sub
	}

	// Extract email
	if email, ok := claims[p.emailClaim].(string); ok {
		user.Email = email
	}

	// Extract name
	if name, ok := claims["name"].(string); ok {
		user.Name = name
	} else if givenName, ok := claims["given_name"].(string); ok {
		user.Name = givenName
		if familyName, ok := claims["family_name"].(string); ok {
			user.Name += " " + familyName
		}
	}

	// Extract picture/avatar
	if picture, ok := claims["picture"].(string); ok {
		user.AvatarURL = picture
	}

	// Extract groups
	if p.groupsClaim != "" {
		if groups, ok := claims[p.groupsClaim].([]any); ok {
			for _, g := range groups {
				if group, ok := g.(string); ok {
					user.Groups = append(user.Groups, group)
				}
			}
		}
	}

	return user
}
