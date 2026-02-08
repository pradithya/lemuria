---
layout: default
title: Authentication
nav_order: 4
---

# Authentication

Lemuria supports multiple authentication providers for the web UI, including GitHub OAuth, generic OIDC (Okta, Auth0, Azure AD, etc.), and basic auth for development.

---

## Overview

| Provider | Use Case | Features |
|----------|----------|----------|
| **GitHub OAuth** | GitHub-centric organizations | Org/team restrictions |
| **OIDC** | Enterprise SSO | Works with any OIDC provider, domain restrictions |
| **Basic Auth** | Local development | Simple username/password |

---

## Enabling Authentication

```yaml
auth:
  enabled: true
  session_secret: "${SESSION_SECRET}"
  session_ttl: 24h
  cookie_secure: true
  default_role: "user"
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable authentication |
| `session_secret` | string | Required | Secret for signing cookies |
| `session_ttl` | duration | `24h` | Session duration |
| `cookie_domain` | string | auto | Cookie domain |
| `cookie_secure` | bool | `true` | HTTPS-only cookies |
| `default_role` | string | `user` | Default role for new users |

### Generate Session Secret

```bash
openssl rand -hex 32
```

---

## GitHub OAuth

Authenticate users via GitHub OAuth and optionally restrict access to organization or team members.

### Step 1: Create OAuth App

1. Go to **GitHub Settings** → **Developer settings** → **OAuth Apps**
2. Click **New OAuth App**
3. Configure:

| Field | Value |
|-------|-------|
| **Application name** | Lemuria |
| **Homepage URL** | `https://lemuria.example.com` |
| **Authorization callback URL** | `https://lemuria.example.com/auth/github/callback` |

4. Note the **Client ID** and generate a **Client Secret**

### Step 2: Configure Lemuria

```yaml
auth:
  enabled: true
  session_secret: "${SESSION_SECRET}"

  github:
    client_id: "${GITHUB_OAUTH_CLIENT_ID}"
    client_secret: "${GITHUB_OAUTH_CLIENT_SECRET}"

    # Optional: Restrict to organization members
    allowed_orgs:
      - "myorg"

    # Optional: Restrict to team members
    allowed_teams:
      - "myorg/platform-team"
      - "myorg/devops"
```

### Configuration Options

| Field | Type | Description |
|-------|------|-------------|
| `client_id` | string | OAuth App Client ID |
| `client_secret` | string | OAuth App Client Secret |
| `allowed_orgs` | []string | Restrict to org members |
| `allowed_teams` | []string | Restrict to team members |

### Team Format

Teams are specified as `org/team-slug`:

```yaml
allowed_teams:
  - "myorg/platform-team"    # myorg organization, platform-team
  - "myorg/devops"           # myorg organization, devops team
```

---

## OIDC (OpenID Connect)

Integrate with any OIDC-compliant identity provider (Okta, Auth0, Azure AD, Keycloak, Google, etc.).

### Generic OIDC Configuration

```yaml
auth:
  enabled: true
  session_secret: "${SESSION_SECRET}"

  oidc:
    name: "Company SSO"
    issuer_url: "https://login.example.com"
    client_id: "${OIDC_CLIENT_ID}"
    client_secret: "${OIDC_CLIENT_SECRET}"
    scopes:
      - "openid"
      - "profile"
      - "email"
    username_claim: "preferred_username"
    email_claim: "email"
    groups_claim: "groups"
```

### Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | Required | Display name for login button |
| `issuer_url` | string | Required | OIDC issuer URL |
| `client_id` | string | Required | OIDC client ID |
| `client_secret` | string | Required | OIDC client secret |
| `scopes` | []string | `["openid", "profile", "email"]` | OAuth scopes |
| `username_claim` | string | `preferred_username` | Claim for username |
| `email_claim` | string | `email` | Claim for email |
| `groups_claim` | string | - | Claim for groups (for role assignment) |
| `allowed_domains` | []string | `[]` | Restrict login to email domains (empty = all allowed) |

