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

	"golang.org/x/crypto/bcrypt"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// BasicProvider implements basic username/password authentication.
// This is intended for local development only.
type BasicProvider struct {
	users map[string]basicUser
}

type basicUser struct {
	passwordHash string
	role         models.Role
}

// NewBasicProvider creates a new basic auth provider from config.
func NewBasicProvider(cfg *config.BasicAuthConfig) *BasicProvider {
	users := make(map[string]basicUser)
	for _, u := range cfg.Users {
		role := models.Role(u.Role)
		if !role.Valid() {
			role = models.RoleUser
		}
		users[u.Username] = basicUser{
			passwordHash: u.PasswordHash,
			role:         role,
		}
	}
	return &BasicProvider{users: users}
}

// Name returns the provider identifier.
func (p *BasicProvider) Name() string {
	return "basic"
}

// DisplayName returns the human-readable provider name.
func (p *BasicProvider) DisplayName() string {
	return "Basic Auth"
}

// Authenticate validates username and password, returning a user on success.
func (p *BasicProvider) Authenticate(ctx context.Context, username, password string) (*models.User, error) {
	user, exists := p.users[username]
	if !exists {
		return nil, fmt.Errorf("invalid username or password")
	}

	// Compare password against bcrypt hash
	if err := bcrypt.CompareHashAndPassword([]byte(user.passwordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid username or password")
	}

	return &models.User{
		ID:       "basic:" + username,
		Login:    username,
		Email:    username + "@local",
		Name:     username,
		Provider: "basic",
		Role:     user.role,
	}, nil
}
