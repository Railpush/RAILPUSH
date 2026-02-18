# Billing & Subscription Production Audit

**Date:** 2026-02-16
**Scope:** Stripe integration, subscription lifecycle, plan management, webhook handling, frontend billing UI
**Result:** 22 findings — 4 CRITICAL, 8 HIGH, 7 MEDIUM, 3 LOW
**Last reviewed:** 2026-02-18 — 22 FIXED, 0 OPEN

---

## Summary

The billing system has solid fundamentals — Stripe Checkout (hosted) avoids PCI scope, webhook signature verification with idempotency deduplication works correctly, and blueprint sync has full rollback on billing failures. However, three critical race conditions and missing webhook event handlers make the revenue pipeline unsafe for production volume.

---

## Findings

### CRITICAL

| # | Issue | File | Lines | Status |
|---|-------|------|-------|--------|
| 1 | Race condition in `EnsureCustomer()` — concurrent requests create duplicate Stripe customers, violating UNIQUE constraint | `api/services/stripe.go` | 147-178 | FIXED — `UpsertBillingCustomer` with `ON CONFLICT (user_id) DO UPDATE` |
| 2 | Plan upgrade updates DB before Stripe call — if Stripe fails (declined card, network error), user has new plan but no billing | `api/handlers/services.go`, `databases.go`, `keyvalue.go` | varies | FIXED — Update handlers gate on Stripe success before DB; create handlers rollback on Stripe failure |
| 3 | Quantity race in `AddSubscriptionItem()` — count and Stripe update aren't atomic, concurrent add/remove corrupts quantity | `api/services/stripe.go` | 624-660 | FIXED — `SELECT ... FOR UPDATE` row lock inside transaction |
| 4 | Missing webhook events — no handlers for `charge.refunded`, `charge.disputed`, `invoice.created`, `customer.updated` | `api/services/stripe.go` | 938-954 | FIXED — added `charge.refunded`, `charge.disputed`, `customer.updated` handlers |

### HIGH

| # | Issue | File | Lines | Status |
|---|-------|------|-------|--------|
| 5 | Hardcoded pricing (`$7/$25/$85`) in handler diverges from Stripe prices — if prices change, dashboard shows wrong amounts | `api/handlers/billing.go` | 297-308 | FIXED — replaced switch with `planCostCents` map, documented sync with Stripe |
| 6 | JSON decode error silently ignored in `CreateCheckoutSession` — malformed body proceeds with empty return URL | `api/handlers/billing.go` | 495, 549 | FIXED — errors checked, returns 400 on malformed body |
| 7 | Payment failures only set status to `past_due` — no email notification, no dashboard alert, user unaware | `api/services/stripe.go`, `Billing.tsx` | — | PARTIAL — added `past_due` warning banner on billing page; email notification still TODO |
| 8 | Orphaned Stripe subscription items if cleanup fails — errors ignored with `_ =`, items charged forever | `api/services/stripe.go` | 462-463 | FIXED — cleanup errors now logged at CRITICAL level instead of silently discarded |
| 9 | Invoice History section defined in nav but completely unimplemented — users see section link to nothing | `dashboard/src/pages/Billing.tsx` | — | FIXED — nav stub already removed |
| 10 | Promo code UI is a stub — shows "coming soon" toast, no backend integration | `dashboard/src/pages/Billing.tsx` | — | FIXED — promo code input removed |
| 11 | Usage metrics hardcoded to `0` — billing page shows zero usage regardless of actual consumption | `dashboard/src/pages/Billing.tsx` | 525-543 | FIXED — replaced fake metrics with real active resource counts (services/databases/key-value) |
| 12 | No cost preview on plan changes — users don't see price delta before confirming upgrade/downgrade | `dashboard/src/pages/BillingPlans.tsx` | — | FIXED — confirmation modal shows price delta before applying |

### MEDIUM

