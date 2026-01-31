package config

import "time"

// Config represents the main Lemuria server configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	GitHub   GitHubConfig   `yaml:"github"`
	ArgoCD   ArgoCDConfig   `yaml:"argocd"`
	Redis    RedisConfig    `yaml:"redis"`
	Defaults DefaultsConfig `yaml:"defaults"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
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
		},
	}
}

// LockTTL returns the default lock TTL (7 days).
func LockTTL() time.Duration {
	return 7 * 24 * time.Hour
}
