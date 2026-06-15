# InPost — Feature Mapping

API: **InPost Group API 2025**
Base URL: `https://api.inpost-group.com`
Auth: OAuth 2.1 (Bearer token)
Coverage: Poland (shipping + pickups + returns), Italy + UK (returns only)
Implementation status: **Production-ready** — adapter targets the InPost Group API 2025

---

## Summary

The InPost adapter targets the InPost Group API 2025 (`api.inpost-group.com`). It supports
shipping, label retrieval, and tracking for all InPost markets; pickups for Poland; and
returns for Poland, Italy, and the United Kingdom. OAuth 2.1 Client Credentials tokens are
cached in-process and refreshed automatically.

---

## Feature fit/gap

### Booking (`/shipping/v2/`)

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Book shipment | ✅ | ✅ Implemented | `POST /shipping/v2/organizations/{orgId}/shipments` |
| Cancel shipment | ❌ | ❌ → 501 | Not available in the InPost API — 501 is correct |
| Update shipment | ❌ | ❌ → 501 | Not available — 501 is correct |
| Idempotency key | ✅ | ✅ Implemented | `X-Deduplication-Id` header from `IdempotencyKey` |
| APM (locker) destination | ✅ | ✅ Implemented | `destination.pointId` from `Receiver.ServicePointID` |
| Home delivery destination | ✅ | ✅ Implemented | `destination` as street address when no ServicePointID |
| Drop-off code (label-less) | ✅ | ✅ Implemented | `enableDropOffCode: true` when `ReturnFunctionality == "labelless"` |
| Multiple parcels | ✅ (up to 99) | ✅ Implemented | Each Colli maps to one parcel |
| Custom references | ✅ | ✅ Implemented | `references.custom.invoiceNumber` from IdempotencyKey; `externalId` per colli |
| Value-added services | ✅ | ✅ Implemented | `valueAddedServices` from `Shipment.ValueAddedServices`; `productVariant` from `Shipment.ServiceTier`; `brand` from `Shipment.Brand` |
| Return-to-sender address | ✅ (PL only) | ✅ Implemented | `returnDestination` from `Shipment.ReturnAddress` — wired for PL sender country only |
| Customs clearance | ✅ | ✅ Implemented | `customsClearance` at shipment and parcel level; max 10 contents items |

### Labels (`/shipping/v2/` + `/returns/v1/`)

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| PDF A4 / A6 | ✅ | ✅ Implemented | Default A6 for shipments; A4 for returns |
| ZPL 203 dpi | ✅ | ✅ Implemented | `text/zpl;dpi=203` |
| ZPL 300 dpi | ✅ | ✅ Implemented | `text/zpl;dpi=300` (LabelFormatZPLGK) |
| EPL2 203 dpi | ✅ (PL domestic) | ✅ Implemented | Poland domestic only |
| DPL 203 dpi | ✅ (PL domestic, pilot) | ✅ Implemented | `LabelFormatDPL` → `text/dpl;dpi=203`; pilot — contact InPost Integrations team before use |
| JSON+Base64 wrapper | ✅ (all formats) | ❌ Not wired | Useful for programmatic storage before printing |
| Return label (PDF/ZPL/EPL2) | ✅ | ✅ Implemented | `GET /returns/v1/.../label`; no A6, no DPL, no JSON+Base64 |

### Tracking (`/tracking/v1/`)

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Event history | ✅ | ✅ Implemented | `GET /tracking/v1/parcels?trackingNumbers=...` |
| Event versioning | ✅ | ✅ Implemented | `x-inpost-event-version: V1` header on all requests |
| Batch tracking (up to 10) | ✅ | ❌ Not wired | Gateway calls one-by-one; batching is an optimisation opportunity |
| Data retention | ⚠️ 121 days | — | Tracking data unavailable after 121 days — known constraint |

