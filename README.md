# backend

The core of the platform: the business domain and the API every frontend uses. It is a modular
monolith, one codebase with clear domain boundaries. That shows sound domain design without the
deployment cost of splitting everything into microservices.

## Responsibilities

- The domains: catalog, customers, carts, orders, inventory, payments, billing, subscriptions and
  users
- A versioned API with a published OpenAPI specification, from which the frontends' SDK is generated
- Authentication, sessions and authorization, including the roles used by the admin application
- The recurring billing engine: plans, subscriptions, invoices, payment attempts, retries, dunning,
  credits, refunds, proration, grace periods and reconciliation
- Payments through the Authorize.Net sandbox, using tokenization
- Marketing: promotion and discount rules, newsletter subscribers and their consent, and marketing
  email
- Domain events, published reliably through a transactional outbox to RabbitMQ
- Background workers, the billing scheduler, the outbox publisher and the real-time WebSocket
  service, all running as processes from this one codebase
- The PostgreSQL schema, migrations, constraints and seed data
- Caching, rate limiting and locks in Valkey (Redis-compatible); search indexing into Meilisearch;
  email
- Audit records for sensitive operations
- A development clock that can be moved forward to simulate billing cycles in minutes instead of
  months

## Does not own

- The infrastructure itself. The backend declares what it needs (database, cache, broker, mail and
  search), and the platform provides it locally.
- Payment processing. Authorize.Net authorizes, captures, settles and tokenizes.
- Presentation. The frontends and the design system own that.

## Works with

- **storefront, admin and checkout.** They use the API through the generated SDK and receive
  real-time events.
- **platform.** It runs the backend and its dependencies locally.

## Getting started

You need Go 1.27 and Docker. The other tools (sqlc, golangci-lint, go-arch-lint, oasdiff, hadolint,
actionlint) run in pinned containers from `compose.tools.yaml`, so there is nothing else to install.

```sh
make test       # unit and integration tests; Postgres runs in a throwaway container
make lint       # every static check CI runs
make fragment   # build the image and run it the way the platform does
make help       # all the targets
```

To run the backend with the rest of the system, use the platform: it includes this repository's
`compose.platform.yaml` and serves the API at `https://api.rsl-commerce.test`.

## Commands

The image runs one binary, `backend`, with a subcommand per process:

| Command       | What it does                                                                 |
| ------------- | ---------------------------------------------------------------------------- |
| `serve`       | Runs the HTTP API on `$PORT` (default 8080); drains for up to 10 s on SIGTERM |
| `migrate`     | Applies pending migrations from the binary; safe to run on every start       |
| `seed`        | Upserts the development products; running it again changes nothing          |
| `healthcheck` | Exits 0 if the API reports healthy; the image's health check (no shell)      |
| `openapi`     | Prints the OpenAPI 3.1 document; needs no database                           |

## Configuration

| Variable                 | Required by               | Meaning                                                  |
| ------------------------ | ------------------------- | -------------------------------------------------------- |
| `DATABASE_URL`           | `serve`, `migrate`, `seed` | A `postgres://` URL **without** a password                |
| `DATABASE_PASSWORD_FILE` | `serve`, `migrate`, `seed` | A file holding the password (a Compose secret)            |
| `PORT`                   | `serve`, `healthcheck`    | The API's port; default 8080                             |

The password never appears in a URL, the environment, logs or errors.

## The API and its SDK

`api/openapi.json` is generated from the code (`make spec`) and committed; CI fails if it drifts or
breaks the previous contract (`make check-spec`). The frontends use the typed SDK built from it,
`@reference-systems-lab/commerce-api`, published privately to GitHub Packages:

```sh
# .npmrc in the consuming repository
@reference-systems-lab:registry=https://npm.pkg.github.com
```

```ts
import { createClient } from "@reference-systems-lab/commerce-api";

const api = createClient({ baseUrl: "https://api.rsl-commerce.test" });
const { data } = await api.GET("/v1/products", { params: { query: { limit: 20 } } });
```

Installing it needs a token that can read packages: `GITHUB_TOKEN` in CI (the package grants the
repository read access), or a classic personal access token with `read:packages` on your machine.

## Releasing

Publish a GitHub release tagged `vX.Y.Z` on a commit on `main` (`gh release create vX.Y.Z
--generate-notes`). The release workflow then, stopping at the first failure:

1. checks the tag and that its commit is on `main`;
2. builds and type-checks the SDK, with no write access;
3. scans the image with Grype, stopping on a critical finding, then pushes
   `ghcr.io/reference-systems-lab/commerce-backend:X.Y.Z` for amd64 and arm64 with an SBOM and a
   signed build-provenance attestation;
4. publishes the SDK at `X.Y.Z`, with an attested tarball;
5. attaches `openapi.json` to the release.

A rerun never republishes a version that is already on GHCR or GitHub Packages.

The platform runs whatever digest its wiring file pins, so a release takes effect when the platform
bumps that pin. Verify an artifact with `gh attestation verify --owner Reference-Systems-Lab`.

## Decisions

- [ADR 0001: The backend's stack](docs/adr/0001-backend-stack.md)

## Git hooks

Run `.githooks/setup` once after cloning. It turns on the committed hooks, which use
[git-secrets](https://github.com/awslabs/git-secrets#installing-git-secrets) to refuse any commit
that contains a secret.

## Status

The walking skeleton: health, the products list, the OpenAPI contract, the SDK, the image and the
release. The other domains follow.

## License

[MIT](LICENSE)
