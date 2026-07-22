# Hermes Germany — Feature Mapping

API: **Hermes HSI API (HEX — Hermes Exchange)**
Auth: OAuth2 client credentials (clientID + clientSecret → Bearer token)
Coverage: Germany — domestic home delivery network.
Implementation status: **Partial** — Cancel and update are confirmed carrier
limitations (explicitly not supported by the HSI API), so all primary methods
are complete. Pickup scheduling and manifest status are unconfirmed (secondary),
which is what keeps this at Partial rather than Production.

---

## Summary

Hermes Germany (HSI) covers booking, tracking, labels, and return labels. The
integration is based on directly obtained API specs — there is no public
documentation. Cancellation and post-booking update are explicitly not supported
by the HSI API. Pickup scheduling and manifest are not confirmed.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | HEX order endpoint — label returned inline in booking response |
| Cancel shipment | ❌ | Not supported by HSI API |
| Update shipment | ❌ | Not supported by HSI API |
| Idempotency key | ❌ | Client-side only |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Print label | ✅ | PDF (default) or ZPL (203 dpi). Inline in booking response. |
| Reprint label | ✅ | `FetchLabel` — requires shipmentOrderID. See note below. |
| Return label | ✅ | `bookReturnShipment` → `/returnorders/labels` endpoint |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ✅ | HSI event API — normalized status |
| Event history | ✅ | Hermes event codes mapped to normalized statuses. Full event code table in `agents/hermes_Germany_Eventcodes.csv`. |
| Estimated delivery | ✅ | Where returned |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ❓ | Not confirmed — Hermes Germany typically operates via standing collection agreement |
| Update pickup | ❓ | Unknown |
| Cancel pickup | ❓ | Unknown |

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest | ❓ | Not confirmed via HSI API |
| Manifest document | ❓ | Unknown |

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| SMS notification | ✅ | Mapped via HSI notifications block. Requires `receiver.phone`. |
| Email notification | ✅ | Mapped via HSI notifications block. Requires `receiver.email`. |
| Signature required | ✅ | Mapped to HSI additional service |
| Cash on delivery | ✅ | Mapped to HSI COD service — amount and currency required |
| Flex delivery | ❌ | Not available in HSI API |
| Insurance | ❌ | Not wired |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Customs / cross-border | ❌ | Germany domestic only — no customs needed |
| Service point delivery | ✅ | HSI routing API supports service point selection (`hermes_routing_openapi.yaml`). `receiver.servicePointId` mapped. |
| Multi-colli | ✅ | Multiple parcels per order |
| Business delivery | ✅ | HSI supports B2B routing |

---

## Endpoint mapping

| carrier-gateway | Hermes HSI API | Status |
|---|---|---|
| `POST /api/bookings` | HEX order endpoint | ✅ |
| `DELETE /api/bookings/{id}` | — | ❌ → 501 |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 |
| `GET /api/trackings/{id}` | HSI event API | ✅ |
| `GET /api/labels/{id}` | HEX label reprint | ✅ |
| `POST /api/pickups` | ❓ | ❓ |
| `PUT /api/pickups/{id}` | ❓ | ❓ |
| `DELETE /api/pickups/{id}` | ❓ | ❓ |
| `POST /api/manifests` | ❓ | ❓ |

---

## Environment variables

| Variable | Description |
|---|---|
| `HERMES_CLIENT_ID` | Hermes HSI OAuth2 client ID |
| `HERMES_CLIENT_SECRET` | Hermes HSI OAuth2 client secret |

---

## Implementation notes

**No public documentation.** The Hermes HSI API is not publicly documented.
The integration is based on directly obtained API specifications. Changes to
the API may not be communicated via a public changelog.

**FetchLabel requires shipmentOrderID.** The HSI label reprint endpoint uses
an internal `shipmentOrderID` rather than the parcel tracking number. The
adapter stores this alongside the tracking number at booking time. There is a
TODO in the code to confirm the storage strategy (`internal/adapter/hermes.go`
line ~618).

**Hermes UK is unrelated.** Evri (formerly Hermes UK) is a completely separate
carrier and company with no shared API or infrastructure with Hermes Germany HSI.
