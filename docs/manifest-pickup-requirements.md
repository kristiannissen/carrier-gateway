# Manifest & Pickup Scheduling — Requirements

## Background

Shipments are booked individually throughout the day as orders are packed
(`POST /api/bookings`). At end of day the warehouse hands over the parcels to
the carrier. This document covers the two API operations that support that
handover.

---

## The outbound handover process

```
Book shipments  →  Schedule pickup  →  Truck arrives  →  Handover document
(existing)         (new)               (carrier-side)    (new)
```

**Schedule pickup** — the warehouse tells the carrier when and where to collect.
Done once per carrier per day, typically at the start of or during the day.
Some carriers make this optional if a standing collection agreement exists.

**Handover document (manifest)** — a record of what was loaded onto the truck.
For most carriers this is generated or retrieved after collection. For a small
number of carriers (GLS) it must be submitted before the driver arrives and
effectively acts as the collection order.

These two operations are independent. Pickup scheduling causes the truck to come.
The manifest documents what went on it.

---

## Scope

**In scope:** PostNord, Bring, GLS, DAO, DHL eCommerce Europe, DHL Express,
Hermes (HSI), FedEx.

**Out of scope:** Instabee.

---

## New endpoints

### `POST /api/pickups`

Schedules a carrier collection at the warehouse.

#### Request

```json
{
  "carrier": "dhl_express",
  "pickup": {
    "date": "2026-06-13",
    "readyTime": "14:00",
    "closeTime": "18:00",
    "location": "reception",
    "specialInstructions": "Ring doorbell"
  },
  "contact": {
    "name": "Warehouse Manager",
    "phone": "+4512345678",
    "email": "warehouse@unisport.dk"
  },
  "address": {
    "street": "Industrivej",
    "houseNumber": "10",
    "city": "Copenhagen",
    "postalCode": "2300",
    "country": "DK"
  },
  "estimatedParcels": 80,
  "estimatedWeight": 160.0
}
```

| Field | Type | Description | Required |
|---|---|---|---|
| `carrier` | string | Carrier key | Yes |
| `pickup.date` | string | ISO 8601 date for collection | Yes |
| `pickup.readyTime` | string | Requested time parcels are ready (`HH:MM`). Passed to carriers that accept a time window; ignored by carriers that manage their own schedule. | No |
| `pickup.closeTime` | string | Requested latest collection time (`HH:MM`). Passed to carriers that accept a time window; ignored by carriers that manage their own schedule. | No |
| `pickup.location` | string | Collection point description (e.g. `reception`) | No |
| `pickup.specialInstructions` | string | Free-text instructions for driver | No |
| `contact` | object | Contact person at pickup location | Yes |
| `address` | object | Pickup address — defaults to configured sender address if omitted | No |
| `estimatedParcels` | int | Estimated number of parcels | No |
| `estimatedWeight` | float | Estimated total weight in kg | No |

#### Response

```json
{
  "carrier": "dhl_express",
  "confirmationNumber": "PRG999126012345",
  "date": "2026-06-13",
  "readyTime": "14:00",
  "closeTime": "18:00",
  "status": "booked"
}
```

| Field | Type | Description |
|---|---|---|
| `status` | string | `booked` |
| `confirmationNumber` | string | Carrier-issued pickup confirmation reference |
| `readyTime` | string | Confirmed earliest pickup time from the carrier. May differ from the requested time, or be absent if the carrier does not return a window. |
| `closeTime` | string | Confirmed latest pickup time from the carrier. May differ from the requested time, or be absent if the carrier does not return a window. |

---

### `PUT /api/pickups/{confirmationNumber}`

Updates a previously booked pickup. The request body follows the same shape as
`POST /api/pickups`. Only fields that differ from the original booking need to
be included; omitted fields are left unchanged.

```
PUT /api/pickups/PRG999126012345?carrier=dhl_express
```

```json
{
  "pickup": {
    "date": "2026-06-13",
    "readyTime": "15:00",
    "closeTime": "18:00"
  }
}
```

#### Response — success

```json
{
  "carrier": "dhl_express",
  "confirmationNumber": "PRG999126012345",
  "date": "2026-06-13",
  "readyTime": "15:00",
  "closeTime": "18:00",
  "status": "updated"
}
```

#### Response — carrier does not support update

HTTP `501` with:

```json
{
  "error": "carrier bring does not support pickup update"
}
```

---

### `DELETE /api/pickups/{confirmationNumber}`

Cancels a previously booked pickup.

```
DELETE /api/pickups/PRG999126012345?carrier=dhl_express
```

#### Response — success

```json
{"confirmationNumber": "PRG999126012345", "carrier": "dhl_express", "status": "cancelled"}
```

#### Response — carrier does not support cancel

HTTP `501` with:

```json
{
  "error": "carrier bring does not support pickup cancellation"
}
```

---

### `POST /api/manifests`

Retrieves or generates the handover document for a carrier and shipping day.
For carriers that require a close call before collection (GLS), this also
submits the end-of-day instruction to the carrier.

#### Request

```json
{
  "carrier": "gls",
  "date": "2026-06-12",
  "trackingNumbers": ["1Z999AA10123456784", "1Z999AA10123456785"]
}
```

| Field | Type | Description | Required |
|---|---|---|---|
| `carrier` | string | Carrier key | Yes |
| `date` | string | ISO 8601 date — the shipping day | Yes |
| `trackingNumbers` | array | Tracking numbers to include. Required for carriers that need an explicit list; ignored by carriers that close server-side. | No |

#### Response

