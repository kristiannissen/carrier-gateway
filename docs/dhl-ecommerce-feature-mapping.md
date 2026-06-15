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
Pickup scheduling via the eConnect API is not available — DHL eCommerce Europe uses standing collection agreements. For DHL Parcel DE domestic pickup scheduling, use `DHLParcelDEAdapter` (`dhl_parcel_de.go`). Manifest is not available via eConnect.

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
| Response schema | ✅ | OpenAPI 3.0 spec available at `APIdocs/dhl_unified_track_6.yaml` (v1.5.8). Field mapping is validated against this spec. |
| Status code normalization | ✅ | `normalizeStatus("dhl", statusCode)` maps the five high-level `statusCode` values (`delivered`, `failure`, `pre-transit`, `transit`, `unknown`) from the spec. Full raw event code reference (per service variant) in `APIdocs/dhl_status_1.csv`. |
| Proof of Delivery (POD) | ✅ | `proofOfDelivery.documentUrl`, `signatureUrl`, `signed` (name), and `timestamp` mapped to `TrackingResponse.ProofOfDelivery`. Available from DHL Express and DHL Freight after postal-code validation. Field is omitted when the carrier has not yet confirmed delivery. |
| Piece-level tracking | ❌ | Not implemented. `details.pieceIds[]` and per-event `pieceIds[]` are in the spec but not mapped. |
| Rate limiting | ⚠️ | Default quota is 250 calls/day, 1 per 5 seconds — insufficient for production. No throttle/retry logic in adapter. Request upgrade via DHL developer portal. |
| `service` parameter | ✅ | Configurable via `DHLAdapter.TrackingService`. Defaults to `"ecommerce-europe"`. Set to `""` for API auto-detection, or to any of the 17 valid service values (`express`, `parcel-de`, `parcel-nl`, `freight`, `dgf`, etc.). |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ❌ | No pickup endpoint in the eConnect API — DHL eCommerce Europe uses standing collection agreements. For DHL Parcel DE domestic pickup, use `DHLParcelDEAdapter`. |
| Update pickup | ❌ | Not available via eConnect API |
| Cancel pickup | ❌ | Not available via eConnect API |

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
| Bulky | ✅ | `AddOnBulky` → `features.bulky = true`. Required for shipments outside standard dimensions or that prevent automated sorting. |

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
| `POST /api/pickups` | — | ❌ → use `DHLParcelDEAdapter` for Parcel DE pickup |
| `PUT /api/pickups/{id}` | — | ❌ → use `DHLParcelDEAdapter` |
| `DELETE /api/pickups/{id}` | — | ❌ → use `DHLParcelDEAdapter` |
| `POST /api/manifests` | — | ❌ → 501 (no manifest API) |

---

## Environment variables

| Variable | Description |
|---|---|
| `DHL_CLIENT_ID` | DHL eConnect OAuth2 client ID |
| `DHL_CLIENT_SECRET` | DHL eConnect OAuth2 client secret |
| `DHL_CUSTOMER_ID` | DHL customerIdentification |
| `DHL_TRACKING_API_KEY` | DHL Unified Tracking API subscription key (separate credential) |

---

## Implementation notes

**Two separate credentials.** The eConnect booking API uses OAuth2 (clientID +
clientSecret). The DHL Unified Tracking API uses a separate API key
(`DHL_TRACKING_API_KEY`). Both are required for full functionality.

**Tracking spec.** `APIdocs/dhl_unified_track_6.yaml` is the full OpenAPI 3.0
spec (v1.5.8) for the DHL Unified Tracking API. `APIdocs/dhl_status_1.csv`
provides the exhaustive raw event status code reference for all service variants.
These supersede the earlier `DHL_Shipment Tracking_2_0.yaml` stub (which is a
WADL pointer with no schemas) and `dhl_tracking_unified.md` (narrative overview
only). Use the YAML as the authoritative reference for piece-level tracking and
extended status normalization work.

**Proof of Delivery.** `TrackingResponse.ProofOfDelivery` is populated when the
carrier returns `proofOfDelivery.documentUrl` or `signatureUrl` in the tracking
response. Available for DHL Express and DHL Freight after successful postal-code
validation — the API returns an empty object before delivery is confirmed, which
the adapter treats as absent. The digital signature (hand-held device capture) is
typically available the next business day after delivery. `signedBy` is resolved
from `signed.name`, falling back to `givenName + familyName`, then
`organizationName`.

