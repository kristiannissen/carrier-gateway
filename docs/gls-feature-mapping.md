# GLS — Feature Mapping

API: **GLS ShipIT API v1**
Auth: OAuth2 client credentials (clientID + clientSecret → Bearer token)
Coverage: Multi-country — DE, DK, SE, NL, BE, FR, ES, PT, IT, AT, IE, HR, SI, SK, CZ, HU and more via single credentials.
Implementation status: **Production** — UpdateShipment is a confirmed carrier
limitation (no update/modify/amend endpoint found anywhere in
`APIdocs/GLS_Shipping_API_v0.8.pdf`), so all primary methods are complete.
Pickup scheduling (`sporadiccollection`) and manifest close (`endofday`) are
now wired via `BookPickup` and `CloseManifest`. `UpdatePickup`, `CancelPickup`,
and `GetPickupAvailability` return `ErrNotSupported` — confirmed carrier
limitations, since `gls_shipit-farm.yaml` defines no update/cancel/availability
operation for a sporadic collection. No genuine implementation gaps remain.

---

## Summary

GLS covers booking, cancellation, tracking, labels, pickup scheduling, and
manifest close. The ShipIT API has an `endofday` endpoint that must be called
before the driver arrives — this is the only carrier in the gateway where
manifest close is a hard operational requirement, and it is now wired.
`SporadicCollection` (ad-hoc pickup) is also wired via `BookPickup`. Post-booking
update, pickup update/cancel, and pickup availability are confirmed unsupported
by the API itself — no such operations exist in the ShipIT API spec.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | ShipIT API v1 `POST /rs/shipments` |
| Cancel shipment | ✅ | `POST /rs/shipments/cancel/{trackID}` |
| Update shipment | ❌ | Confirmed carrier limitation — no update/modify/amend endpoint in `GLS_Shipping_API_v0.8.pdf` |
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
| Book pickup (sporadic) | ✅ | `POST /rs/sporadiccollection` — `PreferredPickUpDate` built from `Pickup.Date` + `Pickup.ReadyTime` (defaults to 09:00) |
| Update pickup | ❌ | Confirmed carrier limitation — no update operation for a sporadic collection in `gls_shipit-farm.yaml` |
| Cancel pickup | ❌ | Confirmed carrier limitation — no cancel operation for a sporadic collection in `gls_shipit-farm.yaml` |
| Pickup availability | ❌ | Confirmed carrier limitation — no availability endpoint exists; callers proceed directly to `BookPickup` |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ✅ | `POST /rs/shipments/endofday?date=...`. **Operationally required for GLS** — must be called before the driver arrives. Acts as the collection order. Account-wide by date; `req.TrackingNumbers` is not sent since the API has no filtered variant. |
| Manifest document | ❌ | `endofday` returns the day's `Shipments` array (used for `ParcelsConfirmed`); no PDF/manifest document field in the response schema |

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
| `POST /api/pickups` | `POST /rs/sporadiccollection` | ✅ |
| `PUT /api/pickups/{id}` | — | ❌ confirmed carrier limitation |
| `DELETE /api/pickups/{id}` | — | ❌ confirmed carrier limitation |
| `POST /api/manifests` | `POST /rs/shipments/endofday` | ✅ |

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
this means the driver will not collect the parcels. `CloseManifest` is now wired.

**Add-ons.** Most add-on service schemas exist in ShipIT API v1 (CashService,
DepositService, IdentService) but are not yet mapped in the adapter. They return
`ErrNotSupported` today; wiring them is a future task.

**OAuth token.** The adapter fetches and caches a Bearer token (30s expiry
buffer). Token refresh is transparent to callers.