### Pickups (`/pickups/v1/`) — Poland only

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Book one-time pickup | ✅ | ✅ Implemented | `POST /pickups/v1/organizations/{orgId}/one-time-pickups`; PL-only gate |
| Cancel pickup | ✅ | ✅ Implemented | `PUT .../cancel` |
| Update pickup | ❌ | ❌ → 501 | No update endpoint — cancel and rebook |
| Get pickup by ID | ✅ | ✅ Implemented | `GET /api/pickups/{id}?carrier=inpost` → `PickupQuerier.GetPickupByID` |
| List pickups (paged) | ✅ | ✅ Implemented | `GET /api/pickups?carrier=inpost&page=&size=&sort=` → `PickupQuerier.ListPickups` |
| Get cutoff time | ✅ | ✅ Implemented | `GET /api/pickups/cutoff-time?carrier=inpost&postalCode=&countryCode=` → `PickupQuerier.GetCutoffTime` |
| Pickup availability slots | ❌ | ❌ → 501 | InPost exposes cutoff time, not a slot grid; GetPickupAvailability returns 501 |
| Recyclable packaging pickup | ✅ | ✅ Implemented | `PickupRequest.ItemType = "RECYCLABLE_PACKAGING"` → `itemType` in volume object |
| Recurring pickups | ❌ | ❌ N/A | Must be arranged with account manager — not available via API |
| Geographic availability | PL only | — | Pickup API not available for UK, IT, or other markets |

### Returns (`/returns/v1/`) — PL, IT, GB only

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Create return shipment | ✅ | ✅ Implemented | `POST /returns/v1/organizations/{orgId}/shipments`; country gate enforced |
| Drop-off code (label-less) | ✅ | ✅ Implemented | `enableDropOffCode` forwarded from request |
| GB subdivision code | ✅ | ✅ Implemented | `origin.subdivisionCode` from `Sender.State` |
| Parcel dimensions/weight | ✅ | ✅ Implemented | First colli mapped; additional colli silently dropped (API accepts exactly 1) |
| Expiration date | ✅ | ✅ Implemented | `expirationDate` forwarded from `ReturnRequest.ExpiresAt` |
| Return label retrieval | ✅ | ✅ Implemented | `GET /api/returns/{trackingNumber}/label?carrier=inpost` |
| Get return shipment info | ✅ | ✅ Implemented | `GET /api/returns/{id}?carrier=inpost` → `ReturnQuerier.GetReturnShipment` |
| Cancel return | ❌ | ❌ N/A | Not documented in the InPost API |
| Geographic availability | PL / IT / GB | — | Other markets not yet supported |

### Manifest

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Close manifest | ❌ | ❌ → 501 | Not applicable for locker/drop-off network — 501 is correct |

---

## Authentication

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| OAuth 2.1 Client Credentials | ✅ | ✅ Implemented | In-process token cache; 30s expiry buffer |
| Token expiry / refresh | ✅ | ✅ Implemented | Refreshed automatically before expiry |
| All required scopes | ✅ | ✅ Implemented | All 8 scopes requested at token creation time |
| Staging environment | ✅ | ❌ Not configured | `stage-api.inpost-group.com` for integration testing |

## Observability

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| `X-Request-Id` forwarding | ✅ | ✅ Implemented | Gateway request ID forwarded to InPost on all outbound calls |
| 406 Not Acceptable | ✅ | ✅ Handled | Mapped to descriptive error message |
| 422 Unprocessable Entity | ✅ | ✅ Handled | Body included in error |
| 429 Too Many Requests | ✅ | ✅ Handled | Propagated as error |
| 202 Accepted (async) | ✅ | ❌ Not handled | Not observed in production yet; polling/webhook not yet wired |

---

## Customs clearance (UK ecosystem — `/shipping/v2/`)

Customs requirements are determined by origin + destination subdivision. Four rule types apply:

| Rule | Trigger | Key mandatory fields |
|---|---|---|
| No customs | Domestic GB/IM, NI→GB (B2C) | None |
| Type 1 | GB-GBN → GB-NIR, IM → GB-NIR | `contents[].description`, `.quantity`, `.unitValue` |
| Type 2 | Any → GG or JE (Channel Islands) | Above + `incoterm`, `exportReason`, `shippingCost`, `productOriginCountryCode` |
| Type 3 | Any → IE | Above + `eoriNumber`, `hsCode` |

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Customs object on shipment | ✅ | ✅ Implemented | `incoterm`, `eoriNumber`, `exportReason`, `shippingCost`, `invoice` |
| Customs per parcel | ✅ | ✅ Implemented | `value`, `currency`, `contents[]` (max 10 items) |
| Subdivision code | ✅ | ✅ Implemented | Only GB-ENG, GB-SCT, GB-WLS, GB-GBN, GB-NIR accepted |
| GG / JE origin shipping | ❌ | ❌ N/A | InPost does not offer outbound from Channel Islands |
| British Overseas Territories | ❌ | ❌ N/A | Not supported by InPost |

