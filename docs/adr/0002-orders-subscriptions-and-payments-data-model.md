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
  | `subscriptions` | `subscriptions`, `subscription_periods`, `price_transitions`, `subscription_price_changes`, `subscription_discounts`, `subscription_notices`, `terms_versions`, `subscription_consents` |
  | `tax` | `tax_records` |
  | `payments` | `payment_methods`, `payment_sessions`, `payment_transactions`, `refund_lines` |
  | `reconciliation` | `gateway_batches`, `gateway_transactions`, `reconciliation_discrepancies` |
  | `platform` | `idempotency_keys`, `outbox_messages`, `inbox_messages`, `audit_events` |

  Foreign keys may cross domains, since there is one database. Go imports may not. Three workflow components
  coordinate several domains in one database transaction, and go-arch-lint lets no domain depend on a workflow:
  - `reconcile` may depend on payments and reconciliation. It owns everything from the gateway: webhooks, sweeps,
    lookups and the nightly comparison. It settles each transaction's status, and in the same transaction writes a
    `payment.outcome` message to the outbox.
  - `checkout` handles checkout orders, returns and refunds.
  - `billing` handles renewals, retries, the notice job and abandonment.

  `checkout` and `billing` may depend on catalog, customers, promotions, orders, subscriptions, tax and payments.
  Each consumes `payment.outcome` through the inbox, for its own orders.
- **Money** is a `bigint` count of cents. Each document that owns amounts (a price, an order, a transaction, a
  tax record) carries one `currency char(3)` with `CHECK (currency = 'USD')` (D-2). Percentage discounts are stored
  in basis points. An amount for one line rounds half up. An amount split across lines uses the largest-remainder method, so the
  parts always add up to the whole.
- **The catalog.** A product is one sellable SKU, with `kind` either `physical` or `digital` (D-10). Recurrence
  belongs to the price, not the product, so a physical subscription box is a physical product with a recurring price. It also carries a shipping address
  and method, and each renewal bills a shipping line. Prices are immutable
  rows. A product may have several live prices, but only one per currency and interval. A price change is always a new row. Retiring a price moves no one; a price transition names where its subscribers
  move to.
- **The order is the frozen record of what was charged.** It holds the lines (products and shipping), each line's
  unit amount, discount, tax and total, the order totals, and snapshots of the billing and shipping addresses.
  Nothing recalculates it. Shipping is a line of kind `shipping`, so tax and refunds treat it like any other line.
  A checkout order bills everything bought at once. After that, each cycle of each subscription gets its own
  renewal order.
- **Fulfilment.** Shipments and returns refer to order lines by quantity, so split shipments and partial returns
  need nothing extra. A one-time digital purchase grants an entitlement. It is revoked once the line's reversals (refunds, or a void
  before settlement) reach the line's full total.
  Subscription access comes from the subscription itself. Stock reservation is a separate issue (D-11), but order
  lines already carry the product and quantity it needs.
- **Payments.**
  - **The log.** Every call the backend makes to the gateway is a `payment_transactions` row, written as `pending`
    before the call. So is every attempt the gateway reports for a hosted session. The kinds are `charge`,
    `authorization`, `capture`, `void`, `refund` and `chargeback`.
  - **Gateway IDs.** A gateway transaction ID is unique only for the kinds that receive one: charges,
    authorizations and refunds. A void or capture points at its parent.
  - **Hosted checkout.** Each checkout is a `payment_sessions` row that owns the invoice number. A new session can
    open only once every earlier one is closed, which means confirmed through reporting to have no unrecorded
    attempt, and once nothing on the order is open or collected. A card is always saved for an order with a
    subscription line.
  - **Collected and reversed.** A charge counts as collected once approved (`approved_at`), whatever happens to it
    later. Refunds, and voids of collected charges, are reversals reported beside the balance. A paid order is
    never reopened, and never charged again. An approval that arrives for an order already paid or cancelled is
    reversed and logged as a discrepancy.
  - **Unknown and held.** A call that times out becomes `unknown`, and a fraud hold becomes `held`. Both count as
    neither collected nor declined. Only one charge the backend sends, and one operation per charge, may be open at a time.
  - **Recovering an unknown charge.** The backend sends `duplicateWindow` 28800 with every charge and refund it
    makes, and resolves an unknown one by resending it unchanged. The duplicate error carries the original
    `transId`, whose details give the outcome. Past the window, it checks the unsettled list and the settled batches
    by invoice number before treating the charge as absent.
  - **Refunds.** Each reversal is split across order lines. Every reversal that hasn't failed counts against a
    line's total. No refund is issued against a disputed or lost chargeback.
- **Subscriptions.**
  - **Periods.** A subscription keeps its billing day (`anchor_day`, clamped to the month's length). Each period is
    linked to the one order line that billed it, and the link is unique on the period's start, so a renewal
    running twice can't bill twice.
  - **Access** lasts through `paid_through`. When a renewal isn't collected by the period's start, for whatever
    reason, access continues through `grace_ends_on`, 7 days later (D-6).
  - **Price changes** are `subscription_price_changes` rows, each scheduled for one renewal of one subscription.
    Each carries out a `price_transitions` decision. A change applies only if its notice was sent between 30 and 7
    days before that renewal, counted in UTC dates and inclusive. Otherwise it moves to a later renewal with a new
    notice. Notices are written 10 to 30 days ahead, which leaves room for mail delays.
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
- **Abandonment.** A checkout order is cancelled 24 hours after it was placed if it has collected nothing, has no
  open attempt and has every session closed. Cancelling it releases its promotion redemption. A renewal whose grace
  has expired is cancelled under the same conditions. A subscription set to cancel at period end ends at that
  date, and is never renewed.

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
  storefront changes in the same release. This refines ADR 0001's rule that "a breaking change fails CI until it is versioned":
  - **While `/v1`'s only consumers are first-party applications released alongside the backend** (today, the
    storefront), a breaking change may stay in `/v1`.
  - **#31 adds `api/accepted-breaking-changes.txt`.** `scripts/check-spec.sh` passes it to `oasdiff breaking` with
    `--err-ignore`. Each entry names one break and the PR that accepted it, so the exception is reviewed in the
    diff, never by turning the check off.
  - **The file is emptied by the next release.**
  - **Once `/v1` has a consumer outside this system,** a breaking change needs `/v2`.
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
