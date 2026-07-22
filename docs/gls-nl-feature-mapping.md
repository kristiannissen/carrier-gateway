# GLS NL (Regional API) — Feature Mapping

API: **GLS Shipping API v0.8** (and compatible national portals)
Auth: Username/password in every request body (no token)
Coverage: GLS national subsidiaries — NL, BE, and others using api-portal.gls.nl-style portals
Implementation status: **Production** — BookShipment, CancelShipment, BookPickup,
CancelPickup, and CloseManifest implemented. TrackShipment, FetchLabel (reprint),
and UpdateShipment are confirmed carrier limitations, not implementation gaps —
this regional portal API doesn't expose tracking or a reprint endpoint at all
(the label is returned inline at booking instead), and has no update endpoint.
No genuine gap remains.
Carrier keys: `gls_nl`, `gls_be`, … (one per country, registered via env vars)

---

## Overview

GLS exists as both a unified group (OAuth2 API at api.gls-group.net, carrier key `gls`)
and as separate national subsidiaries that run their own portals and APIs. This adapter
covers the latter — the national portal style documented in `APIdocs/GLS_Shipping_API_v0.8.pdf`.

The two GLS adapters are completely independent:

| Adapter | Key | Auth | Endpoint |
|---|---|---|---|
| GLS Group | `gls` | OAuth2 client credentials | api.gls-group.net/shipit-farm/v1 |
| GLS Regional | `gls_nl`, `gls_be`, … | Username/password per request | api-portal.gls.nl (and equivalents) |

Use the regional adapter when you have a MyGLS account on the national portal
and no access to GLS Group API credentials.

---

## Configuration

Add env vars for each country you want to activate:

```
GLS_NL_USERNAME=<mygls_username>
GLS_NL_PASSWORD=<mygls_password>
GLS_NL_BASE_URL=https://api.mygls.nl

GLS_BE_USERNAME=<mygls_username>
GLS_BE_PASSWORD=<mygls_password>
GLS_BE_BASE_URL=https://api.mygls.be
```

No code change is needed to add a new country. The factory scans for
`GLS_{CC}_USERNAME` where `CC` is exactly a 2-letter ISO country code.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment (parcel) | ✅ | `POST /CreateLabel` — ShipType "P", ≤32 kg per collo |
| Book shipment (freight) | ✅ | `POST /CreateLabel` — ShipType "F", auto-detected when any collo >32 kg |
| Cancel shipment | ✅ | `POST /DeleteLabel` |
| Update shipment | ❌ | Not available in this API |
| Idempotency key | ❌ | Client-side only |
| Return shipment | ✅ | `POST /CreateShopReturn` — triggered by `DeliveryType="return"` |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Label at booking | ✅ | Returned inline as base64 in `colli[].labelUrl` (data URI) |
| Label formats | ✅ | PDF (default). ZPL and A6 variants available via `LabelType` field |
| Reprint label | ❌ | No reprint endpoint in this API — save label from booking response |
| Return label bundled | ✅ | `ShopReturnService: true` returns an extra label in `LabelShopReturn` |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Tracking | ❌ | Not available in this API — use carrier portal or GLS Group adapter |
| Tracking link | ✅ | Returned in CreateLabel response (`TrackingLinkType: "U"`) |

### Pickup

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ✅ | `POST /CreatePickup` — three address blocks required |
| Cancel pickup | ✅ | `POST /DeletePickup` |
| Update pickup | ❌ | Not available — cancel and rebook |
| Pickup availability | ❌ | Not available in this API |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ✅ | `POST /ConfirmLabel` called per unit — one call per tracking number |
| Manifest document | ❌ | Not returned by ConfirmLabel |

Note: Unlike the GLS Group API (`endofday`), ConfirmLabel is per-unit. CloseManifest
iterates over all provided tracking numbers and confirms each individually.

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| Email notification | ✅ | `NotificationEmail.SendMail: true` |
| SMS notification | ❌ | Only available on ExpressService — not wired |
| Saturday delivery | ❌ | `SaturdayService: true` — not wired (no gateway add-on type) |
| Express delivery | ❌ | `ExpressService: T9/T12/T17/S9/S12/S17` — not wired (no gateway add-on type) |
| Parcel shop delivery | ✅ | `receiver.servicePointId` → `ShopDeliveryParcelShopId` |
| Bundled return label | ⚠️ | `ShopReturnService: true` — not wired as add-on; use `DeliveryType="return"` for standalone return |

### Products

The PDF documents these product/service combinations:

| Type | Product | Destination | Services available |
|---|---|---|---|
| Parcel | Business Parcel | Domestic | SRS, SHD, SCB, P&S, P&R |
| Parcel | Euro Business Parcel | Euro | P&S, P&R |
| Parcel | Global Business Parcel | Global | — |
| Parcel | ExpressService | Domestic | SRS |
| Freight | Business Freight | Domestic | P&S, P&R |
| Freight | Euro Business Freight | Euro | P&S, P&R |
| Freight | Freight Solutions | Global | — |
| Freight | Business Express Freight | Domestic | — |

---

## Endpoint mapping

| carrier-gateway | GLS NL API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /CreateLabel` | ✅ |
| `POST /api/bookings` (return) | `POST /CreateShopReturn` | ✅ |
| `DELETE /api/bookings/{id}` | `POST /DeleteLabel` | ✅ |
| `PATCH /api/bookings/{id}` | — | ❌ not available |
| `GET /api/trackings/{id}` | — | ❌ not available in this API |
| `GET /api/labels/{id}` | — | ❌ no reprint endpoint |
| `POST /api/pickups` | `POST /CreatePickup` | ✅ |
| `DELETE /api/pickups/{id}` | `POST /DeletePickup` | ✅ |
| `PUT /api/pickups/{id}` | — | ❌ not available |
| `POST /api/manifests` | `POST /ConfirmLabel` (per unit) | ✅ |

---

## Implementation notes

**Labels returned at booking.** Unlike most carriers where you call FetchLabel
separately, the GLS NL API returns the label inline in the CreateLabel response.
The label is stored in `colli[].labelUrl` as a `data:application/pdf;base64,...`
data URI. There is no reprint endpoint — callers must save the label at booking time.

**Per-unit manifest.** `ConfirmLabel` operates on a single unit number. The gateway's
`CloseManifest` iterates over all provided tracking numbers and confirms each in turn.
A partial failure returns an error listing the unconfirmed unit numbers.

**Freight auto-detection.** If any collo exceeds 32 kg, the adapter automatically
switches ShipType to "F" (freight) and assigns a UnitType based on weight and
length (CO, LG, MP, PL, BP, XL).

**Express and Saturday add-ons.** These are not yet wired because the gateway has
no corresponding `AddOnType` constants. Express delivery (`T9`/`T12`/`T17` for
weekdays; `S9`/`S12`/`S17` for Saturday) and Saturday service can be added once
the gateway interface is extended.
