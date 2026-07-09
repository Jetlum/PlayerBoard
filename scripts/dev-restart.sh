#!/usr/bin/env bash
# Rebuild and restart the api + worker after a code change.
#
# Why this exists: web/*.html and migrations/*.sql are compiled INTO the api binary via
# go:embed, and Go source changes obviously need a rebuild too — a browser refresh alone
# never picks up a code change. This script is the one command for "I edited something, now
# show me the result": stop whatever's running, rebuild both binaries, start them again.
#
# Requires: postgres + nats already up (docker compose up -d postgres nats).
# Usage: ./scripts/dev-restart.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

: "${DATABASE_URL:=postgres://player:player@localhost:5544/playerboard?sslmode=disable}"
: "${NATS_URL:=nats://localhost:4222}"
: "${JWT_SECRET:=dev-jwt-secret-change-me}"
: "${WEBHOOK_HMAC_SECRET:=dev-webhook-secret-change-me}"
: "${SIGNATURE_SCHEME:=hmac}"
: "${DEV_MODE:=true}"
: "${PORT:=8080}"
: "${WORKER_POOL_SIZE:=4}"
export DATABASE_URL NATS_URL JWT_SECRET WEBHOOK_HMAC_SECRET SIGNATURE_SCHEME DEV_MODE PORT WORKER_POOL_SIZE

echo "==> stopping any running api/worker"
pkill -f './bin/api'    2>/dev/null || true
pkill -f './bin/worker' 2>/dev/null || true
sleep 1

echo "==> rebuilding (picks up Go source, embedded web/*.html, embedded migrations/*.sql)"
go build -o bin/api    ./cmd/api
go build -o bin/worker ./cmd/worker

mkdir -p tmp/logs
echo "==> starting api    (applies any new migrations on boot) — log: tmp/logs/api.log"
nohup ./bin/api    > tmp/logs/api.log    2>&1 &
echo "==> starting worker — log: tmp/logs/worker.log"
nohup ./bin/worker > tmp/logs/worker.log 2>&1 &

sleep 2
echo "==> health check"
curl -s -o /dev/null -w "  /healthz -> %{http_code}\n" "http://localhost:${PORT}/healthz" || true

echo
echo "PlayerBoard : http://localhost:${PORT}/"
echo "ClubBoard   : http://localhost:${PORT}/club.html"
echo "logs        : tail -f tmp/logs/api.log tmp/logs/worker.log"
