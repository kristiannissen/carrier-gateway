# DPD — Fit/Gap Analysis

API: **DPD Baltic Shipping API v1.2.1**
Base URLs (prod):
- Lithuania: `https://esiunta.dpd.lt/api/v1`
- Latvia: `https://eserviss.dpd.lv/api/v1`
- Estonia: `https://telli.dpd.ee/api/v1`

Auth: Bearer token (pre-generated via DPD portal or `POST /auth/tokens`)

**Status: implemented** — `internal/adapter/dpd.go`

---

## Carrier key design

DPD Group operates multiple regional entities with the same API schema but
separate endpoints and credentials. The adapter is registered once per
configured country under a scoped key (`dpd_lt`, `dpd_at`, `dpd_be`, etc.)
rather than a single `dpd` key.

Registration is driven by environment variables — no code change is needed
to add a new DPD country:

```
DPD_LT_API_TOKEN=<token>   DPD_LT_BASE_URL=https://esiunta.dpd.lt/api/v1
DPD_AT_API_TOKEN=<token>   DPD_AT_BASE_URL=https://...dpd.at/api/v1
```

`registerDPD` in `InitAdapters` scans `DPD_{COUNTRY}_API_TOKEN` env vars at
startup and registers one `DPDAdapter` instance per country. Missing
`BASE_URL` or an empty token falls back to `MockDPDAdapter`.

**Exception:** DPD UK, SEUR (Spain), and BRT (Italy) are separate legal
entities with different API schemas. These require distinct adapter
implementations and are out of scope here.

---

## Summary

The Baltic DPD API covers the core booking and pickup loop. The implemented
adapter satisfies booking, cancellation, label retrieval, tracking (status +
full event history via `detail=3`), and pickup scheduling.

Gaps: shipment update (no API endpoint), pickup update (no API endpoint),
pickup cancellation (no API endpoint in Baltic API v1), and manifest close
(not required — pickup creation is the handover instruction). All gaps return
HTTP 501 via `ErrNotSupported`.

---

## Feature fit/gap

### Booking

| Feature | Support | DPD endpoint | Implemented |
|---|---|---|---|
| Book shipment | ✅ | `POST /shipments` | ✅ Label requested inline via `labelOptions` |
| Cancel shipment | ✅ | `DELETE /shipments?ids={id}` | ✅ Uses shipment UUID from `BookingResponse.ShipmentID` |
| Update shipment | ❌ | — | ✅ Returns `ErrNotSupported` → HTTP 501 |
| Multi-parcel shipment | ✅ | `POST /shipments` | ✅ One parcel block per colli |
| Idempotency | ❌ | — | ✅ Gateway-side deduplication only (no native key) |

### Labels

| Feature | Support | DPD endpoint | Implemented |
|---|---|---|---|
| Print label | ✅ | `POST /shipments/labels` | ✅ By parcel number |
| Format — PDF | ✅ | `POST /shipments/labels` | ✅ A6 default |
| Format — PNG | ✅ | `POST /shipments/labels` | ✅ |
| Format — ZPL | ❌ | — | ✅ Returns `unsupportedFormat` error |
| Inline label at booking | ✅ | `POST /shipments` (`labelOptions`) | ✅ Label fetched in same request |

Return labels and relabelling are not documented in the Baltic API v1 and are
not implemented.

### Tracking

| Feature | Support | DPD endpoint | Implemented |
|---|---|---|---|
| Current status | ✅ | `GET /status/tracking` | ✅ Normalised via `normalizeDPDStatus` |
| Event history | ✅ | `GET /status/tracking?show_all=1` | ✅ Full history returned in `TrackingResponse.Events` |
| Advanced status codes | ✅ | `detail=3` | ✅ `statusCode` + `serviceCode` + `prevStatusCode` used for accurate normalization |
| Push / webhook | ⚠️ | `GET /status/events/subscribetoparcel` | ❌ Not implemented — requires separate subscription per parcel |
| Parcel-level tracking | ✅ | `GET /status/tracking` | ✅ Parcel number (14 chars) returned at booking is the tracking key |

**Status normalization.** DPD status codes require three fields together
(`statusCode`, `serviceCode`, `prevStatusCode`) to resolve correctly — for
example, status `13` means delivered to consignee or returned to sender
depending on `serviceCode`. The `normalizeDPDStatus` function in `dpd.go`
handles this. DPD is intentionally absent from the single-key
`normalizedStatuses` table in `status.go`.

**Webhook subscription.** `GET /status/events/subscribetoparcel` registers a
per-parcel callback. This requires storing a subscription per shipment and is
not implemented in the current adapter. Callers requiring push events must
poll `TrackShipment` or implement subscription management separately.

### Pickup scheduling

