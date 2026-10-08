#!/bin/sh
# Every static check CI runs (make lint). The tools run in pinned containers (compose.tools.yaml) with no
# network; Go itself comes from the machine.
set -eu

HOST_UID=$(id -u)
HOST_GID=$(id -g)
GOMODCACHE=$(go env GOMODCACHE)
export HOST_UID HOST_GID GOMODCACHE
tool() { docker compose -f compose.tools.yaml run --rm --quiet-pull "$@"; }

echo "lint: go mod download (the analysers read the cache offline)"
go mod download

echo "lint: go mod tidy leaves go.mod and go.sum unchanged"
go mod tidy -diff

echo "lint: golangci-lint"
tool golangci-lint golangci-lint run ./...

echo "lint: go-arch-lint (package boundaries)"
tool go-arch-lint check --project-path /src

echo "lint: sqlc generated code is current"
tool sqlc diff

echo "lint: hadolint"
tool hadolint Dockerfile

echo "lint: actionlint"
tool actionlint

echo "lint: every action is pinned to a commit SHA with its version"
if grep -rnE '^\s*-?\s*uses:' .github/workflows | grep -vE 'uses: [^@ ]+@[0-9a-f]{40} # v[0-9]+(\.[0-9]+)*$'; then
	echo "error: the uses: lines above aren't pinned as <action>@<40-hex sha> # vX.Y.Z" >&2
	exit 1
fi

echo "lint: spec"
sh ./scripts/check-spec.sh

echo "lint: ok"
