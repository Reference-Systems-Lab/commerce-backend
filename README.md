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
- Caching, rate limiting and locks in Redis; search indexing into Meilisearch; email
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

## Git hooks

Run `.githooks/setup` once after cloning. It turns on the committed hooks, which use
[git-secrets](https://github.com/awslabs/git-secrets#installing-git-secrets) to refuse any commit
that contains a secret.

## Status

Planning. No code yet.

## License

[MIT](LICENSE)
