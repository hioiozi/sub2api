#!/bin/sh
set -eu

# Run this from the deployment directory that contains docker-compose.yml and .env.
# Example:
#   SUB2API_IMAGE=ghcr.io/hioiozi/sub2api:v3-<sha> sh update-image.sh

IMAGE=${SUB2API_IMAGE:-}
if [ -z "$IMAGE" ]; then
  echo "SUB2API_IMAGE is required" >&2
  exit 2
fi

COMPOSE_FILE=${COMPOSE_FILE:-docker-compose.yml}
SERVICE=${SERVICE:-sub2api}
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
OVERRIDE=$(mktemp)
ROLLBACK_OVERRIDE=$(mktemp)
AUDIT_FILE=${AUDIT_FILE:-sub2api-image-update-${STAMP}.txt}

cleanup() {
  rm -f "$OVERRIDE" "$ROLLBACK_OVERRIDE"
}
trap cleanup EXIT

if ! docker compose -f "$COMPOSE_FILE" config --quiet; then
  echo "compose configuration is invalid" >&2
  exit 1
fi

OLD_IMAGE=$(docker inspect --format '{{.Config.Image}}' "$SERVICE" 2>/dev/null || true)
if [ -z "$OLD_IMAGE" ]; then
  echo "running service $SERVICE was not found" >&2
  exit 1
fi

cat > "$OVERRIDE" <<EOF
services:
  $SERVICE:
    image: $IMAGE
EOF

cat > "$ROLLBACK_OVERRIDE" <<EOF
services:
  $SERVICE:
    image: $OLD_IMAGE
EOF

{
  echo "timestamp=$STAMP"
  echo "service=$SERVICE"
  echo "old_image=$OLD_IMAGE"
  echo "new_image=$IMAGE"
} > "$AUDIT_FILE"

echo "Pulling $IMAGE"
docker compose -f "$COMPOSE_FILE" -f "$OVERRIDE" pull "$SERVICE"
echo "Recreating only $SERVICE"
docker compose -f "$COMPOSE_FILE" -f "$OVERRIDE" up -d --no-deps --force-recreate "$SERVICE"

healthy=0
i=0
while [ "$i" -lt 36 ]; do
  state=$(docker inspect --format '{{.State.Status}}' "$SERVICE" 2>/dev/null || true)
  health=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$SERVICE" 2>/dev/null || true)
  echo "check=$i state=$state health=$health"
  if [ "$state" = running ] && [ "$health" = healthy ]; then
    if wget -q -T 5 -O /dev/null http://127.0.0.1:8080/health; then
      healthy=1
      break
    fi
  fi
  i=$((i + 1))
  sleep 5
done

if [ "$healthy" -ne 1 ]; then
  echo "new image failed health checks; rolling back to $OLD_IMAGE" >&2
  docker compose -f "$COMPOSE_FILE" -f "$ROLLBACK_OVERRIDE" up -d --no-deps --force-recreate "$SERVICE"
  exit 1
fi

NEW_DIGEST=$(docker inspect --format '{{.Image}}' "$SERVICE")
echo "new_digest=$NEW_DIGEST" >> "$AUDIT_FILE"
echo "Deployment verified: $SERVICE is healthy and /health returned 200"
