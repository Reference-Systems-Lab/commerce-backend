# 2. The orders, subscriptions and payments data model

- **Status:** Accepted
- **Date:** 2026-11-22

## Context

The backend owns the catalog, orders, subscriptions, billing and payments. None of them has a schema yet beyond a
skeleton `products` table, which puts the price on the product, and a list endpoint that returns it. The catalog
sells three kinds of product: one-time digital goods, subscriptions, and physical goods shipped to US addresses
(#29 D-1). Nothing is in production. The model is therefore designed from scratch for the best structure, not
patched from what exists (D-5).

A draft schema, dated 2026-11-21, set out the core pattern: an order is one billing event, a subscription is the
standing agreement, and each cycle of each subscription is billed by its own small renewal order. Stripe calls the
per-cycle record an invoice; WooCommerce calls it a renewal order.

#29's research tested the draft against Authorize.Net's sandbox and docs, the California automatic renewal law,
and the tax engines available. Several of the draft's assumptions failed:
- A void or a prior-auth capture gets no transaction ID of its own; it changes the parent's status.
- One Accept Hosted session can produce several gateway transactions: a decline, then a retry, both under the same
  invoice number.
- The fraud filter can hold a charge (response code 4). A held charge is neither paid nor declined. It must be
  resolved, or it expires after 5 days.
- Resending a charge with `duplicateWindow` returns the original transaction ID. This is the safe way to resolve a
  charge whose outcome is unknown.
- Refunds work only on settled charges, and get transaction IDs of their own.
- Under California's rules, a price-change notice must fall 7–30 days before the change for that customer.
  Annual-term plans also need a pre-renewal notice 15–45 days ahead. A single per-price date can't satisfy both
  for monthly and annual cycles.
- Writing a notice row doesn't send it. Under the outbox rule, the row commits before any email goes out.

The model must also follow ADR 0001 and `AGENTS.md`:
- money is integer minor units plus a currency;
- domains depend only on `internal/platform`;
- business code reads time only through the clock;
- events go through the outbox;
- an unknown payment outcome is recorded as unknown;
- money operations are idempotent;
- sensitive operations are audited.

## Decision

The full schema (tables, columns, constraints, state machines, rules and a worked example) is in
[`docs/data-model.md`](../data-model.md). That document is kept current as the migrations land. This record fixes
the decisions that shape it.

- **Domains own tables.** Each table belongs to exactly one `internal/<domain>` package. That package alone writes
  it, through its own sqlc block.

  | Domain | Tables |
  | --- | --- |
  | `catalog` | `products`, `prices`, `shipping_methods` |
  | `customers` | `customers`, `customer_addresses` |
  | `promotions` | `promotions`, `promotion_products`, `promotion_redemptions` |
  | `orders` | `orders`, `order_addresses`, `order_lines`, `shipments`, `shipment_lines`, `returns`, `return_lines`, `entitlements` |
  | `subscriptions` | `subscriptions`, `subscription_periods`, `subscription_price_changes`, `subscription_discounts`, `subscription_notices`, `terms_versions`, `subscription_consents` |
  | `tax` | `tax_records` |
  | `payments` | `payment_methods`, `payment_sessions`, `payment_transactions`, `refund_lines` |
  | `reconciliation` | `gateway_batches`, `gateway_transactions`, `reconciliation_discrepancies` |
  | `platform` | `idempotency_keys`, `outbox_messages`, `inbox_messages`, `audit_events` |

  Foreign keys may cross domains, since there is one database. Go imports may not. Three workflow components
  coordinate several domains in one database transaction: `checkout`, `billing` (renewals, retries, notices and
  abandonment) and `reconcile`. go-arch-lint lets each depend only on the domains it coordinates, and lets no
  domain depend on a workflow.
- **Money** is a `bigint` count of cents. Each document that owns amounts (a price, an order, a transaction, a
  tax record) carries one `currency char(3)` with `CHECK (currency = 'USD')` (D-2). Percentage discounts are stored
  in basis points. Every split of an amount follows a stated rounding rule: round half up per line, and give any
  remainder to the largest line.
- **The catalog.** A product is one sellable SKU, with `kind` either `physical` or `digital` (D-10). Recurrence
  belongs to the price, not the product, so a physical subscription box needs nothing new. Prices are immutable
  rows. A product may have several live prices, but only one per currency and interval. A price change is always a
  new row.
- **The order is the frozen record of what was charged.** It holds the lines (products and shipping), each line's
  unit amount, discount, tax and total, the order totals, and snapshots of the billing and shipping addresses.
  Nothing recalculates it. Shipping is a line of kind `shipping`, so tax and refunds treat it like any other line.
  A checkout order bills everything bought at once. After that, each cycle of each subscription gets its own
  renewal order.
- **Fulfilment.** Shipments and returns refer to order lines by quantity, so split shipments and partial returns
  need nothing extra. A one-time digital purchase grants an entitlement, which a full refund of its line revokes.
  Subscription access comes from the subscription itself. Stock reservation is a separate issue (D-11), but order
  lines already carry the product and quantity it needs.
- **Payments.**
  - **The log.** Every call the backend makes to the gateway is a `payment_transactions` row, written as `pending`
    before the call. So is every attempt the gateway reports for a hosted session. The kinds are `charge`,
    `authorization`, `capture`, `void`, `refund` and `chargeback`.
  - **Gateway IDs.** A gateway transaction ID is unique only for the kinds that receive one: charges,
    authorizations and refunds. A void or capture points at its parent.
  - **Hosted checkout.** Each checkout is a `payment_sessions` row that owns the invoice number.
  - **Unknown and held.** A call that times out becomes `unknown`, and a fraud hold becomes `held`. Both count as
    neither collected nor declined. Only one charge and one refund may be open at a time.
  - **Recovering an unknown charge.** The backend sends `duplicateWindow` 28800 with every charge it makes, and
    resolves an unknown attempt by resending it unchanged.
  - **Refunds.** Each refund is split across order lines. Every refund that hasn't failed counts against a line's
    total.
- **Subscriptions.**
  - **Periods.** A subscription keeps its billing day (`anchor_day`, clamped to the month's length). Each period is
    linked to the one order line that billed it, and the link is unique on the period's start, so a renewal
    running twice can't bill twice.
  - **Access** lasts through `paid_through`. When a renewal isn't collected by the period's start, for whatever
    reason, access continues through `grace_ends_on`, 7 days later (D-6).
  - **Price changes** are `subscription_price_changes` rows, each scheduled for one renewal of one subscription.
    A change applies only if its notice's `sent_at` falls 7 to 30 days before that renewal. Otherwise it moves to
    a later renewal with a new notice.
- **Notices.** `subscription_notices` schedules each notice the California rules require for every subscription:
  - the acknowledgment;
  - the price-change notice;
  - the promo-ending notice, 3 to 21 days before a discount longer than 31 days ends;
  - the pre-renewal notice, 15 to 45 days ahead, for terms of a year or more, which replaces the promo-ending
    notice when both apply;
  - a yearly reminder for every subscription (D-7).

  Notices go out through the outbox. `sent_at` is set when the mail relay accepts the message. Cancellation offers
  at most one save offer, always shown with "click to cancel" (D-8). Consent records are built only if counsel
  confirms the consumer rules apply (D-3). EU consumers are out of scope (D-9).
- **Tax** comes from Stripe Tax's standalone API (D-13):
  - placing an order stores a calculation, whose tax is frozen on the order lines;
  - payment turns it into a tax transaction, with the order number as its reference;
  - an abandoned order's calculation is left to expire;
  - a refund records a reversal with its own reference.
- **Reconciliation.** The gateway's settlement batches and transactions are mirrored as reported, and the
  reconciliation component alone writes them (D-12). A nightly comparison records each unresolved discrepancy once
  per kind and side.
- **Platform tables.**
  - `idempotency_keys` makes checkout and refund requests replay safely.
  - `outbox_messages` carries notices and domain events.
  - `inbox_messages` deduplicates webhooks on `notificationId`, after their HMAC-SHA512 signature is verified.
  - `audit_events` is append-only and names the actor for every staff action on money or access.
- **Abandonment.** A checkout order is cancelled 24 hours after it was placed if it has collected nothing and has
  no open attempt. Cancelling it releases its promotion redemption.

## Alternatives

- **Patching the draft** was rejected under D-5. Its weaknesses are structural:
  - pending-price columns can't express a notice that was never delivered;
  - one unique gateway-ID column can't hold voids, captures or hosted retries;
  - nothing covers shipping, fulfilment or access to digital goods;
  - amounts are `numeric` dollars.
- **Separate models per kind of product.** One-time orders apart from subscription invoices would duplicate the
  payment, tax and refund paths. One order per billing event serves all three kinds.
- **Pending-price columns on the subscription**, the draft's design, can hold only one change and keep no history.
  They also can't require that a notice exists. A row per scheduled change, with a foreign key to its notice, can
  do all three.
- **Voids and captures as status flags only**, with no row of their own, would lose the record of the call. The
  call needs that row for save-before-send and for resolving an unknown outcome.
- **AvaTax.** It matches the draft's tax lifecycle exactly: uncommitted, committed, voided, returned. But its
  sandbox is a 90-day trial capped at 1,000 transactions, and its terms after the trial aren't published. It also
  has no Go SDK. Stripe Tax's sandbox has no end date, and Stripe maintains an official Go SDK.
- **A local tax-rate table** can't follow state differences in taxing digital goods and shipping, clothing
  thresholds, stacked local rates or nexus.
- **A second database role for the gateway mirror** would harden it against the app. It would also add a second
  credential to the platform's contract, for little gain over a package boundary plus tests (D-12).
- **Variant tables now.** A product-plus-variant split would change every foreign key that points at a product.
  Keeping one product per SKU, and adding a grouping table later, keeps those keys stable (D-10).

## Consequences

- **The catalog and its API change incompatibly.** `00001_products` is rewritten, so existing local databases need
  `make reset` on the platform. The `Product` API gains `kind` and `prices[]` in place of `price`, and the
  storefront changes in the same release. The breaking-change check in `scripts/check-spec.sh` is passed
  deliberately: the change is reviewed and named in the PR, not by turning the check off.
- **Checkout depends on webhooks and the reporting API**, because Accept Hosted by redirect returns no result.
  This needs an address the sandbox can reach, plus outbound access and keys for both Authorize.Net and Stripe:
  commerce-platform#36. Until then, sweep jobs over the reporting API find declines and expired holds, which raise
  no webhook of their own.
- **Fraud holds are a normal path.** The sandbox's velocity filter held two of its last ten test charges. Each
  hold is resolved by webhook or by staff, or expires after 5 days, and staff decisions are audited.
- **Stripe Tax calculates tax only where a registration is configured,** so its sandbox needs registrations
  before tax checks mean anything.
- **Some readings of the law are Defaults, not rulings:** D-6 to D-8, and when a price change "takes effect".
  Counsel can change them. Notice kinds and windows are data, not code.
- **Refund behaviour comes from the docs.** The sandbox refund test is the first check in the payments
  implementation, once test charges have settled.
- **Follow-up issues** build the model one domain at a time, in the order the workflows need it.
