#!/bin/sh
# Fails when api/openapi.json is stale or breaks the base branch's contract (REQ-008).
#   BASE_REF  the ref to compare with (default origin/main; CI passes the PR's base)
set -eu

base_ref=${BASE_REF:-origin/main}

echo "spec: api/openapi.json matches the code"
go run ./cmd/backend openapi | cmp -s - api/openapi.json || {
	echo "error: api/openapi.json is out of date; run 'make spec' and commit it" >&2
	exit 1
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
if ! git show "$base_ref:api/openapi.json" >"$tmp/base.json" 2>/dev/null; then
	echo "spec: $base_ref has no api/openapi.json yet; nothing to compare for breaking changes"
	exit 0
fi
chmod 0755 "$tmp" && chmod 0644 "$tmp/base.json"

echo "spec: no breaking change against $base_ref"
UID=$(id -u) GID=$(id -g) docker compose -f compose.tools.yaml run --rm --quiet-pull \
	-v "$tmp/base.json:/base/openapi.json:ro" \
	oasdiff breaking /base/openapi.json api/openapi.json --fail-on ERR
