# PostNord — Feature Mapping

API: **PostNord Customer API**
Base URL (prod): `https://api2.postnord.com`
Auth: API key (query parameter `?apikey=`) + customer number + application ID
Coverage: Denmark, Sweden, Norway, Finland — single API key across all four markets.
Implementation status: **Partial** — all five core `CarrierAdapter` methods
are live, so every primary method is complete. BookPickup is a genuine
secondary implementation gap: PostNord's `/v3/pickups/ids` endpoint exists
(see `APIdocs/postnord_pickup_swagger.json`) but `internal/adapter/postnord.go`
has no pickup method at all — a previous version of this doc wrongly claimed
it was implemented. Manifest is a confirmed carrier limitation (handled by EDI
scan at collection, not exposed via API).

---

## Summary

PostNord is the most fully integrated carrier in the gateway. All five core
`CarrierAdapter` methods are live. It is the only carrier with native
idempotency key support. Pickup scheduling is not yet wired despite the
`/v3/pickups/ids` endpoint existing for domestic DK/SE/FI shipments. Manifest
is not available via API.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | v3 EDI API, multi-colli, full address block. Returns `CarrierMessageID` for later reuse on update. |
| Cancel shipment | ✅ | v3 EDI cancel instruction (`messageFunction: "Cancellation"`, `updateIndicator: "Delete"`). See "Unconfirmed source" note below — a single low-confidence source disputes this, but it hasn't been acted on. |
| Update shipment | ✅ (partial) | Phone and email only. Weight and service point change are not supported post-booking. Per `APIdocs/postnord_update_cancel.rtf`, update is documented as SE-only for *all* fields, not just address changes, and the update instruction should reuse the exact `messageId` from the original booking — pass `BookingResponse.CarrierMessageID` back as `UpdateRequest.CarrierMessageID`. |
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
| Book pickup | ❌ | `/v3/pickups/ids` exists in the PostNord API but is not wired in the adapter — genuine gap, not a limitation. A previous version of this doc claimed this was implemented. |
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
| `POST /api/pickups` | `/v3/pickups/ids` (exists, unwired) | ❌ → 501 |
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

**Pickup not wired.** `internal/adapter/postnord.go` does not implement
`BookPickup` (or any `ManifestAdapter` method) despite `/v3/pickups/ids`
existing in the PostNord API and requiring only the carrier item IDs returned
in the booking response, not human-readable tracking numbers. This is a
genuine secondary implementation gap, not a limitation — wiring it would
require adding `ManifestAdapter` support to the adapter.

**Pickup cutoff.** `/v4/sac/pickup/stopdate` can validate the pickup date
against PostNord's cutoff window before submitting — not currently wired.

**Multi-market.** The same API key works for DK, SE, NO, FI. Routing is
determined by the sender/receiver country in the booking payload. No per-country
credential switching is required.

**Update messageId reuse (`APIdocs/postnord_update_cancel.rtf`).** PostNord's
documentation states that update instructions must reuse the exact `messageId`
from the original booking request — not a freshly generated one. Since this
gateway is stateless, `BookShipment` returns the messageId it used as
`BookingResponse.CarrierMessageID`; callers who need to update a PostNord
shipment later should store this value and pass it back as
`UpdateRequest.CarrierMessageID`. If omitted, `UpdateShipment` still generates
a new messageId on a best-effort basis (previous behavior), which PostNord's
API may reject for an existing shipment per this source.

**Update is SE-only for all fields, not just address.** The same source states
the update capability as a whole — not only address changes — is currently
only supported for Sweden. The adapter cannot validate this proactively (no
country is derivable from a bare tracking number in a stateless call), so it
still relies on the carrier rejecting DK/NO/FI update attempts at the API
level, same as before — this is a documentation correction, not a behavior
change.

**Cancel endpoint — unconfirmed discrepancy, not acted on.** The same RTF
claims PostNord's Booking API has no dedicated cancel/void endpoint at all,
recommending the Delivery Order Modification Service (DOMS) or manual
support instead. This directly contradicts the adapter's existing, working
`CancelShipment` (an EDI instruction with `messageFunction: "Cancellation"`,
`updateIndicator: "Delete"`). The source is a single AI-research summary with
no schema for the EDI Instruction format to cross-check either claim against,
and gives no endpoint or schema for DOMS to implement even if it's right.
Not acted on — flagged in code and here for awareness only.
