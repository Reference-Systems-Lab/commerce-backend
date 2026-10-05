# Agent instructions: backend

The backend owns the business domain and the API. Read the root [`AGENTS.md`](../AGENTS.md) as well;
this file wins where the two conflict.

## Boundaries

- Domains stay bounded. One domain never reaches into another's internals.
- Business logic never reads the system clock directly. It goes through the clock abstraction, so
  billing stays deterministic and testable.
- A state change and the events it produces commit in the same database transaction (the outbox).
  Never publish to the broker from inside a request.
- Internal events never reach a browser as they are. The real-time layer authorizes them and turns
  them into browser-safe events first.

## Rules

- Authorization is enforced here, every time. Never trust the client.
- The server decides prices, discounts, tax and totals.
- Never store raw card numbers or CVVs. Payments are sandbox only.
- Payments, invoices, refunds, orders, webhooks and message consumers are idempotent. Retries and
  duplicates must never create a second charge, invoice, refund or order.
- A payment whose outcome is unknown is recorded as unknown and reconciled later. Never guess.
- Sensitive operations write audit records, and audit records are not casually mutable.
