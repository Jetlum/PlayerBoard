#!/usr/bin/env bash
# Send a correctly-signed ScoreBoard performance webhook to the local API.
#
# Usage:  ./scripts/send-webhook.sh <value> [event-id]
# Example: ./scripts/send-webhook.sh 20 evt-appearances-20
#
# Signature = base64( HMAC-SHA256( "<timestamp>.<rawBody>", $WEBHOOK_HMAC_SECRET ) )
set -euo pipefail

VALUE="${1:?usage: send-webhook.sh <value> [event-id]}"
EVENT_ID="${2:-evt-$(date +%s%N)}"
BASE="${BASE:-http://localhost:8080}"
SECRET="${WEBHOOK_HMAC_SECRET:-dev-webhook-secret-change-me}"
ATHLETE_ID="${ATHLETE_ID:-11111111-1111-1111-1111-111111111111}"

BODY="{\"athlete_id\":\"$ATHLETE_ID\",\"metric\":\"appearances\",\"value\":$VALUE}"
TS="$(date +%s)"
SIG="$(printf '%s' "$TS.$BODY" | openssl dgst -sha256 -hmac "$SECRET" -binary | base64)"

curl -sS -i -X POST "$BASE/api/v1/webhooks/scoreboard" \
  -H "X-Event-Id: $EVENT_ID" \
  -H "X-Timestamp: $TS" \
  -H "X-Signature: $SIG" \
  -H "Content-Type: application/json" \
  -d "$BODY"
echo
