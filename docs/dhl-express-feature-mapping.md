# DHL Express — Feature Mapping

API: **MyDHL API v3.3.0**
Base URL (prod): `https://express.api.dhl.com/mydhlapi`
Auth: HTTP Basic (username + password)
Coverage: Worldwide — express international and domestic.
Implementation status: **Not fully implemented yet** (Beta)

---

## Summary

DHL Express covers booking, tracking, label fetch, and return labels. Pickup is
implicit in the booking call (a `dispatchConfirmationNumber` is returned) and
can also be updated and cancelled as a standalone operation. AWB cancellation is
not available via the MyDHL API — the shipment cannot be voided after booking.
Manifest retrieval is available post-collection via the image endpoint.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | `POST /shipments` — label returned inline in booking response |
| Cancel shipment | ❌ | No void/cancel AWB endpoint in MyDHL API. Shipment cannot be voided after booking. |
| Update shipment | ❌ | Not supported by MyDHL API |
| Idempotency key | ❌ | Client-side only |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Print label | ✅ | `GET /shipments/{id}/get-image` |
| Label formats | ✅ | PDF, PNG, ZPL |
| Return label | ✅ | `DeliveryType=return` — uses `returnProductCode` (configurable via `DHL_EXPRESS_RETURN_PRODUCT_CODE`). Defaults to product code `P` (EXPRESS WORLDWIDE). |
| Manifest document | ✅ | `GET /shipments/{id}/get-image?typeCode=MANIFEST` — available post-collection |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ✅ | `GET /shipments/{id}/tracking` — normalized status |
| Event history | ✅ | Scan events returned in `events[]` |
| Estimated delivery | ✅ | Where returned by carrier |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Implicit pickup at booking | ✅ | `pickup.isRequested=true` in booking payload. Returns `dispatchConfirmationNumber`. |
| Book standalone pickup | ❌ | Not yet wired as `POST /api/pickups` |
| Update pickup | ✅ | `PATCH /pickups` — accepts `dispatchConfirmationNumber`, new date/time window |
| Cancel pickup | ✅ | `DELETE /pickups/{dispatchConfirmationNumber}` |

**Note:** Pickup update and cancel are wired via the `ManifestAdapter` interface
but standalone pickup booking (`POST /api/pickups`) is not yet wired — pickup
is currently only triggered via the booking call.

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❌ | DHL Express does not use a pre-collection manifest close. The manifest document is retrieved post-collection. |
| Manifest document | ✅ | Available post-collection via `GET /shipments/{id}/get-image?typeCode=MANIFEST` |

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| SMS notification | ⚠️ | Accepted but triggers `AddOnWarning` — not supported by MyDHL API shipment endpoint |
| Email notification | ✅ | Mapped to `shipmentNotification` in payload. Requires `receiver.email`. |
| Flex delivery / on-demand | ✅ | `onDemandDelivery.servicePointId` for service point redirection |
| Signature required | ⚠️ | Accepted but triggers `AddOnWarning` — not wired |
| Cash on delivery | ⚠️ | Accepted but triggers `AddOnWarning` — not supported by booking endpoint |
| Insurance | ✅ | Mapped to `valueAddedServices` insurance block |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Customs / cross-border | ✅ | Full customs declaration — `Customs` block with Incoterms, HS codes, EORI, IOSS, invoice number/date, line items. IOSS maps to `SDT` registration number on importer. |
| Service point delivery | ✅ | `receiver.servicePointId` → `onDemandDelivery.servicePointId` (6-char DHL code) |
| Multi-colli | ✅ | Multiple packages per shipment |
| Business delivery | ✅ | Product code selection |
| Domestic + international | ✅ | Product code `P` (EXPRESS WORLDWIDE) is the default; overridable |

---

## Endpoint mapping

| carrier-gateway | DHL Express API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /shipments` | ✅ |
| `DELETE /api/bookings/{id}` | — | ❌ Not available → 501 |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 |
| `GET /api/trackings/{id}` | `GET /shipments/{id}/tracking` | ✅ |
| `GET /api/labels/{id}` | `GET /shipments/{id}/get-image` | ✅ |
| `POST /api/pickups` | Implicit via booking | ⚠️ Standalone not wired |
| `PUT /api/pickups/{id}` | `PATCH /pickups` | ✅ |
| `DELETE /api/pickups/{id}` | `DELETE /pickups/{id}` | ✅ |
| `POST /api/manifests` | `GET /shipments/{id}/get-image?typeCode=MANIFEST` | ✅ (post-collection only) |

---

## Environment variables

| Variable | Description |
|---|---|
| `DHL_EXPRESS_USERNAME` | MyDHL API username |
| `DHL_EXPRESS_PASSWORD` | MyDHL API password |
| `DHL_EXPRESS_ACCOUNT_NUMBER` | DHL Express account number |
| `DHL_EXPRESS_PRODUCT_CODE` | Product code for outbound shipments (e.g. `P`) |
| `DHL_EXPRESS_RETURN_PRODUCT_CODE` | Product code for return shipments |

---

## Implementation notes

**No AWB cancellation.** DHL Express does not expose a cancel/void shipment
endpoint. Once a shipment is booked, it cannot be cancelled via API. `CancelShipment`
returns `ErrNotSupported`. The pickup can still be cancelled independently via
`DELETE /pickups/{dispatchConfirmationNumber}`.

**Pickup confirmation number.** The `dispatchConfirmationNumber` from
`BookingResponse` is required to update or cancel the pickup. It is separate
from the AWB tracking number. Callers must store it at booking time.

**Product code.** Defaults to `P` (EXPRESS WORLDWIDE). Override via the
`DHL_EXPRESS_PRODUCT_CODE` environment variable. Return shipments use
`DHL_EXPRESS_RETURN_PRODUCT_CODE`.
