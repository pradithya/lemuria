---
layout: default
title: Troubleshooting
nav_order: 7
---

# Troubleshooting

Common issues and solutions when using Lemuria.

---

## Webhook Issues

### Webhooks Not Received

**Symptoms:**
- No plan comment on PR
- No reaction on commands

**Solutions:**

1. **Check webhook delivery in GitHub**
   - Go to GitHub App settings → Advanced → Recent Deliveries
   - Look for failed deliveries (red X)

2. **Verify webhook URL**
   ```
   https://lemuria.example.com/webhook
   ```

3. **Check Lemuria logs**
   ```bash
   kubectl logs -n lemuria deployment/lemuria
   ```

4. **Verify webhook secret matches**
   - GitHub App webhook secret
   - `github.webhook_secret` in config

5. **Check network/firewall**
   - Lemuria must be accessible from GitHub
   - Check ingress configuration

### Webhook Signature Invalid

**Symptoms:**
- 401 Unauthorized in webhook delivery

**Solutions:**

1. Verify `webhook_secret` matches GitHub App
2. Check for leading/trailing whitespace
3. Regenerate webhook secret in GitHub App

---

## Plan Issues

### Plan Not Generated

**Symptoms:**
- PR opened but no plan comment

**Solutions:**

1. **Check autoplan setting**
   ```yaml
   defaults:
     autoplan: true
   ```

2. **Verify `.lemuria.yaml` exists**
   - Must be in repository root
   - Must be valid YAML

3. **Check path patterns**
   ```yaml
   applications:
     - name: my-app
       paths:
         - "apps/**"  # Must match changed files
   ```

4. **Check allowed_repos**
   ```yaml
   defaults:
     allowed_repos:
       - "myorg/myrepo"  # Must include this repo
   ```

### "No applications affected"

**Symptoms:**
- Plan runs but says no applications affected

**Solutions:**

1. **Verify path patterns match files**
   - Check `.lemuria.yaml` patterns
   - Compare with changed files in PR

2. **Check Argo CD application exists**
   - Application must exist in Argo CD
   - Application must reference this repository

3. **Check repository URL matching**
   - Argo CD app `repoURL` must match
   - Both SSH and HTTPS formats are normalized

### Application Locked by Another PR

**Symptoms:**
```
⚠️ Locked by PR #42 (otheruser)
```

**Solutions:**

1. Wait for other PR to complete
2. Ask owner to run `lemuria unlock` on other PR
3. Close the other PR (auto-unlocks)
4. Force unlock via web UI (admin only)

---

## Sync Issues

### "Plan is stale"

**Symptoms:**
```
⚠️ Plan for `my-app` is stale. Please run `lemuria plan` again.
```

**Cause:**
New commits were pushed after the plan was generated.

**Solution:**
```
lemuria plan
```
Then retry sync.

### "PR must be approved"

**Symptoms:**
```
❌ PR must be approved before sync
```

**Solutions:**

1. Get PR approved by a reviewer
2. Or disable approval requirement. Note the approval precedence (highest priority first):
   - Per-app `sync_requirements` in `.lemuria.yaml` (exact match, then wildcard)
   - Repository-level `require_approval` in `.lemuria.yaml`
   - Server `defaults.require_approval` in `lemuria.yaml`

   Check all three levels to find where the requirement is set.

### "Auto-sync is enabled"

**Symptoms:**
```
❌ Application `my-app` has auto-sync enabled.
Disable auto-sync before using Lemuria to prevent conflicts.
```

**Solution:**

Disable auto-sync on the Argo CD application:

```yaml
# Remove or comment out automated sync
spec:
  syncPolicy:
    # automated:    # Remove this
    #   prune: true
    #   selfHeal: true
```

Or via CLI:
```bash
argocd app set my-app --sync-policy none
```

### "No applications locked by this PR"

**Symptoms:**
```
⚠️ No applications are locked by this PR. Run `lemuria plan` first.
```

**Solution:**
```
lemuria plan
```
Then retry sync.

### Sync Fails with Argo CD Error

**Symptoms:**
- Sync command returns Argo CD error

**Solutions:**

1. **Check Argo CD application status**
   ```bash
   argocd app get my-app
   ```

2. **Check for validation errors**
   - Invalid manifests
   - Schema validation failures

3. **Check Argo CD logs**
   ```bash
   kubectl logs -n argocd deployment/argocd-application-controller
   ```

4. **Verify token permissions**
   - Token must have sync permissions
   - Check RBAC policy

---

### "PR has merge conflicts"

**Symptoms:**
```
❌ PR has merge conflicts, please resolve before syncing
```

**Solution:**

Resolve the merge conflicts in the PR branch and push updated commits. Lemuria checks PR mergeability before allowing sync.

---

## Rollback Issues

### Rollback Blocked

Same causes as sync issues:
- Approval required (same precedence rules as sync)
- Auto-sync enabled on the application

### Rollback "Fails"

**Note:** Rollback syncs to the app's configured `targetRevision` (typically `main` or `HEAD`) by passing an empty revision to the Argo CD sync API. If the target revision itself has issues, rollback will still deploy those issues.

### Rollback Doesn't Undo Changes

Rollback reverts to the app's `targetRevision`, not to a previous sync history entry. If the target branch already contains the changes (e.g., the PR was merged), rollback won't revert them.

---

## Lock Issues

### Lock Not Released

**Symptoms:**
- Application stays locked after PR merge/close

