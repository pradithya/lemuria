package config

import "time"

// Config represents the main Lemuria server configuration.
type Config struct {
	Server   ServerConfig   `koanf:"server"`
	GitHub   GitHubConfig   `koanf:"github"`
	GitLab   GitLabConfig   `koanf:"gitlab"`
	ArgoCD   ArgoCDConfig   `koanf:"argocd"`
	Redis    RedisConfig    `koanf:"redis"`
	Defaults DefaultsConfig `koanf:"defaults"`
	Auth     AuthConfig     `koanf:"auth"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port     int    `koanf:"port"`
	Host     string `koanf:"host"`
	BaseURL  string `koanf:"base_url"`
	LogLevel string `koanf:"log_level"`
}

// GitHubConfig holds GitHub App authentication settings.
type GitHubConfig struct {
	WebhookSecret string `koanf:"webhook_secret"`
	AppID         int64  `koanf:"app_id"`
	AppPrivateKey string `koanf:"app_private_key"`
}

// GitLabConfig holds GitLab connection settings.
type GitLabConfig struct {
	URL           string `koanf:"url"`            // GitLab base URL (default: https://gitlab.com)
	Token         string `koanf:"token"`           // Personal/Group Access Token
	WebhookSecret string `koanf:"webhook_secret"`  // Webhook secret token
}

// ArgoCDConfig holds Argo CD connection settings.
type ArgoCDConfig struct {
	ServerURL      string        `koanf:"server_url"`
	Token          string        `koanf:"token"`
	Insecure       bool          `koanf:"insecure"`
	DiffMode       string        `koanf:"diff_mode"`        // "branch" or "live", default: "branch"
	TempAppTimeout time.Duration `koanf:"temp_app_timeout"` // Timeout for temp app manifest rendering, default: 2m
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Address  string `koanf:"address"`
	Password string `koanf:"password"`
	DB       int    `koanf:"db"`
}

// DefaultsConfig holds default behavior settings.
type DefaultsConfig struct {
	Autoplan           bool     `koanf:"autoplan"`
	RequireApproval    bool     `koanf:"require_approval"`
	DeleteSourceBranch bool     `koanf:"delete_source_branch"`
	AllowedRepos       []string `koanf:"allowed_repos"`
	AutoMerge          bool     `koanf:"auto_merge"`
	MergeMethod        string   `koanf:"merge_method"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	Enabled         bool               `koanf:"enabled"`
	SessionSecret   string             `koanf:"session_secret"`
	SessionTTL      time.Duration      `koanf:"session_ttl"`
	CookieDomain    string             `koanf:"cookie_domain"`
	CookieSecure    bool               `koanf:"cookie_secure"`
	DefaultRole     string             `koanf:"default_role"`
	GitHub          *GitHubOAuthConfig `koanf:"github"`
	GitLab          *GitLabOAuthConfig `koanf:"gitlab"`
	OIDC            *OIDCConfig        `koanf:"oidc"`
	Basic           *BasicAuthConfig   `koanf:"basic"`
	RoleAssignments []RoleAssignment   `koanf:"role_assignments"`
}

// GitLabOAuthConfig holds GitLab OAuth settings.
type GitLabOAuthConfig struct {
	URL           string   `koanf:"url"`            // GitLab base URL
	ClientID      string   `koanf:"client_id"`
	ClientSecret  string   `koanf:"client_secret"`
	AllowedGroups []string `koanf:"allowed_groups"`
}

// BasicAuthConfig holds basic auth settings for local development.
type BasicAuthConfig struct {
	Users []BasicAuthUser `koanf:"users"`
}

// BasicAuthUser represents a basic auth user.
type BasicAuthUser struct {
	Username string `koanf:"username"`
	Password string `koanf:"password"`
	Role     string `koanf:"role"`
}

// GitHubOAuthConfig holds GitHub OAuth settings (separate from GitHub App).
type GitHubOAuthConfig struct {
	ClientID     string   `koanf:"client_id"`
	ClientSecret string   `koanf:"client_secret"`
	AllowedOrgs  []string `koanf:"allowed_orgs"`
	AllowedTeams []string `koanf:"allowed_teams"`
}

// OIDCConfig holds generic OIDC provider settings.
type OIDCConfig struct {
	Name           string   `koanf:"name"`
	IssuerURL      string   `koanf:"issuer_url"`
	ClientID       string   `koanf:"client_id"`
	ClientSecret   string   `koanf:"client_secret"`
	Scopes         []string `koanf:"scopes"`
	UsernameClaim  string   `koanf:"username_claim"`
	EmailClaim     string   `koanf:"email_claim"`
	GroupsClaim    string   `koanf:"groups_claim"`
	AllowedDomains []string `koanf:"allowed_domains"`
}

// RoleAssignment maps patterns to roles.
type RoleAssignment struct {
	Pattern  string `koanf:"pattern"`
	Role     string `koanf:"role"`
	Provider string `koanf:"provider"`
}

// RepoConfig represents per-repository configuration (.lemuria.yaml).
type RepoConfig struct {
	Version          int                  `koanf:"version"`
	Autoplan         *bool                `koanf:"autoplan"`
	RequireApproval  *bool                `koanf:"require_approval"`
	AutoMerge        *bool                `koanf:"auto_merge"`
	Applications     []ApplicationMapping `koanf:"applications"`
	SyncRequirements []SyncRequirement    `koanf:"sync_requirements"`
}

// ApplicationMapping maps Argo CD applications to repository paths.
type ApplicationMapping struct {
	Name           string   `koanf:"name"`
	Paths          []string `koanf:"paths"`
	ApplicationSet string   `koanf:"applicationset"`
}

// SyncRequirement defines sync permissions for applications.
type SyncRequirement struct {
	Name            string   `koanf:"name"`
	RequireApproval bool     `koanf:"require_approval"`
	AllowedUsers    []string `koanf:"allowed_users"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:     4141,
			Host:     "0.0.0.0",
			LogLevel: "info",
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

// HasGitHub returns true if GitHub is configured.
func (c *Config) HasGitHub() bool {
	return c.GitHub.AppID != 0 && c.GitHub.AppPrivateKey != ""
}

// HasGitLab returns true if GitLab is configured.
func (c *Config) HasGitLab() bool {
	return c.GitLab.Token != ""
}

// HasGitLabOAuth returns true if GitLab OAuth is configured.
func (c *Config) HasGitLabOAuth() bool {
	return c.Auth.GitLab != nil && c.Auth.GitLab.ClientID != ""
}

// LockTTL returns the default lock TTL (7 days).
func LockTTL() time.Duration {
	return 7 * 24 * time.Hour
}
