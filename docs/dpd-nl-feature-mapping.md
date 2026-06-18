# DPD NL — Fit/Gap Analysis

API: **DPD NL ShipmentService v3.5 / ParcelLifecycleService v2.0 / LoginService v2.1**
Protocol: SOAP/XML (not REST)
Base URL (prod): `https://wsshipper.dpd.nl/soap/services/`

Auth: 24-hour auth token issued by `LoginService.getAuth` using delisID + password.

**Status: implemented (beta)** — `internal/adapter/dpd_nl.go`

---

## Summary

DPD NL uses a SOAP API that is distinct from the DPD Baltic REST API used by
`dpd_lt`, `dpd_at` etc. It is registered under the separate key `"dpd_nl"` and
requires its own credentials.

The adapter covers booking (B2B, B2C, PSD/parcel-shop, returns, multi-colli,
international customs), label retrieval (from in-process cache), and parcel
tracking. Cancellation and post-booking update are not supported by the API and
return `ErrNotSupported`.

The adapter is marked **beta** — test against the DPD NL sandbox before enabling
in production. All `BookingResponse` objects carry a `BetaWarning` field for
visibility.

---

## API constraints

**Sequential booking.** DPD NL prohibits concurrent `storeOrders` calls per
account. The adapter holds a mutex for the full duration of each booking call.

**Token budget.** LoginService allows a maximum of 10 logins per day per account.
The adapter caches the 24-hour token and refreshes it only on expiry (with a
60-second margin) or on LOGIN\_5/LOGIN\_6 SOAP faults. Both `BookShipment` and
`TrackShipment` retry once after a token-fault refresh.

**No label endpoint.** DPD NL returns the label (base64 PDF or PNG) inline in the
`storeOrders` response. There is no standalone fetch-label endpoint. The adapter
caches labels in a `sync.Map` keyed by tracking number. `FetchLabel` serves from
this cache and returns an error on a cache miss — labels are not recoverable across
process restarts.

---

## Feature fit/gap

### Booking

| Feature | Support | DPD NL endpoint | Implemented |
|---|---|---|---|
| Book shipment (B2B) | ✅ | `ShipmentService.storeOrders` | ✅ Default product |
| Book shipment (B2C / home) | ✅ | `storeOrders` | ✅ `DeliveryType "home"` → product B2C |
| Book return shipment | ✅ | `storeOrders` | ✅ `DeliveryType "return"` → B2C + `<returns>true</returns>` |
| Parcel shop delivery (PSD) | ✅ | `storeOrders` | ✅ `DeliveryType "servicepoint"` → product PSD + `<parcelShopDelivery>` |
| Multi-colli (MPS) | ✅ | `storeOrders` | ✅ One `<parcels>` element per colli |
| International / customs | ✅ | `storeOrders` | ✅ `<international>` block nested inside each `<parcels>` element |
| Cancel shipment | ❌ | — | ✅ Returns `ErrNotSupported` |
| Update shipment | ❌ | — | ✅ Returns `ErrNotSupported` |
| Idempotency | ❌ | — | ✅ Gateway-side deduplication only |

### Labels

| Feature | Support | DPD NL endpoint | Implemented |
|---|---|---|---|
| Label at booking (PDF) | ✅ | `storeOrders` response `<parcellabelsPDF>` | ✅ Cached in-process |
| Label at booking (PNG/QR) | ✅ | `storeOrders` response `<parcellabelsPNG_qr>` | ✅ Requested via `<dropOffType>QR_CODE</dropOffType>` |
| Standalone label fetch | ❌ | — | ✅ Served from in-process cache; error on cache miss |
| ZPL / EPL format | ❌ | — | ✅ Returns `unsupportedFormat` error |

**Label cache caveat.** If the process restarts after booking but before the
label is consumed, `FetchLabel` returns:
> *"label for X not in cache — DPD NL does not expose a fetch-label endpoint;
> the label must be captured at booking time"*

Callers should extract and store the label from `BookingResponse` immediately.

### Tracking

| Feature | Support | DPD NL endpoint | Implemented |
|---|---|---|---|
| Current status | ✅ | `ParcelLifecycleService.getTrackingData` | ✅ Normalised via `normalizeDPDNLStatus` |
| Event history | ✅ | `getTrackingData` | ✅ All `<parcelEvent>` elements returned in `TrackingResponse.Events` |
| Rate limit | ⚠️ | 60 req/min, 12,000/day | ❌ No client-side rate limiting — caller responsibility |
| Push / webhook | ❌ | — | ❌ Not available in this API version |

**Status normalization.** DPD NL status strings are mapped in `status.go` under
the `"dpd_nl"` key:

| DPD NL status | Normalised |
|---|---|
| `CREATED`, `REGISTERED` | `booked` |
| `COLLECTED` | `picked_up` |
| `TRANSIT`, `IN_TRANSIT`, `DEPOT` | `in_transit` |
| `OUT_FOR_DELIVERY` | `out_for_delivery` |
| `DELIVERED` | `delivered` |
| `NOT_DELIVERED`, `EXCEPTION`, `MISSING` | `failed` |
| `RETURNED`, `RETURN_DELIVERED` | `returned` |

