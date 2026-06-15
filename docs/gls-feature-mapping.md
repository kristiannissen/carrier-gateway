# GLS — Feature Mapping

API: **GLS ShipIT API v1**
Auth: OAuth2 client credentials (clientID + clientSecret → Bearer token)
Coverage: Multi-country — DE, DK, SE, NL, BE, FR, ES, PT, IT, AT, IE, HR, SI, SK, CZ, HU and more via single credentials.
Implementation status: **Implemented**

---

## Summary

GLS covers booking, cancellation, tracking, and labels. The ShipIT API has an
`endofday` endpoint that must be called before the driver arrives — this is the
only carrier in the gateway where manifest close is a hard operational
requirement. `SporadicCollection` (sporadic pickup) exists in the API but is
not yet wired. Post-booking update is not yet implemented.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | ShipIT API v1 `POST /rs/shipments` |
| Cancel shipment | ✅ | `POST /rs/shipments/cancel/{trackID}` |
| Update shipment | ❌ | Not yet implemented |
| Idempotency key | ❌ | Client-side only |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Print label | ✅ | PDF and ZPL (200 dpi) |
| Reprint label | ✅ | `POST /rs/shipments/reprintparcel` |
| Return label | ✅ | `bookReturnShipment` — uses GLS Shop Returns API v3 |
| Return coverage | ✅ | Available in most GLS European markets |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ✅ | `POST /rs/tracking/parceldetails` |
| Event history | ✅ | TU history returned in tracking response |
| Estimated delivery | ✅ | Where returned by carrier |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup (sporadic) | ❌ | `POST /rs/sporadiccollection` exists in ShipIT API but not wired in adapter |
| Update pickup | ❌ | Needs investigation |
| Cancel pickup | ❌ | Needs investigation |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❌ | `POST /rs/shipments/endofday` exists in ShipIT API but not wired. **Operationally required for GLS** — must be called before the driver arrives. Acts as the collection order. Returns PDF. |
| Manifest document | ❌ | Returned by `endofday` but endpoint not yet wired |

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| SMS notification | ❌ | Not available in ShipIT API v1 schema — returns `ErrNotSupported` |
| Email notification | ❌ | Not available in ShipIT API v1 schema — returns `ErrNotSupported` |
| Flex delivery | ❌ | `DepositService` exists in API schema but not wired — returns `ErrNotSupported` |
| Signature required | ❌ | `DirectSignature` / `IdentService` exist in API schema but not wired — returns `ErrNotSupported` |
| Cash on delivery | ❌ | `CashService` exists in API schema but not wired |
| Hazmat | ✅ | `HazardousGoodsService` mapped |
| Insurance | ❌ | `AddOnLiabilityService` exists in API schema but not wired |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Customs / cross-border | ✅ | GLS customs-consignments-v3 API. CN22/CN23 form generation. |
| Service point delivery | ✅ | `ShopDeliveryService` — `receiver.servicePointId` → `parcelShopId` |
| Parcel shop search | ✅ | `/rs/parcelshop/distance`, `/rs/parcelshop/country/{countryCode}` in API |
| Multi-colli | ✅ | Multiple parcels per shipment |
| Business delivery | ✅ | `DeliveryType=business` |
| Multi-country | ✅ | Single credentials route across all GLS European markets |

---

## Endpoint mapping

| carrier-gateway | GLS ShipIT API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /rs/shipments` | ✅ |
| `DELETE /api/bookings/{id}` | `POST /rs/shipments/cancel/{id}` | ✅ |
| `PATCH /api/bookings/{id}` | — | ❌ not yet implemented |
| `GET /api/trackings/{id}` | `POST /rs/tracking/parceldetails` | ✅ |
| `GET /api/labels/{id}` | `POST /rs/shipments/reprintparcel` | ✅ |
| `POST /api/pickups` | `POST /rs/sporadiccollection` | ❌ not yet wired |
| `PUT /api/pickups/{id}` | ❓ needs investigation | ❌ |
| `DELETE /api/pickups/{id}` | ❓ needs investigation | ❌ |
| `POST /api/manifests` | `POST /rs/shipments/endofday` | ❌ not yet wired |

---

## Environment variables

| Variable | Description |
|---|---|
| `GLS_API_KEY` | GLS OAuth2 client ID |
| `GLS_CLIENT_SECRET` | GLS OAuth2 client secret |
| `GLS_CONTRACT_ID` | GLS shipper contact ID |

---

## Implementation notes

**Manifest is required.** Unlike other carriers where manifest is optional,
GLS requires `POST /rs/shipments/endofday` before the driver arrives. Skipping
this means the driver will not collect the parcels. Wiring `CloseManifest` for
GLS is a high-priority gap.

**Add-ons.** Most add-on service schemas exist in ShipIT API v1 (CashService,
DepositService, IdentService) but are not yet mapped in the adapter. They return
`ErrNotSupported` today; wiring them is a future task.

**OAuth token.** The adapter fetches and caches a Bearer token (30s expiry
buffer). Token refresh is transparent to callers.
