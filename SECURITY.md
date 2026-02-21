# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |

## Reporting a Vulnerability

If you discover a security vulnerability in Lemuria, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please send an email to the project maintainers (pradithya.aria@gmail.com) with the following information:

1. Description of the vulnerability
2. Steps to reproduce the issue
3. Potential impact
4. Suggested fix (if any)

### Response Timeline

- **Acknowledgment**: Within 48 hours of receiving the report
- **Initial Assessment**: Within 7 days
- **Fix/Patch**: Within 30 days for critical vulnerabilities

### What to Expect

- We will acknowledge receipt of your vulnerability report
- We will provide an estimated timeline for a fix
- We will notify you when the vulnerability is fixed
- We will credit you in the release notes (unless you prefer to remain anonymous)

## Security Best Practices for Deployment

- Always use TLS for webhook endpoints
- Use strong webhook secrets (HMAC-SHA256 for GitHub, token for GitLab)
- Restrict ArgoCD API access with minimal required permissions
- Store sensitive configuration (tokens, secrets) in environment variables, not config files
- Use Redis authentication when deploying with distributed locks