---

## Endpoint mapping

| carrier-gateway | InPost Group API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /shipping/v2/organizations/{orgId}/shipments` | ✅ Implemented |
| `DELETE /api/bookings/{id}` | — | ❌ → 501 (correct) |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 (correct) |
| `GET /api/trackings/{id}` | `GET /tracking/v1/parcels?trackingNumbers={id}` | ✅ Implemented |
| `GET /api/labels/{id}` | `GET /shipping/v2/organizations/{orgId}/shipments/{id}/label` | ✅ Implemented |
| `POST /api/pickups` | `POST /pickups/v1/organizations/{orgId}/one-time-pickups` | ✅ Implemented (PL only) |
| `GET /api/pickups` | `GET /pickups/v1/organizations/{orgId}/one-time-pickups` | ✅ Implemented |
| `GET /api/pickups/{id}` | `GET /pickups/v1/organizations/{orgId}/one-time-pickups/{id}` | ✅ Implemented |
| `GET /api/pickups/cutoff-time` | `GET /pickups/v1/cutoff-time` | ✅ Implemented |
| `DELETE /api/pickups/{id}` | `PUT /pickups/v1/organizations/{orgId}/one-time-pickups/{id}/cancel` | ✅ Implemented |
| `PUT /api/pickups/{id}` | — | ❌ → 501 (no update endpoint) |
| `POST /api/returns` | `POST /returns/v1/organizations/{orgId}/shipments` | ✅ Implemented (PL/IT/GB) |
| `GET /api/returns/{id}` | `GET /returns/v1/organizations/{orgId}/shipments/{id}` | ✅ Implemented |
| `GET /api/returns/{id}/label` | `GET /returns/v1/organizations/{orgId}/shipments/{id}/label` | ✅ Implemented |
| `POST /api/manifests` | — | ❌ → 501 (correct) |

---

## Environment variables

| Variable | Description |
|---|---|
| `INPOST_CLIENT_ID` | InPost Group API OAuth 2.1 client ID |
| `INPOST_CLIENT_SECRET` | InPost Group API OAuth 2.1 client secret |
| `INPOST_ORG_ID` | InPost organization ID (from Merchant Portal) |

---

## Implementation notes

**OAuth 2.1 token management.** Tokens are cached in-process with a 30-second expiry buffer.
The same pattern is used by the Hermes adapter. All 8 API scopes are requested at token
creation time so a single token covers all operations.

**organizationId is required.** All v2/v1 endpoints are scoped to `INPOST_ORG_ID`. Set this
alongside `INPOST_CLIENT_ID` and `INPOST_CLIENT_SECRET`; the adapter falls back to mock mode
if any of the three are absent.

**Pickups are PL-only.** A country gate on `Address.Country` enforces this before any API
call. Same-day pickups require a cutoff-time pre-check — use
`GET /api/pickups/cutoff-time?carrier=inpost&postalCode=&countryCode=PL` before calling
`POST /api/pickups` to verify the window is still open.

**Returns are PL/IT/GB only.** `Sender.Country` is checked before calling the API.
GB shipments must include `Sender.State` (e.g. `"GB-ENG"`) for subdivision routing.
The Returns API accepts exactly one parcel; if `Colli` contains more than one, only the
first is forwarded.

**Return label uses A4, not A6.** The shipping label endpoint accepts A6; the returns label
endpoint does not. `FetchReturnLabel` defaults to A4 automatically.

**To go live.** Obtain OAuth 2.1 credentials and `organizationId` from the InPost Merchant
Portal, set `INPOST_CLIENT_ID`, `INPOST_CLIENT_SECRET`, `INPOST_ORG_ID` in your environment,
and test against `stage-api.inpost-group.com` first (override `BaseURL` and `AuthURL` on the
adapter or set a staging env var).
