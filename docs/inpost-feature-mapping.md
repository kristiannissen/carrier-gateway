# InPost — Feature Mapping

API: **InPost Group API 2025**
Base URL: `https://api.inpost-group.com`
Auth: OAuth 2.1 (Bearer token)
Coverage: Poland (shipping + pickups + returns), Italy + UK (returns only)
Implementation status: **Needs migration** — adapter targets old ShipX (`api.inpost.pl/v1`); new API has breaking URL and payload changes

---

## Summary

The new InPost Group API 2025 (`api.inpost-group.com`) replaces ShipX. It adds
a full Pickups API (Poland only), a Returns API (PL/IT/UK), richer label
formats, idempotency via `X-Deduplication-Id`, multi-format tracking with batch
support, and customs clearance for cross-border shipments. The current adapter
targets the old ShipX URL and payload structure — it must be migrated before any
live traffic can flow.

---

## Feature fit/gap

### Booking (`/shipping/v2/`)

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Book shipment | ✅ | ⚠️ Wrong URL + payload | Adapter hits old ShipX endpoint; migrate to `POST /shipping/v2/organizations/{orgId}/shipments` |
| Cancel shipment | ❌ | ❌ → 501 | Not available in the new API either — returns 501 is correct |
| Update shipment | ❌ | ❌ → 501 | Not available — 501 is correct |
| Idempotency key | ✅ | ❌ Not wired | New API accepts `X-Deduplication-Id` UUID header; adapter currently stuffs key into `reference` field |
| APM (locker) destination | ✅ | ⚠️ Wrong field | New API uses `destination.pointId`; adapter uses `service.targetLocker` (ShipX) |
| Home delivery destination | ✅ | ❌ Not wired | New API supports `destination` as a street address — not exposed in current adapter |
| Drop-off code (label-less) | ✅ | ❌ Not wired | `enableDropOffCode: true` lets sender drop off without printing a label |
| Multiple parcels | ✅ (up to 99) | ✅ | Colli → parcels mapping is structurally compatible |
| Custom references | ✅ | ❌ Not wired | New API accepts `references.custom` at shipment and parcel level (invoiceNumber, orderId, etc.) |
| Value-added services | ✅ | ❌ Not wired | `valueAddedServices` array (e.g. priority); `productVariant` and `brand` fields |
| Return-to-sender address | ✅ (PL only) | ❌ Not wired | `returnDestination` field on the shipment — Poland domestic only |
| Customs clearance | ✅ | ❌ Not wired | `customsClearance` at shipment and parcel level; required for GB↔EU and non-EU cross-border |

### Labels (`/shipping/v2/` + `/returns/v1/`)

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| PDF A4 / A6 | ✅ | ⚠️ PDF only, wrong URL | Adapter sends PDF request to old ShipX URL; migrate to new endpoint |
| ZPL 203 dpi / 300 dpi | ✅ | ❌ Not wired | New API returns ZPL as plain text or JSON+Base64 |
| EPL2 203 dpi | ✅ (PL domestic) | ❌ Not wired | Poland domestic only |
| DPL 203 dpi | ✅ (PL domestic, pilot) | ❌ Not wired | Pilot — contact Integrations team |
| JSON+Base64 wrapper | ✅ (all formats) | ❌ Not wired | Useful for programmatic storage before printing |
| Return label (PDF/ZPL/EPL2) | ✅ | ❌ Not wired | Separate endpoint under `/returns/v1/`; no A6 or DPL variants |

### Tracking (`/tracking/v1/`)

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Event history | ✅ | ⚠️ Wrong URL + shape | Old adapter calls `/tracking/{id}`; new API is `GET /tracking/v1/parcels?trackingNumbers=...` |
| Batch tracking (up to 10) | ✅ | ❌ Not wired | Single-number adapter calls can be batched; max 10 per request |
| Event versioning | ✅ | ❌ Not wired | Pass `x-inpost-event-version: V1` header; V1 supported indefinitely |
| Data retention | ⚠️ 121 days | — | Tracking data unavailable after 121 days — document as a known constraint |

