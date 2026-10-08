# Developer entry points. CI runs the same targets. The tools run in pinned containers
# (compose.tools.yaml); only Go itself is needed on the machine.
.POSIX:
.PHONY: help generate test

TOOLS = UID=$$(id -u) GID=$$(id -g) docker compose -f compose.tools.yaml run --rm --quiet-pull

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' Makefile | sed 's/:.*## /\t/'

generate: ## Regenerate the sqlc query code
	$(TOOLS) sqlc generate

test: ## Unit and integration tests (needs Docker, for testcontainers)
	go test -race ./...
