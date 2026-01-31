#!/bin/bash
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-lemuria-e2e}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E_DIR="$(dirname "$SCRIPT_DIR")"

echo "=== Lemuria E2E Test Teardown ==="

# Kill port forwards
if [ -f "$E2E_DIR/.argocd-pf-pid" ]; then
    PID=$(cat "$E2E_DIR/.argocd-pf-pid")
    if kill -0 "$PID" 2>/dev/null; then
        echo "Stopping Argo CD port-forward (PID: $PID)..."
        kill "$PID" || true
    fi
    rm -f "$E2E_DIR/.argocd-pf-pid"
fi

if [ -f "$E2E_DIR/.redis-pf-pid" ]; then
    PID=$(cat "$E2E_DIR/.redis-pf-pid")
    if kill -0 "$PID" 2>/dev/null; then
        echo "Stopping Redis port-forward (PID: $PID)..."
        kill "$PID" || true
    fi
    rm -f "$E2E_DIR/.redis-pf-pid"
fi

# Delete cluster
if k3d cluster list 2>/dev/null | grep -q "$CLUSTER_NAME"; then
    echo "Deleting k3d cluster: $CLUSTER_NAME..."
    k3d cluster delete "$CLUSTER_NAME"
fi

# Cleanup files
rm -f "$E2E_DIR/.argocd-password"
rm -f "$E2E_DIR/.argocd-token"
rm -f "$E2E_DIR/.env"

echo "Teardown complete."
