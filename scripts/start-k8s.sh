#!/bin/sh

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
STEP="${1:-}"

# Step 1: Build Docker images (runs if no step or step 1 is provided)
if [ -z "$STEP" ] || [ "$STEP" -eq 1 ] 2>/dev/null; then
  echo "==> Building Docker images..."
  docker build -t kraken-backend:latest "$ROOT_DIR/backend"
  docker build -t kraken-frontend:latest "$ROOT_DIR/frontend"

  if command -v k3s >/dev/null 2>&1; then
    echo "==> Importing images into k3s containerd..."
    docker save kraken-backend:latest kraken-frontend:latest | k3s ctr images import -
  elif command -v microk8s >/dev/null 2>&1; then
    echo "==> Importing images into microk8s containerd..."
    docker save kraken-backend:latest kraken-frontend:latest | microk8s ctr image import -
  elif command -v kind >/dev/null 2>&1; then
    echo "==> Loading images into kind..."
    kind load docker-image kraken-backend:latest kraken-frontend:latest
  elif command -v minikube >/dev/null 2>&1; then
    echo "==> Loading images into minikube..."
    minikube image load kraken-backend:latest
    minikube image load kraken-frontend:latest
  elif command -v ctr >/dev/null 2>&1; then
    echo "==> Importing images into containerd (k8s.io namespace)..."
    docker save kraken-backend:latest kraken-frontend:latest | ctr -n k8s.io images import -
  fi
fi

# Step 2: Kubernetes deployment (runs if no step or step 2 is provided)
if [ -z "$STEP" ] || [ "$STEP" -le 2 ] 2>/dev/null; then
  echo "==> Deploying to Kubernetes..."
  kubectl create namespace kraken --dry-run=client -o yaml | kubectl apply -f -
  kubectl apply -k "$ROOT_DIR/k8s"

  if [ -f "$ROOT_DIR/k8s/02-config-secret.yaml" ]; then
    echo "==> Applying optional k8s/02-config-secret.yaml..."
    kubectl apply -f "$ROOT_DIR/k8s/02-config-secret.yaml"
  else
    echo "==> No k8s/02-config-secret.yaml found; starting in zero-secret mode (configure secrets & admin password at /setup)."
  fi

  kubectl rollout status deployment/backend -n kraken
  kubectl rollout status deployment/frontend -n kraken
  kubectl rollout status deployment/proxy -n kraken
  kubectl -n kraken get pods -w
fi
