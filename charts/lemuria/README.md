# Lemuria Helm Chart

A Helm chart for deploying Lemuria - GitOps automation for Argo CD.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.8+

## Installation

### From OCI Registry (GHCR)

```bash
# Pull the chart
helm pull oci://ghcr.io/argo-atlantis/charts/lemuria --version <version>

# Install
helm install lemuria oci://ghcr.io/argo-atlantis/charts/lemuria \
  --namespace lemuria \
  --create-namespace
```

### From Source

```bash
# Clone the repository
git clone https://github.com/argo-atlantis/lemuria.git
cd lemuria

# Install
helm install lemuria ./charts/lemuria \
  --namespace lemuria \
  --create-namespace
```

## Configuration

### Required Secrets

Before installing, you need to create a Kubernetes secret with the following keys:

```bash
kubectl create secret generic lemuria-secrets \
  --namespace lemuria \
  --from-literal=GITHUB_WEBHOOK_SECRET='your-webhook-secret' \
  --from-literal=GITHUB_APP_ID='your-app-id' \
  --from-literal=GITHUB_APP_PRIVATE_KEY='your-private-key' \
  --from-literal=ARGOCD_TOKEN='your-argocd-token' \
  --from-literal=REDIS_PASSWORD='your-redis-password'
```

Or, you can let the chart create the secret by setting `secrets.create=true` and providing the values.

### Values

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `1` |
| `image.repository` | Image repository | `ghcr.io/argo-atlantis/lemuria` |
| `image.tag` | Image tag | `""` (defaults to chart appVersion) |
| `image.pullPolicy` | Image pull policy | `Always` |
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.name` | Service account name | `""` |
| `service.type` | Service type | `ClusterIP` |
| `service.port` | Service port | `80` |
| `ingress.enabled` | Enable ingress | `false` |
| `ingress.className` | Ingress class name | `nginx` |
| `ingress.hosts` | Ingress hosts | `[]` |
| `ingress.tls` | Ingress TLS configuration | `[]` |
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `256Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |
| `secrets.create` | Create secrets | `false` |
| `secrets.existingSecret` | Existing secret name | `lemuria-secrets` |
| `redis.enabled` | Deploy Redis | `true` |
| `redis.persistence.enabled` | Enable Redis persistence | `false` |
| `redis.persistence.size` | Redis PVC size | `1Gi` |
| `config.server.port` | Server port | `4141` |
| `config.defaults.autoplan` | Enable autoplan | `true` |
| `config.defaults.require_approval` | Require approval | `false` |
| `config.defaults.auto_merge` | Auto merge | `false` |
| `config.defaults.merge_method` | Merge method | `squash` |

### Example Values

```yaml
# Production values
replicaCount: 2

ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
  hosts:
    - host: lemuria.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: lemuria-tls
      hosts:
        - lemuria.example.com

resources:
  limits:
    cpu: 1000m
    memory: 512Mi
  requests:
    cpu: 200m
    memory: 256Mi

redis:
  enabled: true
  persistence:
    enabled: true
    size: 5Gi
    storageClass: fast-ssd
```

## Testing

### Lint

```bash
helm lint charts/lemuria
```

### Unit Tests

```bash
helm plugin install https://github.com/helm-unittest/helm-unittest.git
helm unittest charts/lemuria
```

### Template Rendering

```bash
helm template test-release charts/lemuria --namespace lemuria
```

## Upgrading

```bash
helm upgrade lemuria oci://ghcr.io/argo-atlantis/charts/lemuria \
  --version <new-version> \
  --namespace lemuria
```

## Uninstalling

```bash
helm uninstall lemuria --namespace lemuria
```

## License

Apache License 2.0
