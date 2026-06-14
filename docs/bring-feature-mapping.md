# Bring — Feature Mapping

API: **Mybring Booking API + Pickup API**
Base URL: `https://api.bring.com`
Auth: Mybring login ID + API key (HTTP Basic)
Coverage: Norway (primary), Sweden, Denmark, Finland.
Implementation status: **Implemented**

---

## Summary

Bring covers the core booking loop, pickup scheduling, and a solid tracking
event stream. Post-booking update is not supported. Pickup update and cancel
are not supported (cancel and rebook). Manifest is not available via API.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | Mybring Booking API, multi-colli |
| Cancel shipment | ✅ | `DELETE /booking/api/shipment/{consignmentNumber}` |
| Update shipment | ❌ | Not supported by Bring API (`501`) |
| Idempotency key | ❌ | Client-side only — no native deduplication in Bring API |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Print label | ✅ | PDF |
| Label format | ✅ | PDF only. ZPL is not supported — `FetchLabel` returns `501` for non-PDF formats. |
| Return label | ✅ | `DeliveryType=return` — standard and labelless (QR code) |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ✅ | Normalized status |
| Event history | ✅ | Scan events returned in `events[]` |
| Estimated delivery | ✅ | Where returned by carrier |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ✅ | Bring Pickup API — date, time window, location |
| Update pickup | ❌ | Not supported (`501`) — cancel and rebook |
| Cancel pickup | ❌ | Not supported (`501`) |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❌ | Not in Bring API (`501`) |
| Manifest document | ❌ | Not available via API |

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| SMS notification | ✅ | Bring notification service — requires `receiver.phone` |
| Email notification | ✅ | Bring notification service — requires `receiver.email` |
| Flex delivery | ✅ | Mapped to Bring flex delivery VAS |
| Signature required | ✅ | Bring VAS 1131 (direct signature) |
| Cash on delivery | ✅ | Bring VAS 1000. Requires `codAmount`, `codCurrency`, `codAccountNumber`. |
| Insurance | ❌ | Not implemented |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Customs / cross-border | ✅ | `NatureOfCargo`, Incoterms, HS codes, EORI, VAT. Required for NO shipments from EU. |
| Service point delivery | ✅ | `receiver.servicePointId` → `pickupPointId` |
| Multi-colli | ✅ | Multiple packages per consignment |
| Business delivery | ✅ | Service code selection |

---

## Endpoint mapping

| carrier-gateway | Bring API | Status |
|---|---|---|
| `POST /api/bookings` | POST `/booking/api/shipment` | ✅ |
| `DELETE /api/bookings/{id}` | DELETE `/booking/api/shipment/{id}` | ✅ |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 |
| `GET /api/trackings/{id}` | Mybring tracking | ✅ |
| `GET /api/labels/{id}` | Mybring label | ✅ |
| `POST /api/pickups` | Bring Pickup API | ✅ |
| `PUT /api/pickups/{id}` | — | ❌ → 501 |
| `DELETE /api/pickups/{id}` | — | ❌ → 501 |
| `POST /api/manifests` | — | ❌ → 501 |

---

## Implementation notes

**Customs for Norway.** Shipments from EU into Norway require customs data.
The adapter maps `Customs.NatureOfCargo` (defaults to `SALE_OF_GOODS` for
B2B/B2C when empty), Incoterms, HS codes, and EORI/VAT numbers.

**COD.** Only supported on Bring (not other carriers in the gateway as of
writing). Requires a Norwegian bank account number (`codAccountNumber`).

**QR / labelless returns.** The booking response includes a QR code URL when
`generateQrCodes=true` is set. Pickup-point staff scan the code to print the
label. Used when the end customer drops off without a pre-printed label.
