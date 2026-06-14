# Evri (formerly Hermes UK) — Feature Mapping

API: **Evri Classic API v1.0**
Auth: OAuth2 client credentials (`EVRI_CLIENT_ID` + `EVRI_CLIENT_SECRET` → Bearer token)
Coverage: UK domestic only — all delivery addresses must be valid UK postcodes. No cross-border support.
Implementation status: **Beta** (booking and label retrieval only)

---

## Summary

The Evri Classic API exposes five endpoints: two OAuth endpoints, a customer
info endpoint, a parcel creation endpoint, and a label retrieval endpoint.
This covers `BookShipment` and `FetchLabel`. No tracking, cancellation, or
update endpoint exists in the spec.

---

## Feature fit/gap

### Booking

| Feature | Status | Notes |
|---|---|---|
| Book shipment | ✅ Implemented | `POST /api/parcels` — batch; one Evri parcel per colli |
| Cancel shipment | ❌ Not available | No endpoint in the Evri Classic API |
| Update shipment | ❌ Not available | No endpoint in the Evri Classic API |
| Idempotency key | ✅ Native | `clientUID` field — Evri deduplicates server-side |
| Multi-colli | ✅ Implemented | Each colli maps to one parcel in the batch request |
| Return booking | ❌ Not available | No return endpoint in the Evri Classic API |

### Labels

| Feature | Status | Notes |
|---|---|---|
| Label at booking | ❌ Not returned | `POST /api/parcels` returns a barcode only; label must be fetched separately |
| Fetch label (PDF) | ✅ Implemented | `GET /api/labels/{barcode}?format=DEFAULT` with `Accept: application/pdf` |
| Fetch label (ZPL) | ✅ Implemented | `GET /api/labels/{barcode}?format=THERMAL` with `Accept: application/zpl` |
| Fetch label (PNG) | ❌ Not available | Not offered by the Evri Classic API |
| Fetch label (EPL) | ❌ Not available | Not offered by the Evri Classic API |
| Two-per-page label | ❌ Not wired | Evri supports `TWO_PER_PAGE` format param but no gateway format maps to it |

### Tracking

| Feature | Status | Notes |
|---|---|---|
| Current status | ❌ Not available | No tracking endpoint in the Evri Classic API |
| Event history | ❌ Not available | No tracking endpoint in the Evri Classic API |
| Estimated delivery | ❌ Not available | No tracking endpoint in the Evri Classic API |

Tracking is available via the Evri consumer website (`evri.com/track`) but not
via API. If Evri exposes a tracking API in a future version of their spec,
`TrackShipment` can be implemented without changing the adapter interface.

### Pickup scheduling

| Feature | Status | Notes |
|---|---|---|
| Book pickup | ❌ Not available | No pickup endpoint in the Evri Classic API |
| Update pickup | ❌ Not available | — |
| Cancel pickup | ❌ Not available | — |

### Manifest

| Feature | Status | Notes |
|---|---|---|
| Close manifest | ❌ Not available | No manifest endpoint in the Evri Classic API |

### Add-ons

| Add-on | Status | Notes |
|---|---|---|
| Signature required | ✅ Implemented | Mapped to `signatureRequired` on delivery details |
| Flex delivery / safe place | ✅ Implemented | `AddOnFlexDelivery.Instructions` → `deliverySafePlace` |
| Delivery instructions | ✅ Implemented | `Shipment.ShipmentComment` → `deliveryInstructions` |
| Next-day delivery | ✅ Implemented | `ServiceTier: "next_day"` → `nextDay: true` (account-gated by Evri) |
| SMS notification | ❌ Not available | Evri Classic API has no notification add-on |
| Email notification | ❌ Not available | Evri Classic API has no notification add-on |
| Cash on delivery | ❌ Not available | Not in Evri Classic API |
| Insurance | ❌ Not available | Not in Evri Classic API |

### Other features

| Feature | Status | Notes |
|---|---|---|
| Customs / cross-border | ❌ Not applicable | UK domestic only; no `country` field on Evri address |
| Service point delivery | ❌ Not available | No service point routing in the Evri Classic API |
| Business delivery | ❌ Unknown | Not documented; no separate product code in the spec |
| POSTABLE parcel type | ✅ Implemented | `DeliveryType: "postable"` → `type: POSTABLE` |

---

## Endpoint mapping

| carrier-gateway operation | Evri Classic API endpoint | Status |
|---|---|---|
| `POST /api/bookings` | `POST /api/parcels` | ✅ |
| `GET /api/labels/{id}` | `GET /api/labels/{barcode}` | ✅ |
| `GET /api/trackings/{id}` | — | ❌ → 501 Not Supported |
| `DELETE /api/bookings/{id}` | — | ❌ → 501 Not Supported |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 Not Supported |
| `POST /api/pickups` | — | ❌ → 501 Not Supported |
| `POST /api/manifests` | — | ❌ → 501 Not Supported |

---

## Configuration

| Environment variable | Required | Description |
|---|---|---|
| `EVRI_CLIENT_ID` | Yes | OAuth2 client ID issued by Evri |
| `EVRI_CLIENT_SECRET` | Yes | OAuth2 client secret issued by Evri |

When either variable is absent, the adapter falls back to `MockEvriAdapter`.

---

## Implementation notes

**UK-only.** The Evri Classic API has no `country` field on the delivery
address. All parcels are routed within the UK domestic network. Passing a
non-UK postcode will be rejected by Evri at booking time.

**Name splitting.** Evri requires `firstName` and `lastName` separately.
The adapter splits `Receiver.Name` on the first space using the shared
`splitName` helper. Names without a space are placed entirely in `lastName`.

**Batch booking.** `POST /api/parcels` accepts a list of parcels in one call.
The adapter maps each `Colli` to one Evri parcel. Partially successful batches
(some `CREATED`, some `INVALID`) are handled: errors from `INVALID` parcels are
collected in `BookingResponse.Errors`; the response still contains the barcodes
of successfully created parcels.

**clientUID idempotency.** When `BookingRequest.IdempotencyKey` is set, the
adapter prefixes it with the colli ID (`{key}_{colliID}`) to guarantee
uniqueness across colli in the same batch.

**Compensation.** The Evri `compensationRequiredPounds` field has no direct
mapping in the gateway's `BookingRequest`. It defaults to £0 (Evri's default).
If compensation is needed, extend `ParcelDetailsModel` or use `Customs.CustomsValue`.

**No label at booking time.** Unlike some other carriers, the Evri Classic API
does not return label data in the booking response. Labels must be fetched
separately via `FetchLabel` using the barcode returned from booking.

**Hermes UK vs Hermes Germany.** Evri (this adapter) was formerly known as
Hermes UK. It is unrelated to Hermes Germany (HSI/HEX API), which has its own
separate adapter (`hermes.go`).
