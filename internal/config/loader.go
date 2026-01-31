package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// envVarPattern matches ${VAR_NAME} patterns for environment variable substitution.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Load reads and parses the configuration file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Substitute environment variables
	content := substituteEnvVars(string(data))

	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte(content), cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// LoadRepoConfig reads and parses a repository's .lemuria.yaml file.
func LoadRepoConfig(data []byte) (*RepoConfig, error) {
	content := substituteEnvVars(string(data))

	var cfg RepoConfig
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("parsing repo config: %w", err)
	}

	if cfg.Version == 0 {
		cfg.Version = 1
	}

	return &cfg, nil
}

// substituteEnvVars replaces ${VAR_NAME} patterns with environment variable values.
func substituteEnvVars(content string) string {
	return envVarPattern.ReplaceAllStringFunc(content, func(match string) string {
		// Extract variable name from ${VAR_NAME}
		varName := match[2 : len(match)-1]

		// Check for default value syntax: ${VAR_NAME:-default}
		if idx := strings.Index(varName, ":-"); idx != -1 {
			name := varName[:idx]
			defaultVal := varName[idx+2:]
			if val := os.Getenv(name); val != "" {
				return val
			}
			return defaultVal
		}

		return os.Getenv(varName)
	})
}

// validate checks that required configuration fields are set.
func validate(cfg *Config) error {
	var errs []string

	if cfg.GitHub.WebhookSecret == "" {
		errs = append(errs, "github.webhook_secret is required")
	}

	if cfg.GitHub.AppID == 0 {
		errs = append(errs, "github.app_id is required")
	}

	if cfg.GitHub.AppPrivateKey == "" {
		errs = append(errs, "github.app_private_key is required")
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
