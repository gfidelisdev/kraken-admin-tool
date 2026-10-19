#!/bin/sh
set -e

# If docker socket is mounted, dynamically match socket GID to allow kraken user access
if [ -S /var/run/docker.sock ]; then
  SOCKET_GID=$(stat -c '%g' /var/run/docker.sock 2>/dev/null || echo 0)
  if [ "$SOCKET_GID" != "0" ]; then
    SOCKET_GROUP=$(getent group "$SOCKET_GID" | cut -d: -f1 || true)
    if [ -z "$SOCKET_GROUP" ]; then
      SOCKET_GROUP="dockersock"
      addgroup -g "$SOCKET_GID" "$SOCKET_GROUP" 2>/dev/null || true
    fi
    addgroup kraken "$SOCKET_GROUP" 2>/dev/null || true
  else
    addgroup kraken root 2>/dev/null || true
  fi
fi

# Ensure /home/kraken/.kube directory exists for kraken user
mkdir -p /home/kraken/.kube 2>/dev/null || true
chown -R kraken:kraken /home/kraken 2>/dev/null || true

# Execute process as non-root user kraken
exec su-exec kraken "$@"