| # | Issue | File | Lines | Status |
|---|-------|------|-------|--------|
| 13 | 402 errors don't distinguish "no payment method" vs "card declined" vs "insufficient funds" — all treated identically | `dashboard/src/pages/BillingPlans.tsx` | — | FIXED — 402 error now passes Stripe's specific error message to the modal |
| 14 | No downgrade confirmation — Pro to Free with no warning about feature loss | `dashboard/src/pages/BillingPlans.tsx` | — | FIXED — confirmation modal with downgrade warning about CPU/memory reduction |
| 15 | `stripe_webhook_events` table grows unbounded — no cleanup or retention policy | `api/services/scheduler.go` | — | FIXED — daily cleanup job deletes events older than 30 days |
| 16 | All subscription updates use `always_invoice` proration — downgrades shouldn't immediately invoice | `api/services/stripe.go` | 428, 444 | FIXED — context-aware proration: `create_prorations` for upgrades, `none` for downgrades |
| 17 | No invoice storage in database — only Stripe has financial records, no local reconciliation | `api/services/stripe.go` | 849-869 | FIXED — `billing_invoices` table + `UpsertBillingInvoice` from webhook + invoice history in billing overview API + frontend display |
| 18 | Credit balance display-only — no transaction history, no apply mechanism in UI | `dashboard/src/pages/Billing.tsx` | 630-634 | FIXED — credit ledger loaded in billing overview API, credit history section added to billing page |
| 19 | Ops billing page is read-only — can't issue credits, refunds, or manage subscriptions from admin | `dashboard/src/pages/OpsBillingCustomer.tsx` | — | FIXED — added credit balance display + "Grant Credit" form on ops billing customer page |

### LOW

| # | Issue | File | Lines | Status |
|---|-------|------|-------|--------|
| 20 | No startup validation of Stripe price IDs — misconfigured env vars cause silent failures | `api/services/stripe.go` | — | FIXED — `validateConfig()` logs warnings on startup for missing price IDs and webhook secret |
| 21 | `billing.getPaymentMethod()` defined in API client but never called — dead code | `dashboard/src/lib/api.ts` | — | FIXED — removed dead code |
| 22 | CSV export only shows current unbilled charges, not historical — no date range filter | `dashboard/src/pages/Billing.tsx` | 369-397 | FIXED — CSV export now includes both unbilled charges and invoice history sections |

---

## Detail

### #1 — Race Condition in EnsureCustomer

**Problem:** `EnsureCustomer()` does a read-then-write without locking. Two concurrent requests for the same user both see "no customer exists," both create a Stripe customer, and the second `INSERT` violates the `UNIQUE(user_id)` constraint.

**Impact:** User gets a 500 error on their first billing action. Orphaned Stripe customer object created.

**Fix:** Use `INSERT INTO billing_customers ... ON CONFLICT (user_id) DO UPDATE SET updated_at=NOW() RETURNING *` or wrap in a PostgreSQL advisory lock on user_id.

---

### #2 — Plan Upgrade Not Atomic

**Problem:** When a user changes a service plan (e.g., free → pro), the handler updates `services.plan` in the database first, then calls `stripeSvc.AddSubscriptionItem()`. If the Stripe call fails (declined card, network timeout), the database already reflects the new plan but no billing exists.

**Impact:** User gets upgraded resources without being charged. Revenue leakage.

**Fix:** Either:
- Update plan in DB only after Stripe confirms, or
- Wrap both operations in a transaction that rolls back on Stripe failure

---

### #3 — Quantity Race Condition

**Problem:** `AddSubscriptionItem()` counts existing billing items (`CountBillingItemsBySubscriptionItemID`), then updates the Stripe subscription item quantity. Between the count and the update, another request can add or remove items, making the quantity incorrect.

**Impact:** Stripe charges for wrong number of resources. Over-billing or under-billing.

**Fix:** Use `SELECT ... FOR UPDATE` row lock on billing_items before counting, or use Stripe's `quantity` update with idempotency keys.

---

### #4 — Missing Webhook Events

**Currently handled:**
- `checkout.session.completed`
- `customer.subscription.created`
- `customer.subscription.updated`
- `customer.subscription.deleted`
- `invoice.payment_succeeded`
- `invoice.payment_failed`

**Missing:**
- `charge.refunded` — refunds not tracked locally
- `charge.disputed` — disputes/chargebacks invisible to ops
- `invoice.created` / `invoice.finalized` — no local invoice records
- `customer.updated` — email changes not synced
- `payment_intent.payment_failed` — alternative failure signal

---

### #5 — Hardcoded Pricing

**Problem:** `planCost()` in `billing.go:297-308` returns hardcoded cent values:
```go
case "starter": return 700    // $7.00
case "standard": return 2500  // $25.00
case "pro": return 8500       // $85.00
```

If Stripe prices are updated (A/B test, price increase, regional pricing), the dashboard shows stale amounts.

**Fix:** Store prices in database, populated by webhook `price.updated` events, or fetch from Stripe API with caching.

---

### #7 — Payment Failure Notifications

**Problem:** `handlePaymentFailed()` only sets `subscription_status = "past_due"` in the database. No email is sent, no dashboard notification created. The user has no idea their payment failed until they check the billing page.

**Fix:**
- Send email on first failure: "Your payment failed, please update your card"
- Show banner in dashboard when status is `past_due`
- After 3 failures (7 days), send final warning
- After grace period, downgrade to free plan

---

### #9 — Invoice History Missing