**Tracking rate limits.** The initial quota on a new DHL API key is 250 calls/day
with a 1 call/5 second burst limit. This is only suitable for development. Request
a production upgrade via the DHL developer portal before going live. The adapter
has no retry or throttle logic — a 429 response will surface as an error.

**Service hint (`TrackingService`).** `DHLAdapter.TrackingService` controls the
`service` query parameter on tracking requests. It defaults to `"ecommerce-europe"`.
Override it on the struct for other DHL service variants (e.g. `"parcel-de"` for
German domestic, `"express"` for DHL Express, `"freight"` for road freight).
Set to `""` to omit the hint entirely and let the API auto-detect — slower but
correct when the service is unknown. All 17 valid values are listed in the struct
godoc and in the `Service` enum of `APIdocs/dhl_unified_track_6.yaml`.

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

---

# DHL eCommerce UK — Feature Mapping

API: **DHL eCommerce UK API (parceluk)**
Base URL (prod): `https://api.dhl.com/parceluk`
Base URL (UAT): `https://api-uat.dhl.com/parceluk`
Auth: OAuth2 client credentials (`POST /auth/v1/accesstoken`, 60-min tokens).
Carrier key: `dhl_ecommerce_uk`
Implementation status: **Beta**
Spec: `APIdocs/DHL_ecom_UK.json`

---

## Summary

DHL eCommerce UK is a distinct product and API from DHL eCommerce Europe and
DHL eCommerce Americas — separate base URL, separate auth endpoint path, and
a different API surface. Booking, label retrieval, tracking, cancellation, and
pickup booking are all implemented. There is no manifest API on the UK platform.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | `POST /shipping/v1/label?includeLabel=INCLUDE` — label returned inline |
| Cancel shipment | ✅ | `POST /shipping/v1/cancellation` — consignee postal code cached at booking time |
| Update shipment | ❌ | `POST /shipping/v1/amendment` schema is incompatible with `UpdateRequest` — returns 501 |
| Idempotency key | ❌ | Client-side only |
| Multi-colli | ⚠️ | API accepts exactly one shipment per request — adapter fans out one call per colli |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Print label | ✅ | Inline in booking response (base64) |
| Fetch/reprint label | ✅ | `GET /reprintlabels/v1/labels` |
| Label formats | ✅ | ZPL, PDF, PNG (PNG_RAW to avoid ZIP wrapper). EPL not supported |
| Page size | ⚠️ | `label6x4` used by default. A4 available for UK domestic only — not wired |
| Label DPI | ⚠️ | Defaults to 203 dpi — 300 dpi not yet exposed |
| Return label (in-box) | ✅ | `inBoxReturn=true` — requires `DHLECS_UK_RETURN_ACCOUNT` |
| Service-point drop-off barcode | ❌ | `GET /reprintlabels/v1/servicepoint-dropoff-uk` not wired |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ✅ | `GET /tracking/v1/shipments?trackingNumber=...&service=parcelUk` |
| Event history | ✅ | `events[]` returned in response |
| Status normalization | ✅ | `"dhl_ecommerce_uk"` entry in `normalizedStatuses` — 5 codes: `pre-transit`, `transit`, `delivered`, `failure`, `unknown` |
| Estimated delivery | ✅ | `estimatedDeliveryTime` where returned by carrier |
| Proof of delivery | ❌ | `proofOfDelivery.signatureUrl` / `documentUrl` available in API but not surfaced |
| Piece-level tracking | ❌ | `GET /tracking/v1/pieces` not wired |
| Digital assets (signature photo) | ❌ | `GET /tracking/v1/images/{imageId}` not wired |
| Webhooks / push events | ❌ | API is pull-only — no webhook registration endpoint |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ✅ | `POST /pickup/v1/pickup` — requires `DHLECS_UK_TRADING_LOCATION_ID` |
| Update pickup | ❌ | No update endpoint — cancel and rebook |
| Cancel pickup | ❌ | No API endpoint — contact DHL customer service |
| Get pickup availability | ❌ | Not supported — proceed to BookPickup directly |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❌ | DHL eCommerce UK has no manifest or end-of-day closeout API. Shipments are processed automatically |

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| SMS notification | ❌ | Not supported — DHL sends pre-delivery notifications to consignee email automatically |
| Email notification | ⚠️ | Automatic when `consigneeAddress.email` is set — no explicit add-on mapping needed |
| Flex delivery | ❌ | Not available on DHL eCommerce UK |
| Signature required | ✅ | `deliveryChoice=SIG` — maps `signature_required` add-on |
| Age verification | ❌ | `deliveryChoice=AGE` available in API but not wired to an add-on |
| Cash on delivery | ❌ | `exchangeOnDelivery` (UK only) not wired — ignored with warning logged |
| Insurance / extended liability | ✅ | `extendedLiabilityUnits=1` — maps `insurance` add-on when `InsuranceValue > 0` |

