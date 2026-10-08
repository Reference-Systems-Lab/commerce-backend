# Developer entry points. CI runs the same targets. The tools run in pinned containers
# (compose.tools.yaml); only Go itself is needed on the machine.
.POSIX:
.PHONY: help generate spec check-spec test image fragment

TOOLS = UID=$$(id -u) GID=$$(id -g) docker compose -f compose.tools.yaml run --rm --quiet-pull

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' Makefile | sed 's/:.*## /\t/'

generate: ## Regenerate the sqlc query code
	$(TOOLS) sqlc generate

spec: ## Regenerate api/openapi.json from the code
	go run ./cmd/backend openapi > api/openapi.json

check-spec: ## Fail if api/openapi.json is stale or breaks the base branch (BASE_REF=origin/main)
	sh ./scripts/check-spec.sh

test: ## Unit and integration tests (needs Docker, for testcontainers)
	go test -race ./...

image: ## Build the image for this machine as commerce-backend:ci
	docker build -t commerce-backend:ci .

fragment: image ## Run compose.platform.yaml the way the platform does, and check it
	IMAGE=commerce-backend:ci sh ./scripts/fragment-smoke.sh
