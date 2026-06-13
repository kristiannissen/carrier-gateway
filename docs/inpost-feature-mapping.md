# InPost — Feature Mapping

API: **ShipX API**
Auth: API key (Bearer token)
Coverage: Poland (dominant), UK, France, Italy (expanding) — parcel locker network.
Implementation status: **Not fully implemented yet** (Demo / mock only)

---

## Summary

InPost is a parcel locker network. The adapter exists and satisfies the
`CarrierAdapter` interface, but is currently in demo mode — it returns mock
data and is not connected to the live ShipX API. All five interface methods
are present; cancellation and update are not supported by the ShipX API.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ⚠️ | Adapter wired but **demo mode only** — returns mock data, no live API call |
| Cancel shipment | ❌ | Not supported by ShipX API |
| Update shipment | ❌ | Not supported by ShipX API |
| Idempotency key | ❌ | Client-side only |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Print label | ⚠️ | Demo only — `FetchLabel` calls ShipX label endpoint but is not live-tested |
| Return label | ❌ | Not implemented |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ⚠️ | Demo only — `TrackShipment` calls ShipX tracking endpoint but is not live-tested |
| Event history | ⚠️ | Demo only |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ❌ | Not applicable — InPost is a drop-off locker network, not a collected carrier |
| Update/Cancel pickup | ❌ | Not applicable |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❌ | Not applicable for locker network (`501`) |

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| SMS notification | ❌ | Not wired |
| Email notification | ❌ | Not wired |
| Flex / Signature / COD / Insurance | ❌ | Not wired |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Customs / cross-border | ❌ | Not wired |
| Target locker | ✅ | `receiver.servicePointId` → `service.targetLocker` — the locker code the parcel is routed to |
| Multi-colli | ✅ | Multiple parcels per booking |

---

## Endpoint mapping

| carrier-gateway | ShipX API | Status |
|---|---|---|
| `POST /api/bookings` | ShipX create shipment | ⚠️ Demo |
| `DELETE /api/bookings/{id}` | — | ❌ → 501 |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 |
| `GET /api/trackings/{id}` | ShipX tracking | ⚠️ Demo |
| `GET /api/labels/{id}` | ShipX label | ⚠️ Demo |
| `POST /api/pickups` | — | ❌ not applicable |
| `POST /api/manifests` | — | ❌ not applicable |

---

## Implementation notes

**Demo mode.** InPost is a placeholder integration. The adapter compiles and
the interface is satisfied, but `capabilities["inpost"].Demo = true` causes the
booking response to include a `BetaWarning`. No live API calls are made until
the demo flag is removed.

**Locker network model.** InPost does not collect parcels from a sender address.
The sender drops the parcel into an InPost locker using the booking code. The
receiver collects from their chosen target locker. Pickup scheduling and manifest
are not relevant.

**To go live.** Set `INPOST_API_KEY`, remove the `Demo: true` capability flag,
and run integration tests against the ShipX staging environment.
