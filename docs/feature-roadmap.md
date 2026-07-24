# Feature Roadmap — carrier-gateway

## Overview

The actual priority for this gateway is primary-feature completeness per
carrier: book, update, cancel, track, and return a shipment (see the "Feature
tiers" section in `CLAUDE.md`). Closing primary-method gaps on carriers still
in Beta matters more than any of the four features below — check
`docs/carriers.md` and `docs/implementation-status.md` for the current
per-carrier gap list before picking up anything here.

The four features documented in this file are all secondary and are **not
currently prioritized**. They're kept here as a spec for later, not as an
active plan:

```
- Batch booking       — book multiple shipments in one call. Not started.
- Pickup scheduling   — tell the carrier when and where to collect. Already
                         shipped per-carrier (BookPickup/UpdatePickup/
                         CancelPickup) — see docs/carriers.md. This section is
                         now historical reference, not a gap.
- Manifest            — document what went on the truck. Already shipped
                         per-carrier (CloseManifest) — see docs/carriers.md.
                         Also historical reference, not a gap.
- Tracking events     — normalized status updates per parcel, subscription
                         model. Not started. Requires a stateful companion
                         service (see docs/parcel-poller.md).
```

Pickup scheduling and manifest are implemented today as per-carrier
`ManifestAdapter` methods (`POST /api/pickups`, `POST /api/manifests`, etc.) —
see `docs/carriers.md` for which carriers support which. When this roadmap was
written those were still gateway-wide gaps; they no longer are. The sections
below are left intact as detailed reference for anyone extending pickup/manifest
support to a carrier that doesn't have it yet, and as an unbuilt spec for batch
booking and tracking events if either becomes a priority later.

See `manifest-pickup-requirements.md` for full detail on pickup scheduling and
manifest specifically.

---

## `GET /api/health`

Returns the operational status of the gateway. Intended for load balancer
health checks and ops dashboards. Always returns `200 OK` while the process
is reachable.

### Response

```json
{
  "status": "ok",
  "uptime": "3h22m10s",
  "mockMode": false,
  "carriers": {
    "postnord": "production",
    "bring": "production",
    "gls": "beta"
  }
}
```

| Field | Type | Description |
|---|---|---|
| `status` | string | Always `"ok"` |
| `uptime` | string | Time since process start, e.g. `"3h22m10s"` |
| `mockMode` | bool | `true` when `MOCK_MODE=true` — no real carrier API calls are made |
| `carriers` | object | Each registered carrier mapped to its mode: `"production"`, `"mock"`, or `"beta"` |

A carrier is `"mock"` when `mockMode` is `true`, `"beta"` when flagged via
`adapter.IsBeta`, and `"production"` otherwise.

---

## 1. Batch booking — not currently prioritized

Kept as an unbuilt spec, not an active plan. Primary-feature completeness per
carrier takes precedence — see the Overview above.

### Workflow

Shipments are booked individually or in groups throughout the day as orders
are picked and packed. The batch endpoint is not an end-of-day operation — it
fires whenever the caller has a set of ready shipments and wants to book them
in a single call. A single batch will typically contain a mix of carriers
reflecting the day's order mix.

```
Orders packed  →  POST /api/bookings/batch  →  Labels printed per parcel
(throughout day)   (mixed carriers)             (immediately, per result)
```

Errors surface per shipment at booking time, while the packer is still at the
station — not at end of day when the driver is already waiting.

### Concurrency model

The batch is fanned out concurrently, bucketed per carrier. Each carrier runs
its own worker pool so a slow or degraded carrier does not block the others.

```
Batch of 100 shipments
  ├── PostNord (50)  → pool capped at 10  → 5 rounds
  ├── Bring (30)     → pool capped at 10  → 3 rounds   ← runs in parallel
  └── GLS (20)       → pool capped at 10  → 2 rounds   ← runs in parallel
```

Total wall time ≈ max(carrier wall time), not sum. A GLS outage fails only the
GLS portion; PostNord and Bring results are still returned.

### Request

`POST /api/bookings/batch`

```json
{
  "shipments": [
    {
      "idempotencyKey": "order-1001",
      "carrier": "postnord",
      "shipment": {
        "sender": { "name": "Unisport Group", "street": "Industrivej", "houseNumber": "10", "city": "Copenhagen", "postalCode": "2300", "country": "DK" },
        "receiver": { "name": "Anna Svensson", "street": "Storgatan", "houseNumber": "1", "city": "Stockholm", "postalCode": "11122", "country": "SE" },
        "deliveryType": "home",
        "totalWeight": 1.2,
        "colli": [
          { "id": "box-1001", "weight": 1.2, "items": [{ "description": "Football boots", "weight": 1.2, "quantity": 1, "value": 129.95 }] }
        ]
      }
    },
    {
      "idempotencyKey": "order-1002",
      "carrier": "bring",
      "shipment": {
        "sender": { "name": "Unisport Group", "street": "Industrivej", "houseNumber": "10", "city": "Copenhagen", "postalCode": "2300", "country": "DK" },
        "receiver": { "name": "Lars Hansen", "street": "Kirkegata", "houseNumber": "5", "city": "Oslo", "postalCode": "0153", "country": "NO" },
        "deliveryType": "home",
        "totalWeight": 0.8,
        "colli": [
          { "id": "box-1002", "weight": 0.8, "items": [{ "description": "Running shoes", "weight": 0.8, "quantity": 1, "value": 89.95 }] }
        ]
      }
    }
  ]
}
```

| Field | Type | Description | Required |
|---|---|---|---|
| `shipments` | array | List of shipment requests, max 50 | Yes |
| `shipments[].idempotencyKey` | string | Caller-assigned deduplication key, max 64 chars | Yes |
| `shipments[].carrier` | string | Carrier key | Yes |
| `shipments[].shipment` | object | Same shape as `POST /api/bookings` shipment body | Yes |

A batch exceeding 50 shipments returns `413 Request Entity Too Large`.

### Response

Always `200 OK`. Partial failure is normal — check `failed` for per-shipment
errors. The caller should retry failed shipments individually via
`POST /api/bookings`.

```json
{
  "succeeded": [
    {
      "idempotencyKey": "order-1001",
      "carrier": "postnord",
      "trackingNumber": "00073215400599388772",
      "shipmentId": "1234567890",
      "status": "booked",
      "colli": [
        { "id": "box-1001", "trackingNumber": "00073215400599388772", "labelUrl": "JVBERi0xLj...", "status": "booked" }
      ]
    }
  ],
  "failed": [
    {
      "idempotencyKey": "order-1002",
      "carrier": "bring",
      "error": "Bring API: invalid postalCode 0153 for country NO"
    }
  ],
  "summary": {
    "total": 2,
    "succeeded": 1,
    "failed": 1
  }
}
```

| Field | Type | Description |
|---|---|---|
| `succeeded` | array | Successfully booked shipments. Each entry mirrors the `POST /api/bookings` response, with `idempotencyKey` added. |
| `failed` | array | Failed shipments with per-item error message and the `idempotencyKey` and `carrier` for retry. |
| `summary.total` | int | Total shipments submitted |
| `summary.succeeded` | int | Count booked successfully |
| `summary.failed` | int | Count that failed |

### Code changes

**New files:**

| File | Purpose |
|---|---|
| `internal/handler/batch.go` | `POST /api/bookings/batch` handler |

**Existing files touched:**

| File | Change |
|---|---|
| `internal/router/router.go` | Wire `POST /api/bookings/batch` |
| `internal/adapter/adapter.go` | No interface change — batch handler calls existing `CarrierAdapter.Book` per shipment |

The batch handler owns all concurrency logic. The adapter layer is unchanged —
each shipment in the batch is booked via the same `Book` call used by the
single-booking endpoint. Per-carrier worker pools are managed inside
`batch.go` using a semaphore per carrier key.

### Validation

- Maximum 50 shipments per batch; `413` above that.
- Each shipment in the batch is validated using the same rules as
  `POST /api/bookings` before any carrier calls are made. Validation failures
  are returned in `failed` without making a carrier API call.
- `idempotencyKey` is required per shipment and must be unique within the batch.
  Duplicate keys within the same batch return `400`.

---

## 2. Pickup scheduling — already shipped

**This has already shipped per-carrier** (`BookPickup`/`UpdatePickup`/
`CancelPickup` on `ManifestAdapter`) — see `docs/carriers.md` for which
carriers support it and which don't (as a confirmed carrier limitation, not a
gap). The detail below is kept as reference spec, not an open item.

