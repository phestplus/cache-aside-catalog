#!/usr/bin/env bash
# End-to-end demo: builds and starts the real stack (app + Redis via
# docker-compose), then runs cmd/simulate against it over the network to
# prove the cache-aside behavior actually holds, not against mocks.
set -euo pipefail

cd "$(dirname "$0")/.."

echo "Starting docker-compose stack (app + redis)..."
docker compose up -d --build

cleanup() {
  echo ""
  echo "Stack is still running. Inspect it with 'docker compose logs -f',"
  echo "or tear it down with 'docker compose down -v'."
}
trap cleanup EXIT

go run ./cmd/simulate "$@"
