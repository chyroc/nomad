#!/usr/bin/env bash
# Build the embedded web view, compile nomad, then run the single
# binary, passing all arguments straight through:
#
#   ./dev.sh                      # start the TUI
#   ./dev.sh -p "hello"           # headless prompt
#   ./dev.sh --output-format stream-json
set -euo pipefail
cd "$(dirname "$0")"

FE_DIR="internal/webview/frontend"

if [ ! -d "$FE_DIR/node_modules" ]; then
  (cd "$FE_DIR" && (npm ci --no-audit --no-fund || npm install --no-audit --no-fund))
fi
(cd "$FE_DIR" && npm run build)

go build -o bin/nomad ./cmd/nomad

exec ./bin/nomad "$@"
