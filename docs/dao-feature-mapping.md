# DAO — Feature Mapping

API: **DAO PHP API (proprietary)**
Base URL: `https://api.dao.as`
Auth: customerID + API key
Coverage: Denmark only — consumer parcel network with strong home delivery and shop coverage.
Implementation status: **Not fully implemented yet** (Beta)

---

## Summary

DAO is a Denmark-only carrier with a proprietary PHP-based API. The core
booking and cancellation flow is live. Uniquely, DAO supports post-booking
updates to weight and service point ID alongside the standard contact fields.
No pickup scheduling or manifest endpoints exist in the DAO API. COD, flex, and
signature add-ons are not available.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | Home delivery (`DAODirekte/leveringsordre.php`), shop delivery (`DAOPakkeshop/leveringsordre.php`), return (`DAOPakkeshop/returordre.php`) |
| Cancel shipment | ✅ | `AnnullerePakke.php` |
| Update shipment | ✅ | Phone + email (`OpdaterKontaktOplysning.php`), weight (`OpdaterVaegt.php`), service point (`OpdaterShopid.php`) |
| Idempotency key | ❌ | Client-side only |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Print label | ✅ | PDF via `HentLabel.php` |
| Return label | ✅ | `DeliveryType=return` — `withlabel` (customer prints) or `labelless` (QR code) |
| Label format | ✅ | PDF only |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ✅ | `TrackNTrace_v2.php` — normalized status |
| Event history | ✅ | Scan events returned in `events[]` |
| Estimated delivery | ✅ | Where returned by carrier |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ❌ | No pickup scheduling endpoint in DAO API |
| Update pickup | ❌ | Not in DAO API |
| Cancel pickup | ❌ | Not in DAO API |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❌ | Not in DAO API (`501`) |
| Manifest document | ❌ | Not available via API |

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| SMS notification | ⚠️ | Accepted but triggers `AddOnWarning` — DAO contact update API does not support notifications via standard channel. Contact update is sent post-booking. |
| Email notification | ⚠️ | Same as SMS — contact update sent, but notification triggering is not guaranteed. |
| Flex delivery | ❌ | Not supported by DAO |
| Signature required | ❌ | Not supported by DAO |
| Cash on delivery | ❌ | Not supported by DAO |
| Insurance | ❌ | Not supported by DAO |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Customs / cross-border | ❌ | Denmark-only carrier — no customs needed |
| Service point delivery | ✅ | `receiver.servicePointId` → `shopid` parameter. Post-booking service point change also supported via `OpdaterShopid.php`. |
| Locker delivery | ✅ | `receiver.servicePointId` → `lockerId` (DAO locker network) |
| Multi-colli | ❌ | DAO API takes a single set of dimensions per request — only `Colli[0]` is sent |
| Business delivery | ✅ | `DeliveryType=business` |
| Weight update | ✅ | `OpdaterVaegt.php` — converts kg to grams internally. Must be before first terminal scan. |

---

## Endpoint mapping

| carrier-gateway | DAO API | Status |
|---|---|---|
| `POST /api/bookings` (home) | `DAODirekte/leveringsordre.php` | ✅ |
| `POST /api/bookings` (shop) | `DAOPakkeshop/leveringsordre.php` | ✅ |
| `POST /api/bookings` (return) | `DAOPakkeshop/returordre.php` | ✅ |
| `DELETE /api/bookings/{id}` | `AnnullerePakke.php` | ✅ |
| `PATCH /api/bookings/{id}` | `OpdaterKontaktOplysning.php` / `OpdaterVaegt.php` / `OpdaterShopid.php` | ✅ |
| `GET /api/trackings/{id}` | `TrackNTrace_v2.php` | ✅ |
| `GET /api/labels/{id}` | `HentLabel.php` | ✅ |
| `POST /api/pickups` | — | ❌ → 501 |
| `PUT /api/pickups/{id}` | — | ❌ → 501 |
| `DELETE /api/pickups/{id}` | — | ❌ → 501 |
| `POST /api/manifests` | — | ❌ → 501 |

---

## Implementation notes

**Beta status.** DAO is marked Beta in the gateway (`capabilities["dao"].Beta = true`).
The integration is functional but not fully validated in production.

**Weight update.** DAO stores weight in grams internally. The adapter converts
the `float64` kg value from `UpdateRequest.Weight` before calling
`OpdaterVaegt.php`. Weight updates must be applied before the first terminal
scan.

**Test mode.** Set `DAO_TEST_MODE=true` to add `test=1` on requests that support
it. Per the DAO API spec, `test=1` is only documented on booking endpoints
(`leveringsordre.php`, `returordre.php`) and `OpdaterVaegt.php`. It is
intentionally not sent on cancel, tracking, label, contact-update, or
shop-update calls, which do not list it in their parameter tables.

**Notification add-ons.** SMS and email are passed through as contact updates
rather than true notification triggers. The booking response includes an
`AddOnWarning` to flag this. Callers should not rely on DAO to send the
SMS/email at a specific shipment event.
