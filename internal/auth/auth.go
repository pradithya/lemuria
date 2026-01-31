package auth

import (
	"context"
	"net/http"

	"github.com/org/lemuria/internal/models"
)

// Provider defines the interface for authentication providers.
type Provider interface {
	// Name returns the provider identifier (e.g., "github", "oidc").
	Name() string

	// DisplayName returns the human-readable name for UI.
	DisplayName() string

	// AuthURL returns the URL to redirect users for authentication.
	AuthURL(state string) string

	// Exchange exchanges the authorization code for user information.
	Exchange(ctx context.Context, code string) (*models.User, error)
}

// SessionStore defines the interface for session storage.
type SessionStore interface {
	// Create creates a new session for the user.
	Create(ctx context.Context, user *models.User) (*models.Session, error)

	// Get retrieves a session by ID.
	Get(ctx context.Context, sessionID string) (*models.Session, error)

	// Delete removes a session.
	Delete(ctx context.Context, sessionID string) error

	// Refresh extends session expiration.
	Refresh(ctx context.Context, sessionID string) error

	// ListAll returns all active sessions (for admin).
	ListAll(ctx context.Context) ([]*models.Session, error)
}

// StateStore defines the interface for OAuth state storage.
type StateStore interface {
	// Create creates a new state for OAuth flow.
	Create(ctx context.Context, redirectURL string) (*models.AuthState, error)

	// Validate validates and consumes an OAuth state.
	Validate(ctx context.Context, state string) (*models.AuthState, error)
}

// RoleResolver determines a user's role based on configuration.
type RoleResolver interface {
	// ResolveRole returns the role for a user based on configured rules.
	ResolveRole(user *models.User) models.Role

	// SetUserRole sets a persistent role override for a user.
	SetUserRole(ctx context.Context, userID string, role models.Role) error

	// GetUserRole gets the persistent role override for a user, if any.
	GetUserRole(ctx context.Context, userID string) (models.Role, bool, error)
}

// ContextKey type for context values.
type ContextKey string

const (
	// UserContextKey is the context key for the authenticated user.
	UserContextKey ContextKey = "user"
	// SessionContextKey is the context key for the session.
	SessionContextKey ContextKey = "session"
)

// UserFromContext extracts the user from the request context.
func UserFromContext(ctx context.Context) *models.User {
	user, _ := ctx.Value(UserContextKey).(*models.User)
	return user
}

// SessionFromContext extracts the session from the request context.
func SessionFromContext(ctx context.Context) *models.Session {
	session, _ := ctx.Value(SessionContextKey).(*models.Session)
	return session
}

// UserFromRequest extracts the user from the HTTP request context.
func UserFromRequest(r *http.Request) *models.User {
	return UserFromContext(r.Context())
}

// SessionFromRequest extracts the session from the HTTP request context.
func SessionFromRequest(r *http.Request) *models.Session {
	return SessionFromContext(r.Context())
}

// WithUser adds a user to the context.
func WithUser(ctx context.Context, user *models.User) context.Context {
	return context.WithValue(ctx, UserContextKey, user)
}

// WithSession adds a session to the context.
func WithSession(ctx context.Context, session *models.Session) context.Context {
	return context.WithValue(ctx, SessionContextKey, session)
}

// IsAuthenticated returns true if the request has an authenticated user.
func IsAuthenticated(r *http.Request) bool {
	return UserFromRequest(r) != nil
}

// IsAdmin returns true if the request user is an admin.
func IsAdmin(r *http.Request) bool {
	user := UserFromRequest(r)
	return user != nil && user.Role.IsAdmin()
}

// HasPermission returns true if the request user has the specified permission.
func HasPermission(r *http.Request, permission string) bool {
	user := UserFromRequest(r)
	return user != nil && user.Role.HasPermission(permission)
}
