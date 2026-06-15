# PostNord — Feature Mapping

API: **PostNord Customer API**
Base URL (prod): `https://api2.postnord.com`
Auth: API key (query parameter `?apikey=`) + customer number + application ID
Coverage: Denmark, Sweden, Norway, Finland — single API key across all four markets.
Implementation status: **Implemented**

---

## Summary

PostNord is the most fully integrated carrier in the gateway. All five core
`CarrierAdapter` methods are live. It is the only carrier with native
idempotency key support. Pickup scheduling is supported for domestic DK/SE/FI
shipments. Manifest is not available via API.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | v3 EDI API, multi-colli, full address block |
| Cancel shipment | ✅ | v3 EDI cancel instruction |
| Update shipment | ✅ (partial) | Phone and email only. Weight and service point change are not supported post-booking. |
| Idempotency key | ✅ | Native — passed to the v3 EDI API directly |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Print label | ✅ | PDF format via v3 label endpoint |
| Return label | ✅ | `DeliveryType=return` triggers return booking |
| Label format | ✅ | PDF |
| ZPL | ⚠️ | Code path exists (`/rest/shipment/v3/edi/labels/zpl`) but not validated against live PostNord API |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ✅ | v5 trackandtrace — normalized status |
| Event history | ✅ | Scan events returned in `events[]` |
| Estimated delivery | ✅ | `estimatedDelivery` populated where carrier returns it |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ✅ | `/v3/pickups/ids` — accepts item IDs from booking response and a time window |
| Update pickup | ❌ | Not in PostNord API (`501`) |
| Cancel pickup | ❌ | Not in PostNord API (`501`) |
| Geographic scope | ⚠️ | Domestic DK, SE, FI only. Cross-border PostNord shipments return `not_supported`. |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❌ | Not in PostNord API — handled by EDI scan at collection time (`501`) |
| Manifest document | ❌ | Not available via API |

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| SMS notification | ✅ | Mapped to PostNord `additionalServiceCode`. Requires `receiver.phone`. |
| Email notification | ✅ | Mapped to PostNord `additionalServiceCode`. Requires `receiver.email`. |
| Flex delivery | ✅ | Mapped to `additionalServiceCode` with instructions |
| Signature required | ✅ | `additionalServiceCode "A2"` |
| Insurance | ✅ | `additionalServiceCode "A8"`. Requires `insuranceValue > 0`. |
| Cash on delivery | ❌ | Not supported by PostNord API |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Customs / cross-border | ✅ | v3 EDI customs block — HS code, EORI, VAT, Incoterms, items |
| Service point delivery | ✅ | `receiver.servicePointId` → `servicePointId` in EDI |
| Multi-colli | ✅ | Multiple colli per booking, each gets its own tracking number |
| Business delivery | ✅ | `DeliveryType=business` |

---

## Endpoint mapping

| carrier-gateway | PostNord API | Status |
|---|---|---|
| `POST /api/bookings` | v3 EDI create | ✅ |
| `DELETE /api/bookings/{id}` | v3 EDI cancel | ✅ |
| `PATCH /api/bookings/{id}` | v3 EDI update | ✅ (phone, email only) |
| `GET /api/trackings/{id}` | v5 trackandtrace | ✅ |
| `GET /api/labels/{id}` | v3 label | ✅ |
| `POST /api/pickups` | `/v3/pickups/ids` | ✅ (domestic DK/SE/FI) |
| `PUT /api/pickups/{id}` | — | ❌ → 501 |
| `DELETE /api/pickups/{id}` | — | ❌ → 501 |
| `POST /api/manifests` | — | ❌ → 501 |

---

## Implementation notes

## Environment variables

| Variable | Description |
|---|---|
| `POSTNORD_API_KEY` | PostNord API key |
| `POSTNORD_CUSTOMER_NUMBER` | Account number (partyId) |
| `POSTNORD_APPLICATION_ID` | Application ID (optional) |

---

## Implementation notes

**Item IDs for pickup.** PostNord `/v3/pickups/ids` requires the carrier item
IDs returned in the booking response, not human-readable tracking numbers. The
adapter must store or receive these IDs via the `trackingNumbers` field on
`PickupRequest`.

**Pickup cutoff.** `/v4/sac/pickup/stopdate` can validate the pickup date
against PostNord's cutoff window before submitting — not currently wired.

**Multi-market.** The same API key works for DK, SE, NO, FI. Routing is
determined by the sender/receiver country in the booking payload. No per-country
credential switching is required.