**Solutions:**

1. **Manual unlock**
   ```
   lemuria unlock -a my-app
   ```

2. **Check Redis connectivity**
   ```bash
   redis-cli -h redis ping
   ```

3. **Check Lemuria received webhook**
   - PR close event triggers unlock
   - Check webhook delivery

4. **Force unlock (admin)**
   - Via web UI (admin role required)
   - Via API: `DELETE /api/v1/locks/{app}` (admin role required)
   - Via Redis CLI (last resort):
     ```bash
     redis-cli DEL lemuria:lock:my-app
     ```

### Lock Timeout

Locks have a 7-day TTL by default. If a PR is abandoned:

1. Close the PR (triggers auto-unlock)
2. Use `lemuria unlock` on the PR
3. Wait for TTL expiration

---

## Authentication Issues

### "User not in allowed organization"

**Solutions:**

1. Verify user is org member
2. Check `allowed_orgs` spelling
3. For private orgs, ensure OAuth app has access

### "Invalid redirect URI"

**Solutions:**

Ensure callback URLs match exactly:

| Provider | Callback URL |
|----------|--------------|
| GitHub | `https://lemuria.example.com/auth/github/callback` |
| OIDC | `https://lemuria.example.com/auth/oidc/callback` |

### Session Expired

**Solutions:**

1. Log in again
2. Increase `session_ttl`:
   ```yaml
   auth:
     session_ttl: 72h
   ```

### OIDC Discovery Failed

**Solutions:**

1. Verify `issuer_url` is correct
2. Test discovery endpoint:
   ```bash
   curl https://your-issuer/.well-known/openid-configuration
   ```
3. Check network connectivity

### OIDC Login Rejected

If a user can authenticate with the IdP but is rejected by Lemuria, check:

1. **`allowed_domains`** - User's email domain may not be in the allowed list
2. **Email claim** - Ensure the IdP returns the email in the configured `email_claim` field

---

## Redis Issues

### Connection Refused

**Symptoms:**
```
failed to connect to Redis: connection refused
```

**Solutions:**

1. Verify Redis is running
   ```bash
   kubectl get pods -l app=redis
   ```

2. Check address in config
   ```yaml
   redis:
     address: "redis:6379"
   ```

3. Check network policies

### Authentication Failed

**Solutions:**

1. Verify password in config:
   ```yaml
   redis:
     password: "${REDIS_PASSWORD}"
   ```

2. Check environment variable is set

---

## Argo CD Issues

### Connection Failed

**Symptoms:**
```
failed to connect to Argo CD
```

**Solutions:**

1. Verify URL:
   ```yaml
   argocd:
     server_url: "https://argocd.example.com"
   ```

2. Check network connectivity:
   ```bash
   curl -k https://argocd.example.com/api/version
   ```

3. For internal clusters, use service URL:
   ```yaml
   argocd:
     server_url: "https://argocd-server.argocd.svc:443"
     insecure: true  # If using self-signed cert
   ```

### Permission Denied

**Symptoms:**
```
permission denied: applications, sync
```

**Solutions:**

1. Check token has correct permissions
2. Verify RBAC policy:
   ```csv
   p, lemuria, applications, get, */*, allow
   p, lemuria, applications, sync, */*, allow
   ```

3. Regenerate token

---

## Common Error Messages

| Error | Cause | Solution |
|-------|-------|----------|
| `not a lemuria command` | Comment doesn't contain valid command | Check command syntax |
| `application not found` | App doesn't exist in Argo CD | Verify app name |
| `failed to acquire lock` | Redis error | Check Redis connectivity |
| `merge conflicts` | PR has conflicts | Resolve conflicts |
| `webhook secret mismatch` | Invalid signature | Check webhook secret |
| `auto-sync enabled` | App has automated sync policy | Disable auto-sync on the Argo CD app |

---

## Debugging

### Enable Debug Logging

Set `log_level` to `debug` in the server configuration:

```yaml
server:
  log_level: "debug"    # debug, info, warn, error
```

### Check Health and Readiness

Lemuria provides two health check endpoints:

```bash
# Health check (always returns 200 if the server is running)
curl https://lemuria.example.com/health

# Readiness check (verifies Redis connectivity, returns 503 if unavailable)
curl https://lemuria.example.com/ready
```

The `/ready` endpoint checks Redis connectivity via `PING`. If it returns 503, check your Redis connection.

### Check Component Status

```bash
# Lemuria
kubectl logs -n lemuria deployment/lemuria

# Redis
kubectl logs -n lemuria deployment/redis

# Argo CD
kubectl logs -n argocd deployment/argocd-server
```

### Test Connectivity

```bash
# GitHub API
curl -H "Authorization: token $GITHUB_TOKEN" https://api.github.com/user

# Argo CD API
curl -k -H "Authorization: Bearer $ARGOCD_TOKEN" \
  https://argocd.example.com/api/v1/applications

# Redis
redis-cli -h redis ping
```

---

## Getting Help

If you're still stuck:

1. **Check existing issues:** [GitHub Issues](https://github.com/pradithya/lemuria/issues)
2. **Open a new issue** with:
   - Lemuria version
   - Configuration (redact secrets)
   - Error messages
   - Steps to reproduce
3. **Join our community**

---

## Next Steps

- [Getting Started](getting-started) - Installation guide
- [Configuration](configuration) - Configuration reference
- [Commands](commands) - Command reference
