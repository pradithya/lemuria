package auth

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/org/lemuria/internal/models"
)

// RoleAssignment defines a rule for assigning roles to users.
type RoleAssignment struct {
	// Pattern is a glob pattern to match against user identifier.
	// For email: "admin@example.com", "*@platform.example.com"
	// For GitHub team: "@org/team-name"
	// For OIDC group: "group-name"
	Pattern string `yaml:"pattern" json:"pattern"`

	// Role is the role to assign if pattern matches.
	Role string `yaml:"role" json:"role"`

	// Provider optionally restricts the match to a specific provider.
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
}

// ConfigRoleResolver resolves roles based on configuration rules.
type ConfigRoleResolver struct {
	assignments []RoleAssignment
	defaultRole models.Role
	store       *RedisSessionStore
}

// NewConfigRoleResolver creates a new config-based role resolver.
func NewConfigRoleResolver(assignments []RoleAssignment, defaultRole models.Role, store *RedisSessionStore) *ConfigRoleResolver {
	if !defaultRole.Valid() {
		defaultRole = models.RoleUser
	}
	return &ConfigRoleResolver{
		assignments: assignments,
		defaultRole: defaultRole,
		store:       store,
	}
}

// ResolveRole returns the role for a user based on configured rules.
func (r *ConfigRoleResolver) ResolveRole(ctx context.Context, user *models.User) models.Role {
	// Check for persistent role override first
	if r.store != nil {
		if role, found, err := r.store.GetUserRole(ctx, user.ID); err == nil && found {
			if role.Valid() {
				return role
			}
		}
	}

	// Check assignments in order
	for _, assignment := range r.assignments {
		if r.matchesAssignment(user, assignment) {
			role := models.Role(assignment.Role)
			if role.Valid() {
				return role
			}
		}
	}

	return r.defaultRole
}

// matchesAssignment checks if a user matches an assignment rule.
func (r *ConfigRoleResolver) matchesAssignment(user *models.User, assignment RoleAssignment) bool {
	// Check provider filter
	if assignment.Provider != "" && assignment.Provider != user.Provider {
		return false
	}

	pattern := assignment.Pattern

	// Check for GitHub team pattern (@org/team)
	if strings.HasPrefix(pattern, "@") {
		return r.matchesGitHubTeam(user, pattern)
	}

	// Check for group pattern (no @ or *)
	if !strings.Contains(pattern, "@") && !strings.Contains(pattern, "*") {
		return r.matchesGroup(user, pattern)
	}

	// Email pattern matching
	return r.matchesEmail(user, pattern)
}

// matchesEmail checks if user email matches the pattern.
func (r *ConfigRoleResolver) matchesEmail(user *models.User, pattern string) bool {
	if user.Email == "" {
		return false
	}

	// Exact match
	if pattern == user.Email {
		return true
	}

	// Wildcard match (e.g., "*@example.com")
	matched, err := filepath.Match(pattern, user.Email)
	if err != nil {
		return false
	}
	return matched
}

// matchesGitHubTeam checks if user is a member of the GitHub team.
func (r *ConfigRoleResolver) matchesGitHubTeam(user *models.User, pattern string) bool {
	if user.Provider != "github" {
		return false
	}

	// Pattern format: @org/team-name
	// User groups should contain "org/team-name"
	teamPath := strings.TrimPrefix(pattern, "@")
	for _, group := range user.Groups {
		if group == teamPath {
			return true
		}
	}

	return false
}

// matchesGroup checks if user is a member of an OIDC group.
func (r *ConfigRoleResolver) matchesGroup(user *models.User, groupName string) bool {
	for _, group := range user.Groups {
		if group == groupName {
			return true
		}
	}
	return false
}

// SetUserRole sets a persistent role override for a user.
func (r *ConfigRoleResolver) SetUserRole(ctx context.Context, userID string, role models.Role) error {
	if r.store == nil {
		return nil
	}
	return r.store.SetUserRole(ctx, userID, role)
}

// GetUserRole gets the persistent role override for a user, if any.
func (r *ConfigRoleResolver) GetUserRole(ctx context.Context, userID string) (models.Role, bool, error) {
	if r.store == nil {
		return "", false, nil
	}
	return r.store.GetUserRole(ctx, userID)
}

// DeleteUserRole removes a user role override.
func (r *ConfigRoleResolver) DeleteUserRole(ctx context.Context, userID string) error {
	if r.store == nil {
		return nil
	}
	return r.store.DeleteUserRole(ctx, userID)
}
