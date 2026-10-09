# 1. The backend's stack

- **Status:** Accepted
- **Date:** 2026-10-08

## Context

The backend owns the business domain and the API. Every frontend reaches it only through the API
and the SDK generated from it, and the platform runs it locally from an image and a compose
fragment. The domain needs reliable messaging, Postgres-heavy billing, idempotent payments and
strict authorization; the backend must also be small, secure and quick to start in a container.
The spike (commerce-backend#1) compared Go, TypeScript on Node, Bun, Deno and Rust and decided D1
to D9. The walking skeleton (commerce-backend#2) builds the first slice: health, one catalog
endpoint, the contract, the image, the fragment and the release. This record covers both.

## Decision

- **Language.** Go 1.27, staying on the two supported Go releases (D1). One static binary with
  subcommands: `serve`, `migrate`, `seed`, `healthcheck` and `openapi` now; `worker`, `scheduler`,
  `outbox` and `ws` as those processes arrive. Subcommands are a plain standard-library switch, not
  a CLI framework, to keep the dependency list short (D-4).
- **API contract.** Code-first OpenAPI 3.1 with Huma v2 on the standard `net/http` mux (D2).
  `backend openapi` prints the spec, which is committed as `api/openapi.json`; CI fails when the
  committed file drifts from the code or when `oasdiff` finds a breaking change against the base
  branch. Errors are RFC 9457 problem details. The API is versioned in its path (`/v1`).
- **SDK.** `@reference-systems-lab/commerce-api`: openapi-typescript types plus a typed
  openapi-fetch client, generated from `api/openapi.json` and published privately to GitHub
  Packages at the backend's version, the same registry as the design-system packages (D-1;
  commerce#3 DE-7, DE-10). Consumer repositories get read access per package.
- **Data.** pgx v5, sqlc for queries and goose for migrations, embedded in the binary (D3).
  `migrate` and `seed` are idempotent. Money is an `int64` count of minor units plus an ISO 4217
  currency, with shopspring/decimal only for proration (D4); in JSON it is
  `{amount, currency}` (D-8). Lists page with an opaque keyset cursor, not offsets (D-8).
- **Time.** Business code reads time only through the `Clock` port in `internal/platform/clock`;
  a lint rule refuses `time.Now` anywhere else (D4). The persisted development offset that drives
  billing arrives with the scheduler.
- **Boundaries.** Each domain lives in `internal/<domain>` and may depend only on
  `internal/platform`; go-arch-lint enforces it (D6).
- **Messaging, cache, search.** An outbox table with a relay that claims rows using `SKIP LOCKED`,
  publishing with amqp091-go confirms to quorum queues with a dead-letter exchange and a delivery
  limit, and an inbox table for deduplication (D3). Valkey 9.1 through go-redis and a neutral
  `CACHE_URL` (D8); meilisearch-go. None is used by the skeleton.
- **Payments.** Idempotency-key rows written in the same transaction as the effect; a thin
  Authorize.Net gateway over its JSON API, since there is no Go SDK; an `unknown` state settled by
  a reconciliation job; HMAC-SHA512 webhook checks with deduplication (D5). The checkout uses
  Accept Hosted by redirect. A fake gateway, shipped only as a test image, makes payments testable
  without credentials; without sandbox credentials payments are disabled (D9).
- **Sessions.** Separate customer and staff sessions with scs and our own RBAC: host-only
  `__Host-` cookies, `SameSite=Lax` for customers and `Strict` for staff, exact-origin CORS per
  audience, and an Origin or `Sec-Fetch-Site` check plus a custom header on unsafe methods (D6, D7;
  commerce#3 DE-9).
- **Real time.** coder/websocket serves the browser-safe events (D6). The protocol (URL,
  authentication, event IDs, resync) is still open and gets its own record (D7).
- **Observability.** The OpenTelemetry Go SDK with contrib instrumentation (D6), exporting to the
  platform's `observability` profile when it runs.
- **Configuration and secrets.** Configuration comes from the environment. The database password
  is never in a URL or the environment: `DATABASE_URL` carries none, and `DATABASE_PASSWORD_FILE`
  names the Compose secret that holds it (D-7). Secrets never reach logs, errors or the spec.
- **Container.** A multi-stage build: Go cross-compiles on the build machine for `linux/amd64` and
  `linux/arm64` (no emulation) into `gcr.io/distroless/static-debian13:nonroot` (D6, D-9), every
  base pinned by digest. It runs as `65532`, with `backend healthcheck` as the health check, since
  there is no shell.
- **Running on the platform.** `compose.platform.yaml` declares `backend-migrate`, a one-shot that
  applies migrations, and `backend-api`. It sets no image, ports, networks or dependencies, names no
  infrastructure host, and takes every value the platform provides as a required variable (the
  secret's path is fixed by Compose); the platform's wiring file
  pins the image digest and makes the API wait for the migration (commerce-platform#1 P-D2; D-5,
  D-6).
- **Releases.** A GitHub release `vX.Y.Z` on a commit on `main`, published by hand, runs one
  workflow whose stages stop at the first failure: the SDK is built with no write access; the image
  is scanned with Grype, then pushed to `ghcr.io/reference-systems-lab/commerce-backend` with an
  SBOM, provenance and a build-provenance attestation; the SDK is published at the same version with
  an attested tarball; `openapi.json` is attached to the release (D-2). Only the publishing jobs
  hold write tokens, and a version already published is never pushed again. The first release,
  v0.1.0, is cut by the walking skeleton itself, so the platform and storefront have something to
  run (D-3).
- **Checks.** golangci-lint v2 (with gosec and depguard), go-arch-lint, govulncheck, tests against
  a real Postgres through testcontainers-go, CodeQL for Go and Dependabot for Go modules, Docker,
  npm and actions, every action pinned by commit SHA (D6; commerce#3 DE-4).

## Alternatives

- **TypeScript on Node** was a close second: one language across every repository, better-auth's
  ready-made features and the commoner pairing with Vue. It lost on the messaging client (every
  Node AMQP client is community-maintained; RabbitMQ maintains the Go one), SQL-first tooling and a
  larger supply chain.
- **Bun** has no support policy and breaks OpenTelemetry's module hooks; **Deno**'s support window
  is short and its sources disagree; **Rust** has community AMQP and search clients and beta
  OpenTelemetry for speed an I/O-bound shop doesn't need.
- **Spec-first OpenAPI** (oapi-codegen, ogen) suits a contract with many implementers; this one
  has one, and their OpenAPI 3.1 support is partial or unstated.
- **An ORM** hides the SQL that billing and the outbox depend on; Prisma has no row locks.
- **A CLI framework** (cobra, humacli) for five subcommands adds dependencies for no gain.
- **Running migrations when `serve` starts** races between replicas and hides failures in the API's
  logs; a one-shot service fails visibly and once.
- **Offset pagination** skips or repeats rows while products are added.
- **The public npm registry** for the SDK would need an npm organization and a second registry for
  consumers that already use GitHub Packages.
- **Automated releases** (release-please) need commit-message discipline the team hasn't adopted;
  a hand-published release is one command.

## Consequences

- One small static image (about 5 MB compressed in the spike) with no shell: debugging uses logs,
  `backend healthcheck` and the `:debug` distroless variant, not `docker exec sh`.
- The spec is generated, never hand-edited. Any change to a handler's types shows up in
  `api/openapi.json` in the same pull request, and a breaking change fails CI until it is versioned.
- Frontends install `@reference-systems-lab/commerce-api` with a token that can read packages, as
  they already do for the design system; GitHub Packages has no registry signatures, so consumers
  verify the tarball's attestation.
- A new domain is a new `internal/<domain>` package; the boundary check keeps domains apart until
  an explicit rule allows otherwise.
- The platform runs whatever digest its wiring file pins; a release does nothing until the
  platform bumps that pin.
- `time.Now` in business code fails the lint, so billing can later run on a moved clock.
- The first GHCR push may create a private package; it must be made public once so the platform
  can pull it anonymously.
