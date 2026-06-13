# DHL eCommerce Europe — Feature Mapping

API: **DHL eConnect API (cPAN)**
Base URL (prod): `https://api.dhl.com` (eConnect endpoints)
Auth: OAuth2 client credentials (clientID + clientSecret → Bearer token). Optional DHL Unified Tracking API key for tracking.
Coverage: 28 European countries — B2C cross-border parcel product.
Implementation status: **Not fully implemented yet** (Beta)

---

## Summary

DHL eCommerce Europe covers the cross-border B2C parcel product (DHL Parcel
Connect and variants). Booking, tracking, labels, and returns are implemented.
Cancellation and post-booking update are not available via the eConnect API.
Pickup scheduling and manifest are unknown — no documentation confirmed.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | `POST /ccc/send-cpan` — label returned in response |
| Cancel shipment | ❌ | No cancellation endpoint in eConnect API |
| Update shipment | ❌ | No update endpoint in eConnect API |
| Idempotency key | ❌ | Client-side only |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Print label | ✅ | `GET /ccc/label-reprint` |
| Label formats | ✅ | PDF, PNG, ZPL. Sizes: 15×10cm, 21×10cm. Resolutions: 200 or 300 dpi. |
| Return label | ✅ | DHL Parcel Return Connect product (`DeliveryType=return`) |
| Labelless return | ✅ | QR code / GIF format — DHL Parcel Return Connect (`returnFunctionality=labelless`) |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ✅ | DHL Unified Tracking API — normalized status |
| Event history | ✅ | Scan events returned in `events[]` |
| Estimated delivery | ✅ | Where returned |
| Tracking API key | ⚠️ | Separate credential (`DHL_TRACKING_API_KEY`) from eConnect booking credentials |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ❓ | Not documented — pickup is handled by standing collection agreement |
| Update pickup | ❓ | Unknown |
| Cancel pickup | ❓ | Unknown |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❓ | Not documented — DHL eCommerce Europe does not have a confirmed manifest API |
| Manifest document | ❓ | Unknown |

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| SMS notification | ❓ | Not yet confirmed in adapter |
| Email notification | ❓ | Not yet confirmed in adapter |
| Flex delivery | ✅ | `DepositService` / `deliveryOption=deposit` |
| Signature required | ✅ | Mapped in adapter |
| Cash on delivery | ✅ | SEPA COD — requires `codAmount`, `codCurrency`, `codAccountNumber` (IBAN), `codBic` |
| Insurance | ✅ | Additional insurance via contract |
| Bulky | ⚠️ | Available in API schema but not wired in adapter |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Customs / cross-border | ✅ | Customs data block for non-EU and cross-border shipments |
| Service point delivery | ✅ | `deliveryType=parcelshop`, `parcelstation`, or `postOffice` — mapped from `receiver.servicePointId` |
| Multi-colli | ✅ | Multiple pieces per shipment |
| Customer barcode | ❌ | Available in API but not wired |

---

## Endpoint mapping

| carrier-gateway | DHL eConnect API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /ccc/send-cpan` | ✅ |
| `DELETE /api/bookings/{id}` | — | ❌ → 501 |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 |
| `GET /api/trackings/{id}` | DHL Unified Tracking API | ✅ |
| `GET /api/labels/{id}` | `GET /ccc/label-reprint` | ✅ |
| `POST /api/pickups` | ❓ | ❓ |
| `PUT /api/pickups/{id}` | ❓ | ❓ |
| `DELETE /api/pickups/{id}` | ❓ | ❓ |
| `POST /api/manifests` | ❓ | ❓ |

---

## Implementation notes

**Two separate credentials.** The eConnect booking API uses OAuth2 (clientID +
clientSecret). The DHL Unified Tracking API uses a separate API key
(`DHL_TRACKING_API_KEY`). Both are required for full functionality.

**SEPA COD.** DHL eCommerce Europe COD uses SEPA bank transfer — not cash on
delivery to a courier. Requires a valid IBAN and BIC. Only available on the
`DHL Parcel Connect` (ParcelEurope.parcelconnect) product.

**Labelless returns.** The QR code / GIF return flow is DHL Parcel Return
Connect only. The customer uses the code to drop off at a DHL service point
without printing a label. Return instructions (multi-language) must be
provided separately — request from DHL.

**Coverage.** 28 European countries. Not a domestic carrier — intended for
cross-border B2C flows originating from a European hub (typically DE or NL).