### Domain Restrictions

Use `allowed_domains` to restrict OIDC login to users with email addresses from specific domains. Domain matching is case-insensitive.

```yaml
oidc:
  name: "Company SSO"
  issuer_url: "https://login.example.com"
  client_id: "${OIDC_CLIENT_ID}"
  client_secret: "${OIDC_CLIENT_SECRET}"
  allowed_domains:
    - "example.com"
    - "subsidiary.com"
```

If `allowed_domains` is empty or not set, all domains are allowed.

### Provider-Specific Examples

#### Okta

```yaml
oidc:
  name: "Okta"
  issuer_url: "https://mycompany.okta.com"
  client_id: "${OKTA_CLIENT_ID}"
  client_secret: "${OKTA_CLIENT_SECRET}"
  scopes:
    - "openid"
    - "profile"
    - "email"
    - "groups"
  groups_claim: "groups"
```

**Okta Setup:**
1. Create a new **Web Application** in Okta
2. Set redirect URI: `https://lemuria.example.com/auth/oidc/callback`
3. Enable **Authorization Code** grant
4. Add `groups` claim to ID token

#### Azure AD

```yaml
oidc:
  name: "Azure AD"
  issuer_url: "https://login.microsoftonline.com/{tenant-id}/v2.0"
  client_id: "${AZURE_CLIENT_ID}"
  client_secret: "${AZURE_CLIENT_SECRET}"
  scopes:
    - "openid"
    - "profile"
    - "email"
  username_claim: "preferred_username"
  email_claim: "email"
```

**Azure AD Setup:**
1. Register an application in Azure AD
2. Add redirect URI: `https://lemuria.example.com/auth/oidc/callback`
3. Create a client secret
4. Add API permissions: `openid`, `profile`, `email`

#### Auth0

```yaml
oidc:
  name: "Auth0"
  issuer_url: "https://mycompany.auth0.com/"
  client_id: "${AUTH0_CLIENT_ID}"
  client_secret: "${AUTH0_CLIENT_SECRET}"
  scopes:
    - "openid"
    - "profile"
    - "email"
```

#### Google

```yaml
oidc:
  name: "Google"
  issuer_url: "https://accounts.google.com"
  client_id: "${GOOGLE_CLIENT_ID}"
  client_secret: "${GOOGLE_CLIENT_SECRET}"
  scopes:
    - "openid"
    - "profile"
    - "email"
```

#### Keycloak

```yaml
oidc:
  name: "Keycloak"
  issuer_url: "https://keycloak.example.com/realms/myrealm"
  client_id: "${KEYCLOAK_CLIENT_ID}"
  client_secret: "${KEYCLOAK_CLIENT_SECRET}"
  scopes:
    - "openid"
    - "profile"
    - "email"
  groups_claim: "groups"
```

---

## Basic Auth

For local development only. **Not recommended for production.**

```yaml
auth:
  enabled: true
  session_secret: "dev-secret"
  cookie_secure: false  # Allow HTTP for local dev

  basic:
    users:
      - username: admin
        password: admin
        role: admin
      - username: developer
        password: developer
        role: user
```

---

## Role-Based Access Control

Lemuria has two roles:

| Role | Permissions |
|------|-------------|
| `admin` | Full access: read, write, delete, admin. Can force-unlock applications, manage users and roles. |
| `user` | Read-only access to the web UI. |

### Admin Capabilities

Admins have access to additional API endpoints:

| Action | Endpoint | Description |
|--------|----------|-------------|
| Force unlock | `DELETE /api/v1/locks/{app}` | Remove a lock for any application |
| List users | `GET /api/v1/users` | View all active users and roles |
| Update role | `PUT /api/v1/users/{id}/role` | Change a user's role (`admin` or `user`) |

Force unlock is also available via the web UI for admin users.

### Role Assignment

