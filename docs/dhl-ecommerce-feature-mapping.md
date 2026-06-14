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
| Label formats | ✅ | PDF only. PNG and ZPL are available in the DHL eConnect API schema but not wired — `FetchLabel` returns `501` for non-PDF formats. |
| Return label | ✅ | DHL Parcel Return Connect product (`DeliveryType=return`) |
| Labelless return | ✅ | QR code / GIF format — DHL Parcel Return Connect (`returnFunctionality=labelless`) |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ✅ | DHL Unified Tracking API — normalized status |
| Event history | ✅ | Scan events returned in `events[]` |
| Event detail text | ✅ | `statusDetailed` used when present; falls back to `status` short title |
| Estimated delivery | ✅ | `estimatedTimeOfDelivery` where returned; falls back to `estimatedDeliveryTimeFrame.estimatedFrom` |
| Tracking API key | ⚠️ | Separate credential (`DHL_TRACKING_API_KEY`) from eConnect booking credentials |
| Response schema | ⚠️ | No machine-readable spec available — field mapping is inferred from the DHL developer portal user guide, not a validated OpenAPI schema. The `DHL_Shipment Tracking_2_0.yaml` in APIdocs is a stub (references a WADL, contains no schemas). |
| Status code normalization | ⚠️ | `normalizeStatus("dhl", statusCode)` maps carrier codes to gateway statuses, but the full set of DHL Unified Tracking status codes is not documented in available specs — mapping may be incomplete |
| Proof of Delivery (POD) | ❌ | Not implemented. Available for DHL Express and DHL Freight after postal-code validation; no spec for response schema in available docs |
| Piece-level tracking | ❌ | Not implemented. Mentioned as available in the Unified Tracking API user guide but no response schema documented |
| Rate limiting | ⚠️ | Default quota is 250 calls/day, 1 per 5 seconds — insufficient for production. No throttle/retry logic in adapter. Request upgrade via DHL developer portal |
| `service` parameter | ⚠️ | Hardcoded to `ecommerce-europe`. For DHL Parcel DE domestic tracking use `parcel-de`; for DHL Express use `express`. Value not validated by available docs |

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
| Flex delivery | ⚠️ | Accepted but silently skipped — not mapped to a wire-format field |
| Signature required | ⚠️ | Accepted but silently skipped — not mapped to a wire-format field |
| Cash on delivery | ✅ | SEPA COD — requires `codAmount`, `codCurrency`, `codAccountNumber` (IBAN), `codBic` |
| Insurance | ✅ | Additional insurance via contract |
| Bulky | ⚠️ | Available in API schema but not wired in adapter |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Customs / cross-border | ✅ | Customs data block for non-EU and cross-border shipments |
| Service point delivery | ✅ | `deliveryType=parcelshop`, `parcelstation`, or `postOffice` — mapped from `receiver.servicePointId` |
| Multi-colli | ❌ | DHL eConnect is single-parcel per cPAN — only `Colli[0]` is sent |
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

**Tracking spec gap.** No machine-readable OpenAPI spec for the DHL Unified
Tracking API is available in the APIdocs folder — the `DHL_Shipment Tracking_2_0.yaml`
file is an empty stub referencing a WADL. The response schema (field names,
status code values, nesting) is inferred from the developer portal user guide
(`dhl_tracking.md`) and live response observation. Before adding POD, piece-level
tracking, or extended status normalization, obtain the actual OpenAPI spec from
`developer.dhl.com` and add it to APIdocs.

**Tracking rate limits.** The initial quota on a new DHL API key is 250 calls/day
with a 1 call/5 second burst limit. This is only suitable for development. Request
a production upgrade via the DHL developer portal before going live. The adapter
has no retry or throttle logic — a 429 response will surface as an error.

**Parcel DE vs. eCommerce Europe.** The Unified Tracking API serves both products
via the same endpoint but requires the correct `service` query parameter. The
adapter hardcodes `service=ecommerce-europe`. For German domestic shipments
(DHL Parcel DE) the value must be `parcel-de` — these will not resolve correctly
with the current adapter.

**SEPA COD.** DHL eCommerce Europe COD uses SEPA bank transfer — not cash on
delivery to a courier. Requires a valid IBAN and BIC. Only available on the
`DHL Parcel Connect` (ParcelEurope.parcelconnect) product.