See `manifest-pickup-requirements.md` for full endpoint specification, carrier
fit/gap table, and code changes.

### Summary

```
POST   /api/pickups                       Book a collection
PUT    /api/pickups/{confirmationNumber}  Update time window or date
DELETE /api/pickups/{confirmationNumber}  Cancel
```

Done once per carrier per day, typically mid-morning after the first wave of
bookings. Carriers that have a standing daily collection agreement do not need
this call.

---

## 3. Manifest — already shipped

**This has already shipped per-carrier** (`CloseManifest` on
`ManifestAdapter`) — see `docs/carriers.md` for which carriers support it and
which don't (as a confirmed carrier limitation, not a gap). The detail below
is kept as reference spec, not an open item.

See `manifest-pickup-requirements.md` for full endpoint specification, carrier
fit/gap table, and code changes.

### Summary

```
POST /api/manifests   Close the day and retrieve the handover document
```

For GLS this must be called before the driver arrives — it acts as the
collection order. For other carriers it retrieves the manifest document
after collection. Carriers without manifest API support return
`"status": "not_supported"`.

---

## 4. Tracking events — not currently prioritized

Kept as an unbuilt spec, not an active plan. Requires a stateful companion
service (see `docs/parcel-poller.md`), which is a bigger commitment than
anything else in this file — lowest priority of the four.