### Customs

| Feature | Implemented | Notes |
|---|---|---|
| HS code | ✅ | Normalised to 8-digit no-dot format (`dhlUKHSCode`) |
| Country of origin | ✅ | Falls back from item to shipment-level |
| Item-level customs | ✅ | `customsDetails[]` per piece |
| DDP / DAP | ✅ | `Customs.Incoterms` → `shipmentDetails.dutiesPaid` |
| IOSS | ✅ | `iossShipment=true` + sender registration `type=IOSS` |
| Customs invoice | ✅ | `reasonForExport` mapped from `NatureOfCargo`; invoice number/date forwarded |
| EORI (sender/recipient) | ✅ | Mapped to `senderCustomsRegistrations` / `recipientCustomsRegistrations` |
| VAT (sender/recipient) | ✅ | Mapped to respective customs registration arrays |
| Windsor Framework (GB → NI) | ❌ | `clearanceDeclaration` (C2C/C2B/B2C/B2B) not wired — no `Customs` fields for this yet |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Service point delivery | ✅ | `receiver.servicePointId` → `consigneeAddress.locationId` + `addressType=servicePoint` |
| What3Words delivery | ❌ | `consigneeAddress.what3words` available in API but not wired |
| Carriage-forward / third-party collection | ❌ | `carriageForward=true` flow not wired |
| Product/service code lookup | ❌ | `GET /referencedata/v2/products` not wired — product code is a static env var |
| Service point finder | ❌ | `GET /servicepoint/v1/find-by-address` not wired |
| Customer price block | ❌ | `customerPrice` (VAT reporting) not wired |

---

## Cancellation caveat

The DHL UK cancellation endpoint requires the consignee postal code alongside
the shipment ID. The `CarrierAdapter.CancelShipment` interface only exposes the
tracking number. To bridge this gap the adapter caches `shipmentID → postalCode`
at booking time. Shipments booked outside this process (e.g. in a different pod
or after a restart) cannot be cancelled via the API interface — contact DHL
customer service in those cases.

Shipments already scanned at a DHL depot cannot be cancelled. If a cancellation
was requested before scanning but the parcel is later scanned anyway, DHL
automatically recreates the shipment.

---

## Endpoint mapping

| carrier-gateway | DHL eCommerce UK API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /shipping/v1/label` | ✅ |
| `DELETE /api/bookings/{id}` | `POST /shipping/v1/cancellation` | ✅ |
| `PATCH /api/bookings/{id}` | `POST /shipping/v1/amendment` | ❌ → 501 (schema mismatch) |
| `GET /api/trackings/{id}` | `GET /tracking/v1/shipments` | ✅ |
| `GET /api/labels/{id}` | `GET /reprintlabels/v1/labels` | ✅ |
| `POST /api/pickups` | `POST /pickup/v1/pickup` | ✅ |
| `PUT /api/pickups/{id}` | — | ❌ → 501 |
| `DELETE /api/pickups/{id}` | — | ❌ → 501 |
| `POST /api/manifests` | — | ❌ → 501 (no manifest API) |

---

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `DHLECS_UK_CLIENT_ID` | ✅ | OAuth2 client_id |
| `DHLECS_UK_CLIENT_SECRET` | ✅ | OAuth2 client_secret |
| `DHLECS_UK_PICKUP_ACCOUNT` | ✅ | DHL account number (used as `pickupAccount` on all requests) |
| `DHLECS_UK_ORDERED_PRODUCT` | ❌ | 3-digit product/service code (default: `220` — Signature At Address Next Day) |
| `DHLECS_UK_TRADING_LOCATION_ID` | ❌ | Customer trading location ID — required for pickup booking |
| `DHLECS_UK_RETURN_ACCOUNT` | ❌ | DHL return account number — required when `DeliveryType=return` |
| `DHLECS_UK_RETURN_PRODUCT` | ❌ | Product code for return shipments (defaults to `DHLECS_UK_ORDERED_PRODUCT`) |

## UAT test accounts

| Account type | Pickup account | Drop-off account |
|---|---|---|
| UK domestic | `F020579` | `F020582` |
| International road | `F820579` | `F820582` |
| International air | `F520579` | `F520582` |

UAT base URL: `https://api-uat.dhl.com/parceluk` — set `DHLEcomUKAdapter.BaseURL` directly or use a sandbox constructor.