**Tracking number format.** DPD NL tracking numbers are exactly 14 numeric
digits (`parcelLabelNumber`). This is different from the internal MPS ID
(`mpsId`) returned alongside it. Always track by `parcelLabelNumber`.

### Pickup scheduling

DPD NL `ShipmentService` v3.5 has no pickup booking endpoint. Pickups are
arranged operationally outside the API. `BookPickup`, `UpdatePickup`, and
`CancelPickup` are not implemented and will return `ErrNotSupported`.

### Manifest / end-of-day

No manifest API. Not required — pickup arrangements are made offline.

### Add-ons

| Add-on | Support | Notes |
|---|---|---|
| Email notification (Predict) | ✅ | Emitted as `<predict>` when `receiver.email` is set |
| SMS notification | ❌ | Not supported by ShipmentService v3.5 |
| COD (cash on delivery) | ❌ | Not implemented |
| Signature required | ❌ | Not implemented |
| Saturday delivery | ❌ | Requires separate `saturdayDelivery` product flag — not wired |

---

## Product codes

| `DeliveryType` | DPD NL product | Notes |
|---|---|---|
| *(default / empty)* | `B2B` | Business-to-business |
| `"home"` | `B2C` | Home delivery |
| `"return"` | `B2C` | Return shipment — adds `<returns>true</returns>` per parcel |
| `"servicepoint"` | `PSD` | Parcel shop delivery — requires `receiver.servicePointId` |

---

## International / customs

The `<international>` block is nested inside each `<parcels>` element, as
required by ShipmentService v3.5. It is included when `Customs.CustomsValue > 0`
and `Customs.Items` is non-empty.

| Field | Source | Notes |
|---|---|---|
| `<customsAmount>` | `Customs.CustomsValue × 100` | Integer cent value |
| `<customsCurrency>` | `Customs.CustomsCurrency` | Defaults to `EUR` |
| `<customsTerms>` | `Customs.Incoterms` | Defaults to `"06"` (DAP) |
| `<customsInvoice>` | `Customs.InvoiceNumber` | |
| `<customsInvoiceDate>` | `Customs.InvoiceDate` | YYYYMMDD (dashes stripped) |
| `<customsTarif>` per line | `CustomsItem.HSCode` or `Customs.HSCode` | Falls back to top-level HS code |
| `<grossWeight>` per line | `CustomsItem.NetWeight × quantity` | Converted to decagrams |
| `<amountLine>` per line | `CustomsItem.Value × quantity × 100` | Integer cent value |
| `<customsOrigin>` per line | `CustomsItem.CountryOfOrigin` | ISO 3166-1 alpha-2 |

---

## Weight units

DPD NL expects weight in **decagrams** (1 kg = 100 dg). The adapter converts
using `math.Ceil(kg × 100)` — rounding up to avoid under-declaring weight.

---

## Endpoint mapping

| carrier-gateway | DPD NL SOAP operation | Status |
|---|---|---|
| `POST /api/bookings` | `ShipmentService.storeOrders` | ✅ Implemented |
| `DELETE /api/bookings/{id}` | — | ✅ Returns 501 |
| `PATCH /api/bookings/{id}` | — | ✅ Returns 501 |
| `GET /api/trackings/{id}` | `ParcelLifecycleService.getTrackingData` | ✅ Implemented |
| `GET /api/labels/{id}` | *(in-process label cache)* | ✅ Implemented |
| `POST /api/pickups` | — | ✅ Returns 501 |
| `POST /api/manifests` | — | ✅ Returns 501 |

---

## Environment variables

| Variable | Description |
|---|---|
| `DPD_NL_DELIS_ID` | DPD NL delisID (account identifier) |
| `DPD_NL_PASSWORD` | DPD NL password |

If either variable is unset the adapter falls back to `MockDPDNLAdapter` with a
warning log. Set `MOCK_MODE=true` to force mock mode globally.

---

## Implementation notes

**SOAP endpoint URLs.** The default URLs are:
- `https://wsshipper.dpd.nl/soap/services/LoginService`
- `https://wsshipper.dpd.nl/soap/services/ShipmentService`
- `https://wsshipper.dpd.nl/soap/services/ParcelLifecycleService`

These should be verified against the `soap:address location` in the live WSDLs
(requires DPD NL credentials to access). URLs are overridable on the struct for
testing and sandbox use.

**Sandbox.** DPD NL provides a sandbox at a separate host. Point the struct URL
fields at the sandbox base when running integration tests. The adapter itself has
no built-in sandbox/production toggle.

**Tracking XML schema.** The `ParcelLifecycleService` response struct was derived
from the API documentation. Cross-check field names against the live WSDL schema
(`ParcelLifecycleServiceV20.wsdl`) before go-live.

**Label cache + process restart.** Labels exist only in the current process's
`sync.Map`. Deployments that restart the process between booking and label
consumption will lose cached labels. Mitigate by extracting the label from
`BookingResponse` at booking time and persisting it externally.

**Multi-colli MPS ID.** `BookingResponse.ShipmentID` is the MPS group ID
(`mpsId`). Individual parcel tracking numbers are in `BookingResponse.Colli[i].TrackingNumber`.
Track per parcel, not per MPS group.
