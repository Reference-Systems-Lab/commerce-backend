#!/bin/sh
# Runs compose.platform.yaml the way the platform does (ci/compose.stub.yaml) and checks it (REQ-011):
# migrate completes, the API turns healthy, seeding works, the catalog answers, and every backend
# container is hardened and publishes nothing of its own. Always removes the stack.
#   SKIP_BUILD=1  test the commerce-backend:ci image already built (make fragment builds it first)
set -eu

dir=$(mktemp -d)
CI_SECRETS_DIR=$dir
export CI_SECRETS_DIR
BACKEND_DATABASE_URL="postgres://commerce@postgres:5432/commerce?sslmode=disable"
export BACKEND_DATABASE_URL
compose() { docker compose -f ci/compose.stub.yaml "$@"; }
cleanup() {
	status=$?
	[ "$status" -eq 0 ] || compose logs --no-color >&2 || true
	compose down -v --remove-orphans >/dev/null 2>&1 || true
	rm -rf "$dir"
	exit "$status"
}
trap cleanup EXIT INT TERM

# A throwaway password, readable by the non-root containers like the platform's secret file.
od -An -N16 -tx1 /dev/urandom | tr -d ' \n' >"$dir/postgres_password"
chmod 0644 "$dir/postgres_password"

if [ "${SKIP_BUILD:-}" != 1 ]; then
	docker build -q -t commerce-backend:ci . >/dev/null
fi

echo "fragment: up --wait"
compose up --wait --quiet-pull

echo "fragment: backend-migrate exited 0"
code=$(docker inspect -f '{{.State.ExitCode}}' "$(compose ps -a -q backend-migrate)")
[ "$code" = 0 ] || { echo "error: backend-migrate exited $code" >&2; exit 1; }

echo "fragment: seed"
compose run --rm --no-deps backend-api seed >/dev/null

echo "fragment: GET /health and /v1/products"
curl -fsS http://127.0.0.1:18080/health | grep -qx '{"status":"ok"}'
count=$(curl -fsS 'http://127.0.0.1:18080/v1/products?limit=100' | grep -o '"slug":' | wc -l | tr -d ' ')
[ "$count" = 6 ] || { echo "error: $count products, want 6" >&2; exit 1; }

echo "fragment: hardening"
for svc in backend-migrate backend-api; do
	id=$(compose ps -a -q "$svc")
	got=$(docker inspect -f '{{.Config.User}} ro={{.HostConfig.ReadonlyRootfs}} caps={{.HostConfig.CapDrop}} add={{.HostConfig.CapAdd}} init={{.HostConfig.Init}} priv={{.HostConfig.Privileged}} sec={{.HostConfig.SecurityOpt}}' "$id")
	want='65532:65532 ro=true caps=[ALL] add=[] init=true priv=false sec=[no-new-privileges:true]'
	[ "$got" = "$want" ] || { echo "error: $svc: $got" >&2; exit 1; }
done
# The fragment publishes nothing; only the stub's wiring file publishes the port CI curls.
if grep -nE '^[[:space:]]*(image|ports|networks|depends_on|build):' compose.platform.yaml; then
	echo "error: compose.platform.yaml must not set image, ports, networks, depends_on or build" >&2
	exit 1
fi
# Nor name an infrastructure host: those come from the platform through required variables.
if grep -nE '(postgres|valkey|rabbitmq|meilisearch|mailpit)[:/@]' compose.platform.yaml; then
	echo "error: compose.platform.yaml names an infrastructure host" >&2
	exit 1
fi

echo "fragment: ok"
