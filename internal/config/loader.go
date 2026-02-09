package config

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

// Load reads and parses configuration files at the given paths.
// Multiple files are merged in order - later files override earlier ones.
// Environment variables override config values using double underscore (__) for hierarchy.
// Example: ARGOCD__TOKEN overrides argocd.token
func Load(paths ...string) (*Config, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one config path is required")
	}

	k := koanf.New(".")

	// Load YAML config files in order (later files override earlier ones)
	for _, path := range paths {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("reading config file %s: %w", path, err)
		}
	}

	// Overlay environment variables
	// Uses double underscore (__) for hierarchy, single underscore preserved in keys
	// Examples:
	//   ARGOCD__TOKEN       -> argocd.token
	//   ARGOCD__SERVER_URL  -> argocd.server_url
	//   GITHUB__WEBHOOK_SECRET -> github.webhook_secret
	if err := k.Load(env.Provider("", ".", func(s string) string {
		key := strings.ToLower(s)
		key = strings.ReplaceAll(key, "__", ".")
		return key
	}), nil); err != nil {
		return nil, fmt.Errorf("loading env vars: %w", err)
	}

	// Unmarshal into default config (preserves defaults for unset fields)
	cfg := DefaultConfig()
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// LoadRepoConfig reads and parses a repository's .lemuria.yaml file.
func LoadRepoConfig(data []byte) (*RepoConfig, error) {
	k := koanf.New(".")

	if err := k.Load(rawbytes.Provider(data), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("parsing repo config: %w", err)
	}

	var cfg RepoConfig
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling repo config: %w", err)
	}

	if cfg.Version == 0 {
		cfg.Version = 1
	}

	return &cfg, nil
}

// validate checks that required configuration fields are set.
func validate(cfg *Config) error {
	var errs []string

	// Detect whether any GitHub or GitLab fields are set (partial or full config).
	githubPartial := cfg.GitHub.AppID != 0 || cfg.GitHub.AppPrivateKey != "" || cfg.GitHub.WebhookSecret != ""
	gitlabPartial := cfg.GitLab.Token != "" || cfg.GitLab.WebhookSecret != "" || cfg.GitLab.URL != ""

	if !githubPartial && !gitlabPartial {
		errs = append(errs, "at least one VCS provider (github or gitlab) must be configured")
	}

	// Validate GitHub config if any GitHub fields are set
	if githubPartial {
		if cfg.GitHub.WebhookSecret == "" {
			errs = append(errs, "github.webhook_secret is required when github is configured")
		}
		if cfg.GitHub.AppID == 0 {
			errs = append(errs, "github.app_id is required when github is configured")
		}
		if cfg.GitHub.AppPrivateKey == "" {
			errs = append(errs, "github.app_private_key is required when github is configured")
		}
	}

	// Validate GitLab config if any GitLab fields are set
	if gitlabPartial {
		if cfg.GitLab.Token == "" {
			errs = append(errs, "gitlab.token is required when gitlab is configured")
		}
	}

	if cfg.ArgoCD.ServerURL == "" {
		errs = append(errs, "argocd.server_url is required")
	}

	if cfg.ArgoCD.Token == "" {
		errs = append(errs, "argocd.token is required")
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration errors:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// IsRepoAllowed checks if the given repository is in the allowed list.
func (c *Config) IsRepoAllowed(repo string) bool {
	if len(c.Defaults.AllowedRepos) == 0 {
		return true
	}

	for _, pattern := range c.Defaults.AllowedRepos {
		if matchRepoPattern(pattern, repo) {
			return true
		}
	}

	return false
}

// matchRepoPattern checks if repo matches the pattern (supports wildcards).
func matchRepoPattern(pattern, repo string) bool {
	// Exact match
	if pattern == repo {
		return true
	}

	// Wildcard match: "org/*" matches "org/any-repo"
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		return strings.HasPrefix(repo, prefix+"/")
	}

	return false
}