```json
{
  "carrier": "gls",
  "date": "2026-06-12",
  "status": "closed",
  "parcelsConfirmed": 42,
  "manifestDocument": "JVBERi0xLj...",
  "manifestDocumentFormat": "PDF",
  "warnings": []
}
```

| Field | Type | Description |
|---|---|---|
| `status` | string | `closed` |
| `parcelsConfirmed` | int | Number of parcels confirmed, where returned by carrier |
| `manifestDocument` | string | Base64-encoded manifest document, where returned by carrier |
| `manifestDocumentFormat` | string | `PDF` |
| `warnings` | array | Any non-fatal issues |

---

## Carrier support

| Carrier | Book pickup | Update pickup | Cancel pickup | Manifest document |
|---|---|---|---|---|
| **GLS** | ✅ `POST /rs/sporadiccollection` | ❓ Needs investigation | ❓ Needs investigation | ✅ `POST /rs/shipments/endofday` — must come before driver; acts as collection order. Returns PDF. |
| **DHL Express** | ✅ `POST /pickups` | ❓ Needs investigation | ✅ `DELETE /pickups/{id}` | ✅ Retrievable post-collection via `GET /shipments/{id}/get-image?typeCode=MANIFEST`. |
| **DPD** | ✅ `POST /pickup` | ❓ Needs investigation | ❓ Needs investigation | ✅ Same call as booking. |
| **PostNord** | ✅ `POST /v3/pickups/ids` (SE/DK/FI) | ❌ Not in API | ❌ Not in API | ❌ Not in API — handled by PostNord EDI at scan time. |
| **Bring** | ✅ `POST /api/create` | ❌ Not in API | ❌ Not in API | ❌ Not in API. |
| **FedEx** | ❌ Not in docs | ❌ Not in docs | ❌ Not in docs | ❌ Not in docs. |
| **DHL eCommerce** | ❓ Unknown | ❓ Unknown | ❓ Unknown | ❓ Unknown. |
| **DAO** | ❓ Unknown | ❓ Unknown | ❓ Unknown | ❓ Unknown. |
| **Hermes (HSI)** | ❓ Unknown | ❓ Unknown | ❓ Unknown | ❓ Unknown. |

---

## PostNord pickup — implementation notes

PostNord's `/v3/pickups/ids` takes an array of item IDs (the barcode identifiers
returned in the booking response) and a time window. The adapter must:

1. Accept item IDs via the `trackingNumbers` field on the pickup request, or
   retrieve them from the gateway's own booking records for the given date.
2. Map `pickup.readyTime` → `earliestPickupDate` and `pickup.closeTime` →
   `latestPickupDate` (both as datetime, combined with `pickup.date`).
3. Return the `bookingId` from the response as `confirmationNumber`.

Geographic limitation: domestic SE, DK, FI only. Cross-border PostNord shipments
cannot use this endpoint; return `not_supported` in that case.

The `/v4/sac/pickup/stopdate` endpoint can be used to validate `pickup.date`
against PostNord's actual cutoff windows before submitting.

---

## Code changes

### New files

| File | Purpose |
|---|---|
| `internal/handler/manifests.go` | `POST /api/manifests` handler |
| `internal/handler/pickups.go` | `POST /api/pickups`, `PUT /api/pickups/{id}`, `DELETE /api/pickups/{id}` handlers |
| `internal/adapter/manifest.go` | `ManifestAdapter` interface + `ErrNotSupported` definition |

### Existing files touched

| File | Change |
|---|---|
| `internal/adapter/adapter.go` | Add `ManifestAdapter` interface alongside `CarrierAdapter` |
| `internal/router/router.go` | Wire new routes |
| `internal/adapter/gls.go` | Implement `ManifestAdapter` |
| `internal/adapter/dhl_express.go` | Implement `ManifestAdapter` |
| `internal/adapter/dpd.go` | Implement `ManifestAdapter` |
| `internal/adapter/postnord.go` | Implement `ManifestAdapter` (pickup booking only; update and cancel return `ErrNotSupported`) |
| `internal/adapter/bring.go` | Implement `ManifestAdapter` (pickup booking only; update and cancel return `ErrNotSupported`) |
| `internal/adapter/mock_*.go` | Mock implementations for new interface |
| `internal/validation/` | Pickup date/time validation — reject past dates, enforce carrier cutoff windows |

### Interface design

`ManifestAdapter` is defined separately from `CarrierAdapter`. All four methods
are required on every implementation. Carriers that do not support a particular
operation return `ErrNotSupported`; the handler translates this to HTTP `422`
with a descriptive error message naming the carrier and the unsupported operation.

```
CarrierAdapter      — booking, tracking, labels, cancellation (existing)
ManifestAdapter     — CloseManifest, BookPickup, UpdatePickup, CancelPickup (new)
```

`ErrNotSupported` always maps to HTTP `501` with a descriptive error body,
regardless of which method returns it. This applies equally to `CloseManifest`,
`BookPickup`, `UpdatePickup`, and `CancelPickup`.

```json
{"error": "carrier postnord does not support manifest close"}
```

`501` is the correct status: the server understood the request but the
functionality is not implemented for the selected carrier. Using `501` ensures
engineers hit an error path and cannot accidentally treat an unsupported
operation as a success.

### No changes to

Booking, tracking, labels, notifications, customs, validation (except pickup
date rules) — all unaffected.

---

## Validation rules

- `pickup.date` must not be in the past.
- If both `pickup.readyTime` and `pickup.closeTime` are provided, `readyTime` must be before `closeTime`.
- The confirmed window in the response reflects what the carrier actually scheduled, which may differ from the requested times.
- `date` on manifest request must not be in the future.
- `carrier` must be a known key; unknown carriers return `400`.