Assign roles based on patterns:

```yaml
auth:
  default_role: "user"

  role_assignments:
    # By email
    - pattern: "admin@example.com"
      role: "admin"

    # By email domain
    - pattern: "*@platform.example.com"
      role: "admin"

    # By GitHub team
    - pattern: "@myorg/platform-admins"
      provider: "github"
      role: "admin"

    # By OIDC group
    - pattern: "platform-admins"
      provider: "oidc"
      role: "admin"
```

### Assignment Rules

- First matching rule wins
- If no rule matches, `default_role` is used
- Patterns support wildcards (`*`)

### Pattern Examples

| Pattern | Matches |
|---------|---------|
| `admin@example.com` | Exact email |
| `*@platform.example.com` | Any email at domain |
| `@myorg/team` | GitHub team membership |
| `admin-group` | OIDC group membership |

---

## Multiple Providers

You can configure multiple providers. Users will see login buttons for each:

```yaml
auth:
  enabled: true
  session_secret: "${SESSION_SECRET}"

  github:
    client_id: "${GITHUB_CLIENT_ID}"
    client_secret: "${GITHUB_CLIENT_SECRET}"
    allowed_orgs:
      - "myorg"

  oidc:
    name: "Company SSO"
    issuer_url: "https://login.example.com"
    client_id: "${OIDC_CLIENT_ID}"
    client_secret: "${OIDC_CLIENT_SECRET}"
```

---

## Session Management

### Session Storage

Sessions are stored in Redis (the same instance used for application locks):

```yaml
redis:
  address: "redis:6379"
  password: "${REDIS_PASSWORD}"
```

### Session Duration

```yaml
auth:
  session_ttl: 24h  # Default: 24 hours
```

### Session Cookie

The session cookie is named `lemuria_session` and is configured with `HttpOnly`, `Secure` (when `cookie_secure: true`), and `SameSite=Lax` attributes.

### Logout

Users can log out via the web UI or by calling `POST /auth/logout`.

---

## Auth API Endpoints

These endpoints are available when authentication is enabled:

| Endpoint | Method | Auth Required | Description |
|----------|--------|---------------|-------------|
| `/auth/providers` | GET | No | List available auth providers |
| `/auth/github/login` | GET | No | Initiate GitHub OAuth flow |
| `/auth/github/callback` | GET | No | GitHub OAuth callback |
| `/auth/oidc/login` | GET | No | Initiate OIDC login flow |
| `/auth/oidc/callback` | GET | No | OIDC callback |
| `/auth/basic/login` | POST | No | Basic auth login |
| `/auth/me` | GET | Yes | Get current user info |
| `/auth/logout` | POST | Yes | End session |

---

## Security Best Practices

1. **Use HTTPS** - Set `cookie_secure: true` (default)
2. **Strong session secret** - Use `openssl rand -hex 32`
3. **Rotate secrets** - Periodically rotate session and OAuth secrets
4. **Restrict access** - Use `allowed_orgs`/`allowed_teams` with GitHub, or `allowed_domains` with OIDC
5. **Use SSO** - Prefer OIDC over basic auth in production

---

## Troubleshooting

### "Invalid redirect URI"

Ensure callback URL matches exactly:
- GitHub: `https://lemuria.example.com/auth/github/callback`
- OIDC: `https://lemuria.example.com/auth/oidc/callback`

### "User not in allowed organization"

1. Verify user is a member of the organization
2. Check `allowed_orgs` spelling
3. For private orgs, ensure OAuth app has org access

### "Session expired"

Increase `session_ttl` or have users log in again.

### "OIDC discovery failed"

1. Verify `issuer_url` is correct
2. Check `.well-known/openid-configuration` is accessible
3. Ensure network connectivity to IdP

---

## Next Steps

- [Commands](commands) - Available commands
- [Workflow](workflow) - PR workflow details
- [Troubleshooting](troubleshooting) - Common issues
