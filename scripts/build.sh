#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")/.."

rm -rf internal/frontend/dist
mkdir -p internal/frontend/dist

(
  cd web
  bun install
  bun run build
)

cp -R web/dist/. internal/frontend/dist/
go build -o bin/hermes ./cmd/server

