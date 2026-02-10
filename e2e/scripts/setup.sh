#!/bin/bash
# Copyright 2026 Lemuria Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-lemuria-e2e}"
ARGOCD_VERSION="${ARGOCD_VERSION:-v2.10.0}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(dirname "$SCRIPT_DIR")"

echo "=== Lemuria E2E Test Setup ==="

# Check prerequisites
check_prerequisites() {
    echo "Checking prerequisites..."

    local missing=()

    command -v k3d >/dev/null 2>&1 || missing+=("k3d")
    command -v kubectl >/dev/null 2>&1 || missing+=("kubectl")
    command -v helm >/dev/null 2>&1 || missing+=("helm")

    if [ ${#missing[@]} -ne 0 ]; then
        echo "ERROR: Missing required tools: ${missing[*]}"
        echo "Please install them and try again."
        exit 1
    fi

    echo "All prerequisites met."
}

# Create k3d cluster
create_cluster() {
    echo "Creating k3d cluster: $CLUSTER_NAME..."

    if k3d cluster list | grep -q "$CLUSTER_NAME"; then
        echo "Cluster $CLUSTER_NAME already exists, deleting..."
        k3d cluster delete "$CLUSTER_NAME"
    fi

    k3d cluster create "$CLUSTER_NAME" \
        --agents 1 \
        --wait \
        --timeout 120s \
        -p "8080:80@loadbalancer" \
        -p "8443:443@loadbalancer"

    echo "Waiting for cluster to be ready..."
    kubectl wait --for=condition=Ready nodes --all --timeout=120s

    echo "Cluster created successfully."
}

# Install Argo CD
install_argocd() {
    echo "Installing Argo CD ${ARGOCD_VERSION}..."

    kubectl create namespace argocd --dry-run=client -o yaml | kubectl apply -f -

    kubectl apply -n argocd -f "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml"

    echo "Waiting for Argo CD to be ready..."
    kubectl wait --for=condition=Available deployment/argocd-server -n argocd --timeout=300s
    kubectl wait --for=condition=Available deployment/argocd-repo-server -n argocd --timeout=300s
    kubectl wait --for=condition=Available deployment/argocd-applicationset-controller -n argocd --timeout=300s

    # Patch argocd-server to disable TLS (for easier testing)
    kubectl patch deployment argocd-server -n argocd --type='json' \
        -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--insecure"}]'

    # Wait for rollout
    kubectl rollout status deployment/argocd-server -n argocd --timeout=120s

    echo "Argo CD installed successfully."
}

# Create Ingress for Argo CD via Traefik
create_argocd_ingress() {
    echo "Creating Ingress for Argo CD..."

    cat <<EOF | kubectl apply -f -
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: argocd-server
  namespace: argocd
spec:
  rules:
  - host: argocd.127.0.0.1.nip.io
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: argocd-server
            port:
              number: 80
EOF

    echo "Waiting for Ingress to be ready..."
    # Wait for Traefik to pick up the Ingress
    local retries=30
    local i=0
    while [ $i -lt $retries ]; do
        if curl -sf -o /dev/null "http://argocd.127.0.0.1.nip.io:8080/api/v1/session" 2>/dev/null || \
           curl -sf -o /dev/null "http://argocd.127.0.0.1.nip.io:8080/healthz" 2>/dev/null; then
            echo "Argo CD Ingress is ready."
            return
        fi
        sleep 2
        i=$((i + 1))
    done
    echo "WARNING: Ingress may not be ready yet, continuing..."
}

# Get Argo CD credentials
get_argocd_credentials() {
    echo "Getting Argo CD credentials..."

    # Get initial admin password
    ARGOCD_PASSWORD=$(kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d)

    echo "Argo CD Admin Password: $ARGOCD_PASSWORD"
    echo "$ARGOCD_PASSWORD" > "$E2E_DIR/.argocd-password"

    echo "Argo CD available at http://argocd.127.0.0.1.nip.io:8080"
}

# Generate Argo CD API token
generate_argocd_token() {
    echo "Generating Argo CD API token..."

    ARGOCD_PASSWORD=$(cat "$E2E_DIR/.argocd-password")

    # Login and get session token
    SESSION_TOKEN=$(curl -s -X POST "http://argocd.127.0.0.1.nip.io:8080/api/v1/session" \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"admin\",\"password\":\"$ARGOCD_PASSWORD\"}" | \
        grep -o '"token":"[^"]*"' | cut -d'"' -f4)

    if [ -z "$SESSION_TOKEN" ]; then
        echo "ERROR: Failed to get Argo CD session token"
        exit 1
    fi

    echo "$SESSION_TOKEN" > "$E2E_DIR/.argocd-token"
    echo "Argo CD token saved."
}

# Install Redis (in-cluster with port-forward — single connection, stable)
install_redis() {
    echo "Installing Redis..."

    kubectl create namespace redis --dry-run=client -o yaml | kubectl apply -f -

    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: redis
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      containers:
      - name: redis
        image: redis:7-alpine
        ports:
        - containerPort: 6379
        resources:
          limits:
            memory: "128Mi"
            cpu: "500m"
---
apiVersion: v1
kind: Service
metadata:
  name: redis
  namespace: redis
spec:
  selector:
    app: redis
  ports:
  - port: 6379
    targetPort: 6379
EOF

    echo "Waiting for Redis to be ready..."
    kubectl wait --for=condition=Available deployment/redis -n redis --timeout=120s

    # Port forward Redis (single stable connection, not affected by parallel load)
    kubectl port-forward svc/redis -n redis 6379:6379 &
    REDIS_PF_PID=$!
    echo "$REDIS_PF_PID" > "$E2E_DIR/.redis-pf-pid"

    sleep 2

    echo "Redis available at localhost:6379"
}

# Create test namespaces and applications
create_test_apps() {
    echo "Creating test applications..."

    # Create test namespaces
    kubectl create namespace test-apps --dry-run=client -o yaml | kubectl apply -f -
    kubectl create namespace e2e-test-apps --dry-run=client -o yaml | kubectl apply -f -

    # Apply test application manifests
    kubectl apply -f "$E2E_DIR/manifests/"

    echo "Waiting for test applications to sync..."
    sleep 5

    echo "Test applications created."
}

# Write environment file for tests
write_env_file() {
    echo "Writing environment file..."

    ARGOCD_TOKEN=$(cat "$E2E_DIR/.argocd-token" 2>/dev/null || echo "")

    cat > "$E2E_DIR/.env" <<EOF
ARGOCD_SERVER=http://argocd.127.0.0.1.nip.io:8080
ARGOCD_TOKEN=$ARGOCD_TOKEN
ARGOCD_INSECURE=true
REDIS_ADDR=localhost:6379
KUBECONFIG=$(k3d kubeconfig write $CLUSTER_NAME)
EOF

    echo "Environment file written to $E2E_DIR/.env"
}

# Main
main() {
    check_prerequisites
    create_cluster
    install_argocd
    create_argocd_ingress
    install_redis
    get_argocd_credentials
    generate_argocd_token
    create_test_apps
    write_env_file

    echo ""
    echo "=== Setup Complete ==="
    echo "Argo CD: http://argocd.127.0.0.1.nip.io:8080 (admin / $(cat "$E2E_DIR/.argocd-password"))"
    echo "Redis: localhost:6379"
    echo ""
    echo "To run tests: cd $E2E_DIR && go test -v ./..."
    echo "To teardown: $SCRIPT_DIR/teardown.sh"
}

main "$@"