---

# DHL Parcel DE — Pickup Scheduling

API: **DHL Parcel DE Pickup API v3.0.0**
Base URL (prod): `https://api-eu.dhl.com/parcel/de/transportation/pickup/v3`
Auth: ROPC Bearer token (`POST /parcel/de/user/v1/authenticate/apitoken`, `grant_type=password`).
Carrier key: `dhl_parcel_de`
Implementation status: **Pickup scheduling implemented (Beta)**

---

## Summary

DHL Parcel DE (DHL Paket) is the domestic German parcel product — separate from
DHL Express and DHL eCommerce Europe. This adapter covers pickup scheduling only
via `DHLParcelDEAdapter` (`dhl_parcel_de.go`). Shipment booking, tracking, and
labels are not yet implemented.

Authentication uses ROPC (Resource Owner Password Credentials), a third separate
credential set from the eConnect OAuth2 (booking) and Unified Tracking API key.

---

## Feature fit/gap

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup (Bedarfsabholung) | ✅ | Agreed location (AsID), ≤10 parcels, free of charge. Auto-selected when `AsID` is set on adapter. |
| Book pickup (Einmalige Abholung) | ✅ | Agreed location (AsID), >10 parcels or bulky goods. Auto-selected by DHL when `AsID` is set and parcel count exceeds threshold. |
| Book pickup (Einzelabholung) | ✅ | Any German address, paid service. Auto-selected when `BillingNumber` is set and `AsID` is empty. Requires street, postal code, and city in `PickupRequest.Address`. |
| Update pickup | ✅ | Cancel + rebook — no dedicated update endpoint in the API. Response status is `"updated"`. |
| Cancel pickup | ✅ | `DELETE /orders?orderID=...` |
| Get pickup availability | ❌ | Returns `ErrNotSupported` — proceed to `BookPickup` directly; the API validates the date. |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❌ | Returns `ErrNotSupported` — DHL Parcel DE processes shipments automatically |

---

## Endpoint mapping

| carrier-gateway | DHL Parcel DE Pickup API | Status |
|---|---|---|
| `POST /api/pickups` | `POST /orders` | ✅ |
| `PUT /api/pickups/{id}` | `DELETE /orders` + `POST /orders` | ✅ (cancel + rebook) |
| `DELETE /api/pickups/{id}` | `DELETE /orders?orderID=...` | ✅ |
| `POST /api/manifests` | — | ❌ → 501 |

---

## Implementation notes

**Three credential sets.** DHL Parcel DE pickup uses ROPC auth — separate from
both the eConnect OAuth2 credentials (booking) and the Unified Tracking API key
(tracking). Configure `Username`, `Password`, `ClientID`, and `ClientSecret` on
`DHLParcelDEAdapter`.

**Pickup type auto-detection.** The adapter selects the pickup type from adapter
configuration, not from the request:

- `AsID` set → Bedarfsabholung (BDA) or Einmalige Abholung (EMA); DHL determines
  which based on parcel count and transport type.
- `AsID` empty + `BillingNumber` set → Einzelabholung (EZA) at the address in
  `PickupRequest.Address`.
- Neither set → error at `BookPickup` time.

**Transport type.** Defaults to `"PAKET"`. Override via
`DHLParcelDEAdapter.DefaultTransportationType` for pallets, roll containers, or
bulky goods (`ROLLBEHAELTER`, `WECHSELBEHAELTER`, `PALETTEN`, `SPERRGUT`).

**Environment variables.**

| Variable | Required | Description |
|---|---|---|
| `DHL_PARCEL_DE_USERNAME` | ✅ | ROPC username (DHL customer portal login) |
| `DHL_PARCEL_DE_PASSWORD` | ✅ | ROPC password |
| `DHL_PARCEL_DE_CLIENT_ID` | ✅ | OAuth2 client_id |
| `DHL_PARCEL_DE_CLIENT_SECRET` | ✅ | OAuth2 client_secret |
| `DHL_PARCEL_DE_AS_ID` | ❌ | Agreed service point ID (e.g. `AS1234567890`) — required for BDA/EMA |
| `DHL_PARCEL_DE_BILLING_NUMBER` | ❌ | Billing number (e.g. `123456789001AB`) — required for EZA |
| `DHL_PARCEL_DE_TRANSPORT_TYPE` | ❌ | Transport type (default: `PAKET`) |