| Feature | Support | DPD endpoint | Implemented |
|---|---|---|---|
| Book pickup | ✅ | `POST /pickups` | ✅ Accepts `shipmentUuids` or parcel count/weight fallback |
| Cancel pickup | ❌ | — | ✅ Returns `ErrNotSupported` → HTTP 501 |
| Update pickup | ❌ | — | ✅ Returns `ErrNotSupported` → HTTP 501 |

`pickupTimeFrom` and `pickupTimeTo` are mandatory for DPD (minutes must be
`00` or `30`). Both `PickupRequest.Pickup.ReadyTime` and `CloseTime` are
validated before the API call.

`TrackingNumbers` on `PickupRequest` maps to `shipmentUuids` in the DPD
payload. Pass `BookingResponse.ShipmentID` values here — DPD requires the
internal UUID, not the parcel tracking number.

DPD does not return a pickup confirmation number. The adapter synthesises one
as `{date}T{readyTime}` for the `ConfirmationNumber` field.

### Manifest / end-of-day

| Feature | Support | DPD endpoint | Implemented |
|---|---|---|---|
| Close manifest | ❌ | — | ✅ Returns `ErrNotSupported` → HTTP 501 |
| Manifest document | ❌ | — | ✅ Returns `ErrNotSupported` → HTTP 501 |

Pickup creation via `BookPickup` serves as the handover instruction. No
explicit manifest close is required or available.

### Add-ons

| Feature | Support | DPD endpoint | Implemented |
|---|---|---|---|
| COD (cash on delivery) | ✅ | `POST /shipments` (`additionalServices`) | ⚠️ `serviceAlias` must be confirmed with DPD support; COD amount not forwarded |
| PUDO / parcel locker delivery | ✅ | `POST /shipments` (`receiverAddress.pudoId`) | ✅ `Receiver.ServicePointID` maps to `pudoId` |
| Predict (SMS/email ETA) | ✅ | DPD Private service includes SMS by default | ❌ Not implemented — requires service-level selection |
| Hazmat | ✅ | `POST /shipments` | ❌ Not implemented — `additionalServices` block required |
| Customs / cross-border | ✅ | `POST /shipments` | ❌ Not implemented |

---

## Endpoint mapping

| carrier-gateway | DPD API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /shipments` | ✅ Implemented |
| `DELETE /api/bookings/{id}` | `DELETE /shipments?ids={id}` | ✅ Implemented |
| `PATCH /api/bookings/{id}` | — | ✅ Returns 501 |
| `GET /api/trackings/{id}` | `GET /status/tracking?pknr={id}&detail=3&show_all=1` | ✅ Implemented |
| `GET /api/labels/{id}` | `POST /shipments/labels` | ✅ Implemented |
| `POST /api/pickups` | `POST /pickups` | ✅ Implemented |
| `PUT /api/pickups/{id}` | — | ✅ Returns 501 |
| `DELETE /api/pickups/{id}` | — | ✅ Returns 501 |
| `POST /api/manifests` | — | ✅ Returns 501 |

---

## Environment variables

### DPD continental Europe (dynamic per country)

| Variable | Description |
|---|---|
| `DPD_{COUNTRY}_API_TOKEN` | Bearer token for the given country (e.g. `DPD_LT_API_TOKEN`) |
| `DPD_{COUNTRY}_BASE_URL` | API base URL for the given country (e.g. `DPD_LT_BASE_URL`) |

Both vars must be set together. The adapter key becomes `dpd_{country}` (e.g. `dpd_lt`).

### DPD UK (static)

| Variable | Description |
|---|---|
| `DPD_UK_USERNAME` | DPD UK username |
| `DPD_UK_PASSWORD` | DPD UK password |
| `DPD_UK_USER_ID` | DPD UK user ID |
| `DPD_UK_NETWORK_CODE` | DPD UK network code |

---

## Implementation notes

**Carrier keys.** Use `"dpd_lt"`, `"dpd_at"`, `"dpd_be"` etc. in the
`carrier` field of booking requests — not `"dpd"`. The key matches the
country suffix of the env var pair that configured the adapter.

**Shipment UUID vs parcel number.** `BookingResponse.ShipmentID` is the DPD
internal UUID (used for cancellation and pickup linkage). `TrackingNumber` is
the 14-digit parcel number (used for tracking and label retrieval). Both are
returned at booking time and should be stored by the caller.

**Inline label.** `BookShipment` requests the label within the same API call
via `labelOptions`. The label is available in `BookingResponse` immediately;
a separate `FetchLabel` call is only needed for reprints.

**Token management.** DPD Bearer tokens are long-lived and do not rotate
automatically. Each user can hold up to 100 active tokens. Token rotation is
the operator's responsibility. Set tokens via `DPD_{COUNTRY}_API_TOKEN`.

**Sandbox.** Each country provides a sandbox environment (e.g.
`https://sandbox-esiunta.dpd.lt/api/v1`). Point `DPD_{COUNTRY}_BASE_URL` at
the sandbox URL for integration testing.
