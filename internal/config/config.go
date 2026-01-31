package config

import "time"

// Config represents the main Lemuria server configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	GitHub   GitHubConfig   `yaml:"github"`
	ArgoCD   ArgoCDConfig   `yaml:"argocd"`
	Redis    RedisConfig    `yaml:"redis"`
	Defaults DefaultsConfig `yaml:"defaults"`
	Auth     AuthConfig     `yaml:"auth"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port    int    `yaml:"port"`
	Host    string `yaml:"host"`
	BaseURL string `yaml:"base_url"`
}

// GitHubConfig holds GitHub App authentication settings.
type GitHubConfig struct {
	WebhookSecret string `yaml:"webhook_secret"`
	AppID         int64  `yaml:"app_id"`
	AppPrivateKey string `yaml:"app_private_key"`
}

// ArgoCDConfig holds Argo CD connection settings.
type ArgoCDConfig struct {
	ServerURL string `yaml:"server_url"`
	Token     string `yaml:"token"`
	Insecure  bool   `yaml:"insecure"`
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Address  string `yaml:"address"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// DefaultsConfig holds default behavior settings.
type DefaultsConfig struct {
	Autoplan           bool     `yaml:"autoplan"`
	RequireApproval    bool     `yaml:"require_approval"`
	DeleteSourceBranch bool     `yaml:"delete_source_branch"`
	AllowedRepos       []string `yaml:"allowed_repos"`
	AutoMerge          bool     `yaml:"auto_merge"`
	MergeMethod        string   `yaml:"merge_method"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	Enabled         bool               `yaml:"enabled"`
	SessionSecret   string             `yaml:"session_secret"`
	SessionTTL      time.Duration      `yaml:"session_ttl"`
	CookieDomain    string             `yaml:"cookie_domain"`
	CookieSecure    bool               `yaml:"cookie_secure"`
	DefaultRole     string             `yaml:"default_role"`
	GitHub          *GitHubOAuthConfig `yaml:"github,omitempty"`
	OIDC            *OIDCConfig        `yaml:"oidc,omitempty"`
	Basic           *BasicAuthConfig   `yaml:"basic,omitempty"`
	RoleAssignments []RoleAssignment   `yaml:"role_assignments"`
}

// BasicAuthConfig holds basic auth settings for local development.
type BasicAuthConfig struct {
	Users []BasicAuthUser `yaml:"users"`
}

// BasicAuthUser represents a basic auth user.
type BasicAuthUser struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Role     string `yaml:"role"`
}

// GitHubOAuthConfig holds GitHub OAuth settings (separate from GitHub App).
type GitHubOAuthConfig struct {
	ClientID     string   `yaml:"client_id"`
	ClientSecret string   `yaml:"client_secret"`
	AllowedOrgs  []string `yaml:"allowed_orgs,omitempty"`
	AllowedTeams []string `yaml:"allowed_teams,omitempty"`
}

// OIDCConfig holds generic OIDC provider settings.
type OIDCConfig struct {
	Name          string   `yaml:"name"`
	IssuerURL     string   `yaml:"issuer_url"`
	ClientID      string   `yaml:"client_id"`
	ClientSecret  string   `yaml:"client_secret"`
	Scopes        []string `yaml:"scopes"`
	UsernameClaim string   `yaml:"username_claim"`
	EmailClaim    string   `yaml:"email_claim"`
	GroupsClaim   string   `yaml:"groups_claim,omitempty"`
}

// RoleAssignment maps patterns to roles.
type RoleAssignment struct {
	Pattern  string `yaml:"pattern"`
	Role     string `yaml:"role"`
	Provider string `yaml:"provider,omitempty"`
}

// RepoConfig represents per-repository configuration (.lemuria.yaml).
type RepoConfig struct {
	Version          int                   `yaml:"version"`
	Autoplan         *bool                 `yaml:"autoplan,omitempty"`
	RequireApproval  *bool                 `yaml:"require_approval,omitempty"`
	Applications     []ApplicationMapping  `yaml:"applications"`
	SyncRequirements []SyncRequirement     `yaml:"sync_requirements"`
}

// ApplicationMapping maps Argo CD applications to repository paths.
type ApplicationMapping struct {
	Name           string   `yaml:"name"`
	Paths          []string `yaml:"paths"`
	ApplicationSet string   `yaml:"applicationset,omitempty"`
}

// SyncRequirement defines sync permissions for applications.
type SyncRequirement struct {
	Name            string   `yaml:"name"`
	RequireApproval bool     `yaml:"require_approval"`
	AllowedUsers    []string `yaml:"allowed_users"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port: 4141,
			Host: "0.0.0.0",
		},
		Redis: RedisConfig{
			Address: "localhost:6379",
			DB:      0,
		},
		Defaults: DefaultsConfig{
			Autoplan:           true,
			RequireApproval:    false,
			DeleteSourceBranch: false,
			AutoMerge:          false,
			MergeMethod:        "squash",
		},
		Auth: AuthConfig{
			Enabled:      false,
			SessionTTL:   24 * time.Hour,
			CookieSecure: true,
			DefaultRole:  "user",
		},
	}
}

// AuthEnabled returns true if authentication is enabled.
func (c *Config) AuthEnabled() bool {
	return c.Auth.Enabled
}

// HasGitHubOAuth returns true if GitHub OAuth is configured.
func (c *Config) HasGitHubOAuth() bool {
	return c.Auth.GitHub != nil && c.Auth.GitHub.ClientID != ""
}

// HasOIDC returns true if OIDC is configured.
func (c *Config) HasOIDC() bool {
	return c.Auth.OIDC != nil && c.Auth.OIDC.IssuerURL != ""
}

// HasBasicAuth returns true if basic auth is configured.
func (c *Config) HasBasicAuth() bool {
	return c.Auth.Basic != nil && len(c.Auth.Basic.Users) > 0
}

// LockTTL returns the default lock TTL (7 days).
func LockTTL() time.Duration {
	return 7 * 24 * time.Hour
}
