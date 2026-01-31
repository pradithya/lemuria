package models

import "time"

// Role represents a user's access level.
type Role string

const (
	// RoleAdmin has full access: read, write, delete, admin.
	RoleAdmin Role = "admin"
	// RoleUser has read-only access.
	RoleUser Role = "user"
)

// User represents an authenticated user.
type User struct {
	ID          string    `json:"id"`
	Login       string    `json:"login"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	Provider    string    `json:"provider"`
	Role        Role      `json:"role"`
	Groups      []string  `json:"groups,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
}

// Session represents an active user session.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	User      *User     `json:"user"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// IsExpired returns true if the session has expired.
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// IsValid returns true if the session is valid (not expired and has a user).
func (s *Session) IsValid() bool {
	return s.User != nil && !s.IsExpired()
}

// HasPermission checks if the user's role has a specific permission.
func (r Role) HasPermission(permission string) bool {
	permissions := map[Role][]string{
		RoleAdmin: {"read", "write", "delete", "admin"},
		RoleUser:  {"read"},
	}
	for _, p := range permissions[r] {
		if p == permission {
			return true
		}
	}
	return false
}

// IsAdmin returns true if the role is admin.
func (r Role) IsAdmin() bool {
	return r == RoleAdmin
}

// Valid returns true if the role is a valid role.
func (r Role) Valid() bool {
	return r == RoleAdmin || r == RoleUser
}

// AuthProvider represents an available authentication provider.
type AuthProvider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LoginURL string `json:"login_url"`
}

// AuthState holds OAuth state for CSRF protection.
type AuthState struct {
	State       string    `json:"state"`
	RedirectURL string    `json:"redirect_url"`
	CreatedAt   time.Time `json:"created_at"`
}
