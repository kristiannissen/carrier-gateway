# FedEx — Feature Mapping

API: **FedEx Ship API v1 + Track API v1 + Pickup API v1 + Location Search API v1**
Base URL (prod): `https://apis.fedex.com`
Auth: OAuth2 client credentials (clientID + clientSecret → Bearer token)
Coverage: Worldwide.
Implementation status: **Not fully implemented yet** (Beta)

---

## Summary

FedEx covers booking, cancellation, tracking, and pickup scheduling. Labels are
returned inline in the booking response (PDF only); the standalone label reprint
endpoint is not yet wired (spec pending). Post-booking update is not supported.
Pickup scheduling covers book, cancel, and availability check via the FedEx
Pickup API v1; update is not supported (cancel-and-rebook). Service point
delivery (Hold at Location) is wired — set `receiver.servicePointId` to the
FedEx `locationId` code (e.g. "YBZA") obtained from the Location Search API.
Customs and add-ons are not yet wired.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | `POST /ship/v1/shipments` — PDF label returned inline per package |
| Cancel shipment | ✅ | `PUT /ship/v1/shipments/cancel` |
| Update shipment | ❌ | Not supported by FedEx Ship API |
| Idempotency key | ❌ | Client-side only |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Label inline at booking | ✅ | PDF only — `EncodedLabel` in booking response per package |
| Label reprint (FetchLabel) | ❌ | `FetchLabel` returns `ErrNotSupported` — label reprint endpoint spec not yet available |
| Label format | ⚠️ | PDF only via booking. ZPL/PNG not wired. |
| Return label | ❌ | Not yet implemented |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ✅ | `POST /track/v1/trackingnumbers` — normalized status |
| Event history | ✅ | `scanEvents[]` mapped to `events[]` |
| Estimated delivery | ✅ | `dateAndTimes[type=ESTIMATED_DELIVERY]` |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ✅ | `POST /pickup/v1/pickups` — FDXE (Express) carrier code. Returns opaque token encoding code + date + location. |
| Check availability | ✅ | `POST /pickup/v1/pickups/availabilities` — returns available `PickupSlot` windows |
| Update pickup | ❌ | Not supported by FedEx Pickup API — cancel and rebook |
| Cancel pickup | ✅ | `PUT /pickup/v1/pickups/cancel` — requires the token from BookPickup |

**Confirmation token.** `BookPickup` returns a pipe-delimited opaque token
`{confirmationCode}|{YYYY-MM-DD}|{expressLocation}` rather than the raw FedEx
confirmation code. This is required because the cancel endpoint needs the
scheduled date and Express facility location alongside the code. Pass the token
unchanged to `CancelPickup`; do not attempt to parse it.

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❌ | Not supported — standard FedEx pickup accounts have no end-of-day manifest close (`501`) |
| Manifest document | ❌ | Not available |

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| SMS notification | ❌ | Not wired |
| Email notification | ❌ | Not wired |
| Flex delivery | ❌ | Not wired |
| Signature required | ❌ | Not wired (FedEx supports DIRECT, INDIRECT, ADULT signature options) |
| Cash on delivery | ❌ | Not wired |
| Insurance (declared value) | ❌ | Not wired |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Customs / cross-border | ❌ | Not yet wired — FedEx Ship API supports full customs declaration but adapter does not map it |
| Service point delivery (HAL) | ✅ | `receiver.servicePointId` → `HOLD_AT_LOCATION` + `holdAtLocationDetail.locationId`. Use Location Search API to look up `locationId`. |
| Multi-colli | ✅ | Multiple `RequestedPackageLineItems` per shipment |
| Service type auto-selection | ✅ | `fedexServiceType()` selects domestic vs. international service based on sender/receiver country |

---

## Endpoint mapping

| carrier-gateway | FedEx API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /ship/v1/shipments` | ✅ |
| `DELETE /api/bookings/{id}` | `PUT /ship/v1/shipments/cancel` | ✅ |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 |
| `GET /api/trackings/{id}` | `POST /track/v1/trackingnumbers` | ✅ |
| `GET /api/labels/{id}` | — | ❌ → 501 (pending spec) |
| `GET /api/pickups/availability` | `POST /pickup/v1/pickups/availabilities` | ✅ |
| `POST /api/pickups` | `POST /pickup/v1/pickups` | ✅ |
| `PUT /api/pickups/{id}` | — | ❌ → 501 (cancel-and-rebook) |
| `DELETE /api/pickups/{id}` | `PUT /pickup/v1/pickups/cancel` | ✅ |
| `POST /api/manifests` | — | ❌ → 501 |
| Service point lookup (caller-side) | `POST /location/v1/locations` | ℹ️ Not a gateway endpoint — callers call FedEx directly to resolve a `locationId` |

---

## Implementation notes

**Beta status.** FedEx is marked Beta (`capabilities["fedex"].Beta = true`).
Booking and tracking are live against the FedEx API; the booking response
includes a `BetaWarning`.

**Label inline only.** Labels are returned as base64-encoded PDF inside the
`BookShipment` response (`ColliResponse.LabelURL` as a data URI). The `FetchLabel`
method is not implemented — callers must save the label from the booking response.
The FedEx label reprint API requires a separate spec review.

**Pickup type.** The adapter sets `pickupType=USE_SCHEDULED_PICKUP` on shipment
bookings, which is compatible with accounts that have a standing collection
agreement. On-demand pickup scheduling is now available via `POST /api/pickups`.

**Pickup token.** The confirmation number returned by `BookPickup` is an opaque
pipe-delimited token (`{code}|{date}|{location}`) rather than the raw FedEx
confirmation code. The cancel endpoint requires all three values, and they
cannot be recovered from the code alone, so encoding them into the token avoids
external state. The `location` segment is empty for Ground (FDXG) pickups and
populated for Express (FDXE) pickups.

**Hold at Location (service point delivery).** Set `receiver.servicePointId`
to the FedEx `locationId` code (4–5 alphanumeric characters, e.g. "YBZA").
The adapter injects `HOLD_AT_LOCATION` into `specialServiceTypes` and populates
`holdAtLocationDetail.locationId` automatically. To look up valid location IDs
near a delivery address, call `POST /location/v1/locations` on the FedEx
Location Search API and filter by `transferOfPossessionType=HOLD_AT_LOCATION`.

**Customs gap.** The FedEx Ship API supports a full customs declaration block
(commodity descriptions, HS codes, declared values). This is not yet mapped
in the adapter — international shipments requiring customs documentation must
use the FedEx portal until this is wired.
