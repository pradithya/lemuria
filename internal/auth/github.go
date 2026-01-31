package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

const (
	githubUserAPI  = "https://api.github.com/user"
	githubEmailAPI = "https://api.github.com/user/emails"
	githubOrgsAPI  = "https://api.github.com/user/orgs"
	githubTeamsAPI = "https://api.github.com/user/teams"
)

// GitHubProvider implements OAuth authentication via GitHub.
type GitHubProvider struct {
	config       *oauth2.Config
	allowedOrgs  []string
	allowedTeams []string
	httpClient   *http.Client
}

// NewGitHubProvider creates a new GitHub OAuth provider.
func NewGitHubProvider(cfg *config.GitHubOAuthConfig, baseURL string) *GitHubProvider {
	return &GitHubProvider{
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     github.Endpoint,
			RedirectURL:  baseURL + "/auth/github/callback",
			Scopes:       []string{"user:email", "read:org"},
		},
		allowedOrgs:  cfg.AllowedOrgs,
		allowedTeams: cfg.AllowedTeams,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Name returns the provider identifier.
func (p *GitHubProvider) Name() string {
	return "github"
}

// DisplayName returns the human-readable name for UI.
func (p *GitHubProvider) DisplayName() string {
	return "GitHub"
}

// AuthURL returns the URL to redirect users for authentication.
func (p *GitHubProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state)
}

// Exchange exchanges the authorization code for user information.
func (p *GitHubProvider) Exchange(ctx context.Context, code string) (*models.User, error) {
	// Exchange code for token
	token, err := p.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchanging code: %w", err)
	}

	// Get user info
	user, err := p.getUserInfo(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("getting user info: %w", err)
	}

	// Check organization membership if configured
	if len(p.allowedOrgs) > 0 {
		orgs, err := p.getUserOrgs(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("getting user orgs: %w", err)
		}

		if !p.isOrgMember(orgs) {
			return nil, fmt.Errorf("user is not a member of allowed organizations")
		}
	}

	// Get user teams for role resolution
	if len(p.allowedTeams) > 0 || true { // Always fetch teams for RBAC
		teams, err := p.getUserTeams(ctx, token)
		if err != nil {
			// Non-fatal, just log
		} else {
			user.Groups = teams
		}
	}

	return user, nil
}

// getUserInfo fetches the user's profile from GitHub.
func (p *GitHubProvider) getUserInfo(ctx context.Context, token *oauth2.Token) (*models.User, error) {
	// Fetch user profile
	req, err := http.NewRequestWithContext(ctx, "GET", githubUserAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error: %s", string(body))
	}

	var ghUser struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ghUser); err != nil {
		return nil, fmt.Errorf("decoding user: %w", err)
	}

	// If email is not public, fetch from emails API
	email := ghUser.Email
	if email == "" {
		email, _ = p.getPrimaryEmail(ctx, token)
	}

	now := time.Now()
	return &models.User{
		ID:          "github:" + strconv.FormatInt(ghUser.ID, 10),
		Login:       ghUser.Login,
		Email:       email,
		Name:        ghUser.Name,
		AvatarURL:   ghUser.AvatarURL,
		Provider:    "github",
		CreatedAt:   now,
		LastLoginAt: now,
	}, nil
}

// getPrimaryEmail fetches the user's primary email from GitHub.
func (p *GitHubProvider) getPrimaryEmail(ctx context.Context, token *oauth2.Token) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", githubEmailAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get emails")
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", err
	}

	// Find primary email
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}

	// Fallback to first verified email
	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}

	return "", nil
}

// getUserOrgs fetches the user's organizations.
func (p *GitHubProvider) getUserOrgs(ctx context.Context, token *oauth2.Token) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", githubOrgsAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get orgs")
	}

	var orgs []struct {
		Login string `json:"login"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, err
	}

	result := make([]string, len(orgs))
	for i, org := range orgs {
		result[i] = org.Login
	}

	return result, nil
}

// getUserTeams fetches the user's teams.
func (p *GitHubProvider) getUserTeams(ctx context.Context, token *oauth2.Token) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", githubTeamsAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get teams")
	}

	var teams []struct {
		Slug string `json:"slug"`
		Org  struct {
			Login string `json:"login"`
		} `json:"organization"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&teams); err != nil {
		return nil, err
	}

	// Format as "org/team"
	result := make([]string, len(teams))
	for i, team := range teams {
		result[i] = team.Org.Login + "/" + team.Slug
	}

	return result, nil
}

// isOrgMember checks if user is a member of any allowed organization.
func (p *GitHubProvider) isOrgMember(userOrgs []string) bool {
	for _, userOrg := range userOrgs {
		for _, allowedOrg := range p.allowedOrgs {
			if userOrg == allowedOrg {
				return true
			}
		}
	}
	return false
}