### Pickups (`/pickups/v1/`) — Poland only

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Book one-time pickup | ✅ | ❌ Not implemented | `POST /pickups/v1/organizations/{orgId}/one-time-pickups` |
| Get pickup by ID | ✅ | ❌ Not implemented | |
| List pickups (paged) | ✅ | ❌ Not implemented | Sortable, paginated |
| Cancel pickup | ✅ | ❌ Not implemented | `PUT .../cancel` — merchant-initiated only |
| Get cutoff time | ✅ | ❌ Not implemented | Per postal code; required for same-day pickup eligibility check |
| Recyclable packaging pickup | ✅ | ❌ Not implemented | `itemType: RECYCLABLE_PACKAGING` variant |
| Recurring pickups | ❌ | ❌ N/A | Must be arranged with account manager — not available via API |
| Geographic availability | PL only | — | Pickup API not available for UK, IT, or other markets |

### Returns (`/returns/v1/`) — PL, IT, UK only

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Create return shipment | ✅ | ❌ Not implemented | `POST /returns/v1/organizations/{orgId}/shipments`; minimal request needs only `sender` |
| Get return shipment info | ✅ | ❌ Not implemented | |
| Return label retrieval | ✅ | ❌ Not implemented | PDF A4, ZPL 203/300, EPL2 203 only — no A6, no DPL, no JSON+Base64 |
| Drop-off code (label-less return) | ✅ | ❌ Not implemented | `enableDropOffCode: true` (default) — customer uses short code at locker |
| Expiration date | ✅ | ❌ Not implemented | Optional; minimum 7 days from creation |
| Cancel return | ❌ | ❌ N/A | Not documented in the API |
| Geographic availability | PL / IT / UK | — | Other markets not yet supported |

### Manifest

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Close manifest | ❌ | ❌ → 501 | Not applicable for locker/drop-off network — 501 is correct |

---

## Endpoint mapping

| carrier-gateway | New InPost Group API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /shipping/v2/organizations/{orgId}/shipments` | ⚠️ Needs migration |
| `DELETE /api/bookings/{id}` | — | ❌ → 501 (correct) |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 (correct) |
| `GET /api/trackings/{id}` | `GET /tracking/v1/parcels?trackingNumbers={id}` | ⚠️ Needs migration |
| `GET /api/labels/{id}` | `GET /shipping/v2/organizations/{orgId}/shipments/{id}/label` | ⚠️ Needs migration |
| `POST /api/pickups` | `POST /pickups/v1/organizations/{orgId}/one-time-pickups` | ❌ Not implemented |
| `DELETE /api/pickups/{id}` | `PUT /pickups/v1/organizations/{orgId}/one-time-pickups/{id}/cancel` | ❌ Not implemented |
| `POST /api/returns` | `POST /returns/v1/organizations/{orgId}/shipments` | ❌ Not implemented |
| `GET /api/labels/{id}` (return) | `GET /returns/v1/organizations/{orgId}/shipments/{id}/label` | ❌ Not implemented |
| `POST /api/manifests` | — | ❌ → 501 (correct) |

### Authentication

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| OAuth 2.1 Client Credentials | ✅ | ❌ Not wired | Right flow for M2M/backend; replaces static API key |
| Token expiry / refresh | ✅ | ❌ Not wired | Access token expires in ~10 min (599s); adapter needs proactive refresh logic |
| OAuth 2.1 PKCE flow | ✅ | ❌ N/A | For broker/multi-tenant apps only — not needed for the gateway |
| Staging environment | ✅ | ❌ Not configured | `stage-api.inpost-group.com` available for integration testing |
| Required scopes | — | ❌ Not defined | Must request all needed scopes at token creation: `api:shipments:read`, `api:shipments:write`, `api:returns:read`, `api:returns:write`, `api:one-time-pickups:read`, `api:one-time-pickups:write`, `api:tracking:read`, `api:points:read` |

### Customs clearance (UK ecosystem — `/shipping/v2/`)

Customs requirements are determined by origin + destination subdivision. Four rule types apply:

| Rule | Trigger | Key mandatory fields |
|---|---|---|
| No customs | Domestic GB/IM, NI→GB (B2C) | None |
| Type 1 | GB-GBN → GB-NIR, IM → GB-NIR | `contents[].description`, `.quantity`, `.unitValue` |
| Type 2 | Any → GG or JE (Channel Islands) | Above + `incoterm`, `exportReason`, `shippingCost`, `productOriginCountryCode` |
| Type 3 | Any → IE | Above + `eoriNumber`, `hsCode` |

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| Customs object on shipment | ✅ | ❌ Not wired | `incoterm`, `eoriNumber`, `exportReason`, `shippingCost`, `invoice` |
| Customs per parcel | ✅ | ❌ Not wired | `value`, `currency`, `contentsDescription`, `contents[]` (max 10 items) |
| Subdivision code validation | ✅ | ❌ Not wired | Only GB-ENG, GB-SCT, GB-WLS, GB-GBN, GB-NIR accepted — no sub-regions |
| GG / JE origin shipping | ❌ | ❌ N/A | InPost does not offer outbound from Channel Islands |
| British Overseas Territories | ❌ | ❌ N/A | Not supported by InPost logistics |

### Error handling & observability

| Feature | API support | Adapter status | Notes |
|---|---|---|---|
| `X-Request-Id` tracing | ✅ | ❌ Not wired | Client can inject UUID; echoed back in response header — wire to gateway's request ID middleware |
| 406 Not Acceptable | ✅ | ❌ Not handled | Returned when an unsupported `Accept` header is sent (e.g. unsupported label format) — map to gateway 400 |
| 422 Unprocessable Entity | ✅ | ❌ Not handled | Field-level validation failure — map to gateway 422 with upstream body |
| 429 Too Many Requests | ✅ | ❌ Not handled | Rate limit hit — adapter should propagate or back off |
| 202 Accepted (async) | ✅ | ❌ Not handled | Indicates async processing; polling or webhook needed for final status |

---

## Endpoint mapping

| carrier-gateway | New InPost Group API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /shipping/v2/organizations/{orgId}/shipments` | ⚠️ Needs migration |
| `DELETE /api/bookings/{id}` | — | ❌ → 501 (correct) |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 (correct) |
| `GET /api/trackings/{id}` | `GET /tracking/v1/parcels?trackingNumbers={id}` | ⚠️ Needs migration |
| `GET /api/labels/{id}` | `GET /shipping/v2/organizations/{orgId}/shipments/{id}/label` | ⚠️ Needs migration |
| `POST /api/pickups` | `POST /pickups/v1/organizations/{orgId}/one-time-pickups` | ❌ Not implemented |
| `DELETE /api/pickups/{id}` | `PUT /pickups/v1/organizations/{orgId}/one-time-pickups/{id}/cancel` | ❌ Not implemented |
| `POST /api/returns` | `POST /returns/v1/organizations/{orgId}/shipments` | ❌ Not implemented |
| `GET /api/labels/{id}` (return) | `GET /returns/v1/organizations/{orgId}/shipments/{id}/label` | ❌ Not implemented |
| `POST /api/manifests` | — | ❌ → 501 (correct) |

