# FedEx — Feature Mapping

API: **FedEx Ship API v1 + Track API v1**
Base URL (prod): `https://apis.fedex.com`
Auth: OAuth2 client credentials (clientID + clientSecret → Bearer token)
Coverage: Worldwide.
Implementation status: **Not fully implemented yet** (Beta)

---

## Summary

FedEx covers booking, cancellation, and tracking. Labels are returned inline in
the booking response (PDF only); the standalone label reprint endpoint is not
yet wired (spec pending). Post-booking update is not supported. Pickup
scheduling is not in the FedEx API docs reviewed. Customs, add-ons, and service
point delivery are not yet wired.

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
| Book pickup | ❌ | Not in reviewed API docs. Booking uses `pickupType=USE_SCHEDULED_PICKUP` — assumes a standing collection agreement. |
| Update/Cancel pickup | ❌ | Not wired |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❌ | Not in FedEx API docs (`501`) |
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
| Service point delivery | ❌ | Not wired |
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
| `POST /api/pickups` | ❓ | ❌ not wired |
| `PUT /api/pickups/{id}` | ❓ | ❌ not wired |
| `DELETE /api/pickups/{id}` | ❓ | ❌ not wired |
| `POST /api/manifests` | — | ❌ → 501 |

---

## Implementation notes

**Beta status.** FedEx is marked Beta (`capabilities["fedex"].Beta = true`).
Booking and tracking are live against the FedEx API; the booking response
includes a `BetaWarning`.

**Label inline only.** Labels are returned as base64-encoded PDF inside the
`BookShipment` response (`ColliResponse.LabelURL` as a data URI). The `FetchLabel`
method is not implemented — callers must save the label from the booking response.
The FedEx label reprint API requires a separate spec review.

**Pickup type.** The adapter sets `pickupType=USE_SCHEDULED_PICKUP`, which
assumes the account has a standing daily collection agreement with FedEx. If
on-demand pickup is needed, `FedExPickup` API must be wired.

**Customs gap.** The FedEx Ship API supports a full customs declaration block
(commodity descriptions, HS codes, declared values). This is not yet mapped
in the adapter — international shipments requiring customs documentation must
use the FedEx portal until this is wired.
