#!/bin/bash

set -euo pipefail

STEP="${1:-}"

# Step 1: Build Docker images (runs if no step or step 1 is provided)
if [ -z "$STEP" ] || [ "$STEP" -eq 1 ] 2>/dev/null; then
  echo "==> Building Docker images..."
  docker build -t kraken-backend:latest ../backend
  docker build -t kraken-frontend:latest ../frontend
fi

# Step 2: Kubernetes deployment (runs if no step or step 2 is provided)
if [ -z "$STEP" ] || [ "$STEP" -le 2 ] 2>/dev/null; then
  echo "==> Deploying to Kubernetes..."
  # create namespace kraken if necessary
  kubectl create namespace kraken --dry-run=client -o yaml | kubectl apply -f -
  kubectl apply -k ../k8s
  kubectl rollout status deployment kraken-backend -n kraken
  kubectl rollout status deployment kraken-frontend -n kraken
  kubectl -n kraken get pods -w
fi

