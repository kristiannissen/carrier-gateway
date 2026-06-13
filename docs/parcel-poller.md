# Parcel Poller — Design

## What it is

A lightweight companion service that sits on top of carrier-gateway. It owns
the subscription state and drives the poll loop. carrier-gateway owns all
carrier knowledge — the poller has none of it.

```
Caller  →  POST /subscriptions         Register parcels to watch
Poller  →  POST carrier-gateway/api/trackings/{id}   On schedule, per parcel
carrier-gateway  →  POST {webhookUrl}  On status change
```

## Responsibilities

**Poller owns:**
- Subscription registry (which parcels to watch, for whom, with what webhook config)
- Last known normalized status per parcel
- Scheduling — when to poll each parcel

**carrier-gateway owns:**
- Carrier API calls
- Status normalization
- Webhook signing and dispatch
- All carrier-specific knowledge

The poller calls `POST /api/trackings/{trackingNumber}` on carrier-gateway,
passing `previousStatus` and the `notifications` block. carrier-gateway
compares the current carrier status against `previousStatus` and fires the
webhook if the status has advanced. The poller then updates its stored status
to match.

## Scheduling

The poller requires a mechanism to execute the poll loop on a recurring
schedule. The choice of scheduling tool — cron, a task queue, an internal
ticker, a cloud scheduler — is left to the implementor. The poller exposes
no opinion on this. The only contract is that the poll loop is called
periodically and calls carrier-gateway for each active subscription.

## Storage

The poller requires a persistent store for subscriptions and last-known
statuses. The choice of store — relational database, key-value store,
embedded database — is left to the implementor. The store interface needs
to support the following operations:

- Create a subscription
- Get a subscription by ID
- List active subscriptions, optionally filtered by carrier
- Update the current status of a subscription
- Delete a subscription

## Data model

```json
{
  "id": "sub_a1b2c3d4",
  "trackingNumber": "00073215400599388772",
  "carrier": "postnord",
  "currentStatus": "picked_up",
  "webhookUrl": "https://your-service.example.com/hooks/shipments",
  "webhookSecret": "my-secret",
  "events": ["out_for_delivery", "delivered", "failed"],
  "createdAt": "2026-06-12T09:00:00Z",
  "expiresAt": "2026-07-12T09:00:00Z"
}
```

| Field | Description |
|---|---|
| `id` | Unique subscription identifier |
| `trackingNumber` | Parcel tracking number |
| `carrier` | Carrier key passed to carrier-gateway |
| `currentStatus` | Last normalized status observed. Updated after each successful poll that returns a changed status. |
| `webhookUrl` | Forwarded to carrier-gateway on each poll call |
| `webhookSecret` | Forwarded to carrier-gateway on each poll call |
| `events` | Event filter forwarded to carrier-gateway. Empty means all events. |
| `createdAt` | Subscription creation time |
| `expiresAt` | Subscriptions expire after 30 days. Expired subscriptions are not polled. |

On reaching a terminal status (`delivered`, `returned`, `failed`) the
subscription is deleted after the webhook has been confirmed dispatched by
carrier-gateway.

---

## Endpoints

### `POST /subscriptions`

Register one or more parcels to watch.

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

| Field | Type | Required | Description |
|---|---|---|---|
| `subscriptions` | array | Yes | Parcels to watch, max 200 per call |
| `subscriptions[].trackingNumber` | string | Yes | Parcel tracking number |
| `subscriptions[].carrier` | string | Yes | Carrier key |
| `subscriptions[].currentStatus` | string | No | Last known normalized status. A webhook fires only when the status advances past this. Defaults to `booked` if omitted. |
| `notifications.webhookUrl` | string | Yes | HTTPS endpoint to receive events |
| `notifications.webhookSecret` | string | No | HMAC-SHA256 signing secret |
| `notifications.events` | array | No | Event filter. Empty means all non-terminal events. |

#### Response — `201 Created`

```json
{
  "subscriptionId": "sub_a1b2c3d4",
  "accepted": 2,
  "rejected": [],
  "expiresAt": "2026-07-12T09:00:00Z"
}
```

| Field | Description |
|---|---|
| `subscriptionId` | ID for the subscription group — used to cancel all parcels in this registration at once |
| `accepted` | Count of parcels accepted for watching |
| `rejected` | Any parcels rejected at registration time, with reason |
| `expiresAt` | Expiry of this subscription |

---

### `GET /subscriptions/{subscriptionId}`

Returns the current state of a subscription group.

#### Response — `200 OK`

```json
{
  "subscriptionId": "sub_a1b2c3d4",
  "parcels": [
    {
      "trackingNumber": "00073215400599388772",
      "carrier": "postnord",
      "currentStatus": "out_for_delivery",
      "lastPolledAt": "2026-06-13T08:14:00Z"
    },
    {
      "trackingNumber": "370000000001",
      "carrier": "bring",
      "currentStatus": "in_transit",
      "lastPolledAt": "2026-06-13T08:14:05Z"
    }
  ],
  "expiresAt": "2026-07-12T09:00:00Z"
}
```

---

### `DELETE /subscriptions/{subscriptionId}`

Cancel a subscription group. The poller stops watching all parcels in the group.

#### Response — `200 OK`

```json
{
  "subscriptionId": "sub_a1b2c3d4",
  "status": "cancelled"
}
```

---

## Poll call to carrier-gateway

On each poll cycle, for each active subscription, the poller calls:

```
POST carrier-gateway/api/trackings/{trackingNumber}
```

```json
{
  "carrier": "postnord",
  "previousStatus": "picked_up",
  "notifications": {
    "webhookUrl": "https://your-service.example.com/hooks/shipments",
    "webhookSecret": "my-secret",
    "events": ["out_for_delivery", "delivered", "failed"]
  }
}
```

If the response contains a `normalizedStatus` that differs from
`previousStatus`, the poller updates `currentStatus` in its store.
carrier-gateway has already dispatched the webhook.

If `normalizedStatus` is a terminal state, the subscription is deleted.
