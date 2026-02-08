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

	"github.com/org/lemuria/internal/config"
	"github.com/org/lemuria/internal/models"
)

// GitLabProvider implements OAuth authentication via GitLab.
type GitLabProvider struct {
	config        *oauth2.Config
	baseURL       string
	allowedGroups []string
	httpClient    *http.Client
}

// NewGitLabProvider creates a new GitLab OAuth provider.
func NewGitLabProvider(cfg *config.GitLabOAuthConfig, baseURL string) *GitLabProvider {
	gitlabURL := cfg.URL
	if gitlabURL == "" {
		gitlabURL = "https://gitlab.com"
	}

	return &GitLabProvider{
		config: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  gitlabURL + "/oauth/authorize",
				TokenURL: gitlabURL + "/oauth/token",
			},
			RedirectURL: baseURL + "/auth/gitlab/callback",
			Scopes:      []string{"read_user", "read_api"},
		},
		baseURL:       gitlabURL,
		allowedGroups: cfg.AllowedGroups,
		httpClient:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Name returns the provider identifier.
func (p *GitLabProvider) Name() string {
	return "gitlab"
}

// DisplayName returns the human-readable name for UI.
func (p *GitLabProvider) DisplayName() string {
	return "GitLab"
}

// AuthURL returns the URL to redirect users for authentication.
func (p *GitLabProvider) AuthURL(state string) string {
	return p.config.AuthCodeURL(state)
}

// Exchange exchanges the authorization code for user information.
func (p *GitLabProvider) Exchange(ctx context.Context, code string) (*models.User, error) {
	token, err := p.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchanging code: %w", err)
	}

	user, err := p.getUserInfo(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("getting user info: %w", err)
	}

	// Check group membership if configured
	if len(p.allowedGroups) > 0 {
		groups, err := p.getUserGroups(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("getting user groups: %w", err)
		}

		if !p.isGroupMember(groups) {
			return nil, fmt.Errorf("user is not a member of allowed groups")
		}

		user.Groups = groups
	}

	return user, nil
}

// getUserInfo fetches the user's profile from GitLab.
func (p *GitLabProvider) getUserInfo(ctx context.Context, token *oauth2.Token) (*models.User, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/api/v4/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitLab API error: %s", string(body))
	}

	var glUser struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&glUser); err != nil {
		return nil, fmt.Errorf("decoding user: %w", err)
	}

	now := time.Now()
	return &models.User{
		ID:          "gitlab:" + strconv.FormatInt(glUser.ID, 10),
		Login:       glUser.Username,
		Email:       glUser.Email,
		Name:        glUser.Name,
		AvatarURL:   glUser.AvatarURL,
		Provider:    "gitlab",
		CreatedAt:   now,
		LastLoginAt: now,
	}, nil
}

// getUserGroups fetches the user's group memberships from GitLab.
// It paginates through all pages to ensure groups are not missed.
func (p *GitLabProvider) getUserGroups(ctx context.Context, token *oauth2.Token) ([]string, error) {
	var result []string
	page := 1

	for {
		url := fmt.Sprintf("%s/api/v4/groups?min_access_level=10&per_page=100&page=%d", p.baseURL, page)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)

		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("failed to get groups (status %d)", resp.StatusCode)
		}

		var groups []struct {
			FullPath string `json:"full_path"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()

		if len(groups) == 0 {
			break
		}

		for _, g := range groups {
			result = append(result, g.FullPath)
		}

		page++
	}

	return result, nil
}

// isGroupMember checks if user is a member of any allowed group.
func (p *GitLabProvider) isGroupMember(userGroups []string) bool {
	for _, userGroup := range userGroups {
		for _, allowedGroup := range p.allowedGroups {
			if userGroup == allowedGroup {
				return true
			}
		}
	}
	return false
}