**Labelless returns.** The QR code / GIF return flow is DHL Parcel Return
Connect only. The customer uses the code to drop off at a DHL service point
without printing a label. Return instructions (multi-language) must be
provided separately — request from DHL.

**Coverage.** 28 European countries. Not a domestic carrier — intended for
cross-border B2C flows originating from a European hub (typically DE or NL).

---

# DHL eCommerce Americas — Feature Mapping

API: **DHL eCommerce Americas Manifest API v4**
Base URL (prod): `https://api.dhlecs.com`
Base URL (sandbox): `https://api-sandbox.dhlecs.com`
Auth: OAuth2 client credentials (`POST /auth/v4/accesstoken`).
Carrier key: `dhl_ecommerce`
Implementation status: **Manifest only (Beta)**

---

## Summary

DHL eCommerce Americas (formerly DHL eCommerce Solutions) is a separate product
and API from DHL eCommerce Europe. Only end-of-day manifest close is implemented.
Booking, tracking, and label retrieval are not yet wired.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ❌ | Not yet implemented — returns 501 |
| Cancel shipment | ❌ | Not available via API |
| Update shipment | ❌ | Not available via API |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Print label | ❌ | Not yet implemented |
| Label formats | ❌ | Not yet implemented |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ❌ | Not yet implemented |
| Event history | ❌ | Not yet implemented |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ❌ | No API endpoint — handled by standing agreement |
| Update pickup | ❌ | No API endpoint |
| Cancel pickup | ❌ | No API endpoint |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest (specific packages) | ✅ | `POST /shipping/v4/manifest` with `packageIds` — only listed packages are manifested |
| Close manifest (all open packages) | ✅ | `POST /shipping/v4/manifest` with empty `manifests[]` — last 20,000 open labels for the pickup account |
| Manifest document | ✅ | Base64-encoded PDF returned in `GET /shipping/v4/manifest/{pickup}/{requestId}` |
| Invalid package reporting | ✅ | Surfaced as `Warnings` in `ManifestResponse` |

---

## Endpoint mapping

| carrier-gateway | DHL eCommerce Americas API | Status |
|---|---|---|
| `POST /api/bookings` | — | ❌ → 501 |
| `DELETE /api/bookings/{id}` | — | ❌ → 501 |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 |
| `GET /api/trackings/{id}` | — | ❌ → 501 |
| `GET /api/labels/{id}` | — | ❌ → 501 |
| `POST /api/pickups` | — | ❌ → 501 |
| `PUT /api/pickups/{id}` | — | ❌ → 501 |
| `DELETE /api/pickups/{id}` | — | ❌ → 501 |
| `POST /api/manifests` | `POST /shipping/v4/manifest` + `GET /shipping/v4/manifest/{pickup}/{requestId}` | ✅ |

---

## Implementation notes

**Async two-step flow.** Manifest creation is asynchronous. `POST /shipping/v4/manifest`
returns a `requestId` immediately; the adapter polls `GET /shipping/v4/manifest/{pickup}/{requestId}`
every 2 seconds (default) until `status` reaches `COMPLETED`. Default poll
timeout is 60 seconds, configurable via `DHLECSAdapter.PollTimeout`.

**Pickup account number.** Every manifest request requires a DHL eCommerce
pickup account number (`DHLECS_PICKUP_ACCOUNT`). This is separate from the
OAuth2 credentials and is issued by DHL when the account is set up.

**Environment variables.**

| Variable | Required | Description |
|---|---|---|
| `DHLECS_CLIENT_ID` | ✅ | OAuth2 client_id |
| `DHLECS_CLIENT_SECRET` | ✅ | OAuth2 client_secret |
| `DHLECS_PICKUP_ACCOUNT` | ✅ | DHL pickup account number (e.g. `5234567`) |

**Specific vs. all-open manifesting.** Pass `trackingNumbers` in the
`POST /api/manifests` request body to manifest only those package IDs. Omit
`trackingNumbers` (or pass an empty array) to sweep all open packages for
the pickup account. The all-open mode manifests the last 20,000 labels and
may include packages not intended for the current run — use specific IDs
where possible.