**Design decision, not a gap to close later:** carrier-gateway will never
bundle or require a specific queue/broker (RabbitMQ, SQS, Kafka, etc.) to
implement this. The gateway stays stateless; a subscription/event feature —
if it's ever built — lives entirely in a separate companion service that
owns its own queue and store, exactly as `docs/parcel-poller.md` already
specifies. This is architectural, not a missing feature waiting on
prioritization.

**What's already achievable today, without building any of this:** README's
["Queues and event-driven delivery"](../README.md#queues-and-event-driven-delivery--bring-your-own)
section documents how to get webhook + queue behaviour using only the
endpoints that already exist (`POST /api/notifications`,
`POST /api/trackings/{trackingNumber}` with `previousStatus`) plus a
scheduler and queue you already run. The genuine gaps if you want more than
that — no bulk tracking endpoint, no shipped poller reference implementation,
no built-in webhook retry — are listed there too.

### Problem

The existing `GET /api/trackings/{trackingNumber}` is poll-based: the caller
asks, the gateway asks the carrier, the gateway answers. For a warehouse
shipping hundreds of parcels a day, polling each tracking number is
impractical. The caller needs to be notified when something changes.

The existing `POST /api/trackings/{trackingNumber}` with a `notifications` block
handles a single parcel. This feature extends that to a subscription model
where the caller registers interest in a set of parcels and receives webhooks
as statuses change.

### Normalized statuses

All carrier-specific status strings are mapped to a consistent set before any
webhook is dispatched. This is already implemented for individual tracking —
batch subscriptions use the same mapping.

| Normalized status | Meaning |
|---|---|
| `booked` | Booked, not yet collected |
| `picked_up` | Collected from sender |
| `in_transit` | Moving through carrier network |
| `out_for_delivery` | On the delivery vehicle |
| `delivered` | Delivered to recipient |
| `failed` | Delivery attempt failed |
| `returned` | Returning to sender |
| `delayed` | Delayed relative to original ETA |
| `unknown` | Not in mapping table |

`booked` and `unknown` never trigger a webhook dispatch.

### Workflow

```
Caller registers tracking IDs  →  Gateway polls carriers on schedule
POST /api/trackings/subscribe       (background, per carrier batch)

Status changes detected  →  Gateway dispatches webhook to caller
                             POST {webhookUrl}
```

The gateway polls carriers in the background. The caller does not need to poll.

### `POST /api/trackings/subscribe`

Register a set of tracking numbers for event-driven notification.

#### Request

```json
{
  "subscriptions": [
    {
      "trackingNumber": "00073215400599388772",
      "carrier": "postnord",
      "currentStatus": "picked_up"
    },
    {
      "trackingNumber": "370000000001",
      "carrier": "bring",
      "currentStatus": "booked"
    }
  ],
  "notifications": {
    "webhookUrl": "https://your-service.example.com/hooks/shipments",
    "webhookSecret": "my-secret",
    "events": ["out_for_delivery", "delivered", "failed", "returned", "delayed"]
  }
}
```

| Field | Type | Description | Required |
|---|---|---|---|
| `subscriptions` | array | Tracking numbers to watch, max 200 | Yes |
| `subscriptions[].trackingNumber` | string | Parcel tracking number | Yes |
| `subscriptions[].carrier` | string | Carrier key | Yes |
| `subscriptions[].currentStatus` | string | Last known normalized status. A webhook fires only when the status advances past this. | No |
| `notifications.webhookUrl` | string | HTTPS endpoint to receive events | Yes |
| `notifications.webhookSecret` | string | HMAC-SHA256 signing secret | No |
| `notifications.events` | array | Event filter. Empty means all non-`booked`, non-`unknown` events. | No |

#### Response

```json
{
  "subscriptionId": "sub_a1b2c3d4",
  "accepted": 2,
  "rejected": [],
  "webhookUrl": "https://your-service.example.com/hooks/shipments",
  "expiresAt": "2026-07-12T00:00:00Z"
}
```

Subscriptions expire after 30 days. The caller re-subscribes for long-lived
parcels or those still in transit at expiry.

### `DELETE /api/trackings/subscribe/{subscriptionId}`

Cancel a subscription. The gateway stops polling for all tracking numbers in
the subscription.

```json
{ "subscriptionId": "sub_a1b2c3d4", "status": "cancelled" }
```

### Webhook payload

The gateway POSTs the following to `webhookUrl` on each status change:

```json
{
  "subscriptionId": "sub_a1b2c3d4",
  "trackingNumber": "00073215400599388772",
  "carrier": "postnord",
  "previousStatus": "picked_up",
  "status": "out_for_delivery",
  "originalStatus": "READY_FOR_DELIVERY",
  "estimatedDelivery": "2026-06-13",
  "timestamp": "2026-06-13T08:14:00Z",
  "event": "out_for_delivery"
}
```

Signing and `X-Signature` / `X-Event-Type` headers follow the same rules as
the existing `POST /api/notifications` webhook dispatch.

### Polling strategy

The gateway polls each carrier on a per-carrier schedule tuned to that
carrier's typical update frequency and API rate limits. Polling is grouped by
carrier — all PostNord tracking numbers in active subscriptions are fetched in
one batch call where the carrier API supports it, rather than one call per
parcel.

Parcels in terminal states (`delivered`, `returned`) are automatically
unsubscribed after the webhook is dispatched.

### Stateful requirement

This feature cannot be stateless. Subscriptions, last-known status per parcel,
and poll schedules must be persisted. A backing store (Redis or Postgres) is
required. This is the only feature in this roadmap that requires persistent
state.

If persistent state is not yet available in the deployment, this feature should
be deferred. Features 1–3 remain fully stateless.

### Code changes

**New files:**

| File | Purpose |
|---|---|
| `internal/handler/subscriptions.go` | `POST /api/trackings/subscribe`, `DELETE /api/trackings/subscribe/{id}` |
| `internal/subscription/store.go` | Subscription persistence interface |
| `internal/subscription/poller.go` | Background poll loop, per-carrier batching |
| `internal/subscription/dispatcher.go` | Status change detection and webhook dispatch |

**Existing files touched:**

| File | Change |
|---|---|
| `internal/router/router.go` | Wire subscription routes |
| `internal/notification/` | Reused for webhook dispatch — no changes |
| `cmd/api/main.go` | Start background poller on startup |

**Interface design:**

```
SubscriptionStore   — Create, Get, List, Delete, UpdateStatus
                      implemented by Redis or Postgres adapter

Poller              — runs on a ticker per carrier, calls CarrierAdapter.Track,
                      compares against stored status, hands changes to Dispatcher

Dispatcher          — reuses existing notification.Service for webhook delivery
```

---

## Status

| Feature | Status | Depends on | Stateful |
|---|---|---|---|
| Batch booking | Not prioritized | Nothing — uses existing `CarrierAdapter.Book` | No |
| Pickup scheduling | Shipped per-carrier — see `docs/carriers.md` | `ManifestAdapter` interface | No |
| Manifest | Shipped per-carrier — see `docs/carriers.md` | `ManifestAdapter` interface | No |
| Tracking events | Not prioritized | Batch booking useful but not required; requires backing store | Yes |

None of the above is more urgent than closing primary-method gaps on carriers
still in Beta (`DHL eCommerce UK`, `DPD UK`, `Matkahuolto` as of the last
audit) — see `docs/implementation-status.md`.
