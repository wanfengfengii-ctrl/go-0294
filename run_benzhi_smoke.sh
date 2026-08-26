#!/usr/bin/env bash
# Deterministic smoke test for the curtain-wall laminated-glass assembly gate.
#
# It builds the server, starts it against a temporary SQLite database, probes
# the live health and design-lock endpoints over HTTP, verifies the duplicate
# identity rejection, then tears down every spawned process and temporary file.
# It performs no external network access and never delegates to `go test`.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PORT="${PORT:-18080}"
BASE="http://127.0.0.1:${PORT}"
WORKDIR="$(mktemp -d)"
SERVER_PID=""

cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

# request METHOD PATH [JSON_BODY] -> sets RESP_STATUS and RESP_BODY.
# The response is captured into variables (never piped into grep) so the
# pipefail shell cannot kill curl with SIGPIPE.
request() {
  local method="$1" path="$2" data="${3:-}"
  local combined
  if [[ -n "$data" ]]; then
    combined="$(curl -s -w $'\n%{http_code}' -X "$method" \
      -H 'Content-Type: application/json' -d "$data" "${BASE}${path}")"
  else
    combined="$(curl -s -w $'\n%{http_code}' -X "$method" "${BASE}${path}")"
  fi
  RESP_STATUS="${combined##*$'\n'}"
  RESP_BODY="${combined%$'\n'*}"
}

echo "building server"
(cd "$ROOT" && go build -o "$WORKDIR/server" ./cmd/server)

echo "starting server on port ${PORT}"
"$WORKDIR/server" -addr ":${PORT}" -db "$WORKDIR/gate.db" \
  -frontend "$ROOT/frontend/dist" &
SERVER_PID=$!

# Wait for the health endpoint to come up, with a bounded retry loop.
ready=0
for _ in $(seq 1 50); do
  request GET /api/health || true
  if [[ "$RESP_STATUS" == "200" ]]; then ready=1; break; fi
  sleep 0.1
done
if [[ "$ready" != "1" ]]; then
  echo "server did not become healthy" >&2
  exit 1
fi

echo "probing health"
request GET /api/health
[[ "$RESP_STATUS" == "200" ]]
[[ "$RESP_BODY" == *'"status":"ok"'* ]]

echo "locking a design"
LOCK_PAYLOAD='{"project":"Tower-A","facade_zone":"F1","plate_number":"P-001","version":1,"rule_digest":"seed","thickness_um":12000,"width_um":100010,"height_um":200010,"edge_margin_um":5,"edge_scheme":"flat-polish","geometry":{"outline":[{"x":5,"y":5},{"x":100005,"y":5},{"x":100005,"y":200005},{"x":5,"y":200005}],"holes":[]},"furnace_lot":"LOT-7","film_batch":"FILM-9","film_opening_um2":1000000,"thresholds":{"surface_stress":1000},"rack":{"furnace_run":"RUN-1","positions":[{"id":"R1","level":1}],"adjacency":[]},"inspection":{"grid":["G1"],"sampling":{"G1":"P-001"},"destructive":1},"locked_generation":0}'
request POST /api/designs/lock "$LOCK_PAYLOAD"
[[ "$RESP_STATUS" == "201" ]]
[[ "$RESP_BODY" == *'"rule_digest"'* ]]
TASK_ID="$(printf '%s' "$RESP_BODY" | sed -n 's/^{"id":"\([^"]*\)".*/\1/p')"
[[ -n "$TASK_ID" ]]

echo "rejecting duplicate plate identity"
request POST /api/designs/lock "$LOCK_PAYLOAD"
[[ "$RESP_STATUS" == "409" ]]
[[ "$RESP_BODY" == *'"code":"IDENTITY_DUPLICATE"'* ]]

echo "listing tasks"
request GET /api/tasks
[[ "$RESP_STATUS" == "200" ]]
[[ "$RESP_BODY" == *"\"$TASK_ID\""* ]]

echo "smoke test passed (task=$TASK_ID)"