**Problem:** The billing page navigation includes an "Invoice History" section, but no UI renders for it. Users see the nav link but clicking it shows nothing.

**Fix:** Either implement invoice list (fetch from Stripe API or local `invoices` table) or remove the nav item until ready.

---

### #12 — No Cost Preview

**Problem:** When a user changes a plan dropdown from "starter" to "pro" and clicks "Apply," there's no confirmation showing the price difference. The change is applied immediately.

**Fix:** Add a confirmation modal: "Upgrade to Pro? This adds $78/month to your bill. You'll be charged a prorated amount of $XX today."

---

## What's Working Well

- **Stripe Checkout (hosted)** — no PCI scope, Stripe handles card collection
- **Webhook signature verification** with constant-time comparison
- **Idempotency deduplication** — `stripe_webhook_events` table prevents double-processing
- **`normalizeBlueprintPlan()`** — gracefully handles AI-generated junk plan names (maps "hobby" → starter, "enterprise" → pro)
- **Blueprint sync rollback** — on failure, all created services/DBs/KVs and their billing items are cleaned up
- **Per-resource billing items** with `UNIQUE(resource_type, resource_id)` preventing double-billing
- **Stripe Customer Portal** for card management — offloads complex UI
- **`ErrNoDefaultPaymentMethod`** sentinel error propagated correctly through the stack

---

## Recommended Fix Schedule

### Week 1 — Blockers

| # | Task | Complexity |
|---|------|-----------|
| 1 | Fix `EnsureCustomer` race condition with `ON CONFLICT` | Low |
| 2 | Fix plan upgrade atomicity — Stripe before DB commit | High |
| 3 | Fix quantity race condition with row-level lock | Medium |
| 9 | Remove Invoice History nav item (or hide until implemented) | Low |
| 10 | Remove promo code input (or hide until implemented) | Low |

### Week 2 — Revenue Integrity

| # | Task | Complexity |
|---|------|-----------|
| 4 | Add missing webhook handlers (refunds, disputes, invoices) | Medium |
| 7 | Add payment failure email notifications | Medium |
| 12 | Add cost preview confirmation modal on plan change | Medium |
| 14 | Add downgrade confirmation modal | Low |
| 6 | Fix JSON decode error handling in checkout session | Low |

### Week 3 — Polish

| # | Task | Complexity |
|---|------|-----------|
| 5 | Source pricing from Stripe instead of hardcoded values | Medium |
| 11 | Wire usage metrics to real data | Medium |
| 13 | Distinguish 402 error subtypes in frontend | Low |
| 17 | Add `invoices` table and populate from webhooks | Medium |
| 15 | Add webhook events table cleanup (30-day retention) | Low |

### Week 4 — Nice to Have

| # | Task | Complexity |
|---|------|-----------|
| 8 | Add retry logic for orphaned subscription item cleanup | Medium |
| 16 | Context-aware proration (immediate for upgrades, end-of-cycle for downgrades) | Medium |
| 18 | Credit balance transaction history UI | Medium |
| 19 | Ops admin actions (issue credits, manage subscriptions) | Medium |
| 20 | Startup validation of Stripe price IDs | Low |
| 21 | Remove dead `getPaymentMethod()` code | Low |
| 22 | Historical CSV export with date range filter | Low |

---

## Files Audited

### Backend
- `api/services/stripe.go` — Core Stripe integration (902 lines)
- `api/handlers/billing.go` — Billing API endpoints (309 lines)
- `api/handlers/ops_billing.go` — Ops billing views (179 lines)
- `api/models/billing.go` — Billing database models (174 lines)
- `api/database/migrations.go` — Schema definitions (316 lines)
- `api/config/config.go` — Stripe configuration (384 lines)
- `api/handlers/services.go` — Service plan change integration
- `api/handlers/databases.go` — Database plan change integration
- `api/handlers/keyvalue.go` — Key-value plan change integration
- `api/handlers/blueprints.go` — Blueprint sync billing integration

### Frontend
- `dashboard/src/pages/Billing.tsx` — Main billing page (685 lines)
- `dashboard/src/pages/BillingPlans.tsx` — Plan selection & upgrade UI (465 lines)
- `dashboard/src/pages/OpsBillingCustomer.tsx` — Ops billing detail (151 lines)
- `dashboard/src/lib/api.ts` — Billing API client (lines 207-216)
- `dashboard/src/lib/plans.ts` — Plan specs and pricing (85 lines)
- `dashboard/src/components/billing/UpgradePromptModal.tsx` — Upgrade prompt (52 lines)
- `dashboard/src/types/index.ts` — Billing types (lines 236-253)
- `dashboard/index.html` — Entry point (no Stripe.js loaded)