---

## Implementation notes

**API migration required.** The adapter must move from `api.inpost.pl/v1` to
`api.inpost-group.com` and adopt OAuth 2.1 Client Credentials. The shipment
payload shape has changed significantly — `service.targetLocker` becomes
`destination.pointId`, and the top-level `shipment` wrapper is gone.

**OAuth 2.1 token management.** The adapter currently uses a static API key.
Replace with a token client that fetches via `POST /oauth2/token`
(`grant_type=client_credentials`) and refreshes before expiry (~10 min). Store
`client_id`, `client_secret`, and `organizationId` in config; do not hardcode scopes
— request all needed scopes at token creation time.

**organizationId is now required.** All v2 endpoints are scoped to an
`organizationId` path parameter (available in the Merchant Portal). This must
be added to the adapter config alongside the OAuth credentials.

**Wire `X-Request-Id`.** The gateway already has a request ID middleware. Forward
the gateway's request ID to InPost as `X-Request-Id` so responses are traceable
end-to-end.

**Customs gating.** Before calling the shipping API for GB/IM routes, determine
the customs rule type from the origin + destination subdivision and populate the
mandatory fields accordingly. Reject calls with GG/JE/IE origins — InPost does
not serve outbound from those territories.

**Pickups are PL-only and require a logistics contract.** Gate on `countryCode == "PL"`
before calling the pickup API. Same-day pickups require a cutoff-time check first.

**Returns are PL/IT/UK only.** Validate sender country before calling the returns API.

**To go live.** Obtain OAuth 2.1 credentials and `organizationId` from the
Merchant Portal (or Integration Team), configure the staging environment
(`stage-api.inpost-group.com`) first, migrate the payload mappers, wire
`X-Deduplication-Id` and `X-Request-Id`, and run integration tests.
