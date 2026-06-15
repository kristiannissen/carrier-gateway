# FedEx — Feature Mapping

API: **FedEx Ship API v1 + Track API v1 + Pickup API v1 + Location Search API v1 + Ship EndofDayClose API v1**
Base URL (prod): `https://apis.fedex.com`
Auth: OAuth2 client credentials (clientID + clientSecret → Bearer token)
Coverage: Worldwide.
Implementation status: **Not fully implemented yet** (Beta)

---

## Summary

FedEx covers booking, cancellation, tracking, and pickup scheduling. Labels are
returned inline in the booking response; the standalone label reprint endpoint
is not yet wired (spec pending). Post-booking update is not supported.
Pickup scheduling covers book, cancel, and availability check via the FedEx
Pickup API v1; update is not supported (cancel-and-rebook). Service point
delivery (Hold at Location) is wired — set `receiver.servicePointId` to the
FedEx `locationId` code (e.g. "YBZA") obtained from the Location Search API.
Customs are wired for international shipments — populate `shipment.customs`
with line items, HS codes, declared values, Incoterms, and EORI/VAT numbers.
IOSS has no FedEx equivalent and is logged as a warning and dropped.
Ground end-of-day manifest close is now implemented via
`PUT /ship/v1/endofday/`; Express accounts do not require a close call.
Email notification, signature required, insurance (declared value), return
labels, and ZPL/PNG label formats are all supported by the FedEx Ship API and
ready to wire — see add-ons and labels tables below. COD is supported for
Ground only. SMS notification and flex delivery have no FedEx API equivalent.

---

## Feature fit/gap

### Booking

| Feature | Implemented | Notes |
|---|---|---|
| Book shipment | ✅ | `POST /ship/v1/shipments` — PDF label returned inline per package |
| Cancel shipment | ✅ | `PUT /ship/v1/shipments/cancel` |
| Update shipment | ❌ | Not supported by FedEx Ship API |
| Idempotency key | ❌ | Client-side only |

### Labels

| Feature | Implemented | Notes |
|---|---|---|
| Label inline at booking | ✅ | `EncodedLabel` in booking response per package |
| Label reprint (FetchLabel) | ❌ | `FetchLabel` returns `ErrNotSupported` — label reprint endpoint spec not yet available |
| Label format | ✅ | `labelSpecification.imageType`: `PDF`, `PNG`, `ZPLII`, `EPL2`. Set `BookingRequest.LabelFormat`; defaults to PDF. |
| Return label | ✅ | `shipmentSpecialServices.returnShipmentDetail` + `returnType: PRINT_RETURN_LABEL`. Set `Shipment.DeliveryType: "return"`. |

### Tracking

| Feature | Implemented | Notes |
|---|---|---|
| Current status | ✅ | `POST /track/v1/trackingnumbers` — normalized status |
| Event history | ✅ | `scanEvents[]` mapped to `events[]` |
| Estimated delivery | ✅ | `dateAndTimes[type=ESTIMATED_DELIVERY]` |

### Pickup scheduling

| Feature | Implemented | Notes |
|---|---|---|
| Book pickup | ✅ | `POST /pickup/v1/pickups` — FDXE (Express) carrier code. Returns opaque token encoding code + date + location. |
| Check availability | ✅ | `POST /pickup/v1/pickups/availabilities` — returns available `PickupSlot` windows |
| Update pickup | ❌ | Not supported by FedEx Pickup API — cancel and rebook |
| Cancel pickup | ✅ | `PUT /pickup/v1/pickups/cancel` — requires the token from BookPickup |

**Confirmation token.** `BookPickup` returns a pipe-delimited opaque token
`{confirmationCode}|{YYYY-MM-DD}|{expressLocation}` rather than the raw FedEx
confirmation code. This is required because the cancel endpoint needs the
scheduled date and Express facility location alongside the code. Pass the token
unchanged to `CancelPickup`; do not attempt to parse it.

### Manifest

| Feature | Implemented | Notes |
|---|---|---|
| Close manifest (Ground) | ✅ | `PUT /ship/v1/endofday/` — closes open Ground (FDXG) shipments for the day; returns MANIFEST document |
| Close manifest (Express) | N/A | FedEx Express does not require an end-of-day close — parcels are scanned at pickup |
| Manifest document | ✅ | Base64-encoded PDF returned in `ManifestResponse.ManifestDocument` |

### Add-ons

| Add-on | Implemented | Notes |
|---|---|---|
| SMS notification | ❌ | No SMS field in FedEx Ship API |
| Email notification | ✅ | `shipmentSpecialServices.emailNotificationDetail` — ON_SHIPMENT, ON_ESTIMATED_DELIVERY, ON_DELIVERY, ON_EXCEPTION. Requires `Receiver.Email`. |
| Flex delivery | ❌ | No equivalent in FedEx Ship API |
| Signature required | ✅ | `packageSpecialServices.signatureOptionType: DIRECT`. Applied to all packages. |
| Cash on delivery | ⚠️ | `shipmentSpecialServices.shipmentCODDetail` — Ground only. `codCollectionType: ANY`. |
| Insurance (declared value) | ✅ | `requestedPackageLineItems[].declaredValue` — value divided evenly across packages. |

### Other features

| Feature | Implemented | Notes |
|---|---|---|
| Customs / cross-border | ✅ | `customsClearanceDetail` wired — commodities, HS codes, declared values, duties payment, EORI/VAT on shipper/recipient parties |
| Service point delivery (HAL) | ✅ | `receiver.servicePointId` → `HOLD_AT_LOCATION` + `holdAtLocationDetail.locationId`. Use Location Search API to look up `locationId`. |
| Multi-colli | ✅ | Multiple `RequestedPackageLineItems` per shipment |
| Service type auto-selection | ✅ | `fedexServiceType()` selects domestic vs. international service based on sender/receiver country |

---

## Endpoint mapping

| carrier-gateway | FedEx API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /ship/v1/shipments` | ✅ |
| `DELETE /api/bookings/{id}` | `PUT /ship/v1/shipments/cancel` | ✅ |
| `PATCH /api/bookings/{id}` | — | ❌ → 501 |
| `GET /api/trackings/{id}` | `POST /track/v1/trackingnumbers` | ✅ |
| `GET /api/labels/{id}` | — | ❌ → 501 (pending spec) |
| `GET /api/pickups/availability` | `POST /pickup/v1/pickups/availabilities` | ✅ |
| `POST /api/pickups` | `POST /pickup/v1/pickups` | ✅ |
| `PUT /api/pickups/{id}` | — | ❌ → 501 (cancel-and-rebook) |
| `DELETE /api/pickups/{id}` | `PUT /pickup/v1/pickups/cancel` | ✅ |
| `POST /api/manifests` | `PUT /ship/v1/endofday/` | ✅ (Ground) / N/A (Express) |
| Service point lookup (caller-side) | `POST /location/v1/locations` | ℹ️ Not a gateway endpoint — callers call FedEx directly to resolve a `locationId` |

---

## Implementation notes

**Beta status.** FedEx is marked Beta (`capabilities["fedex"].Beta = true`).
Booking and tracking are live against the FedEx API; the booking response
includes a `BetaWarning`.

**Label inline only.** Labels are returned as base64-encoded PDF inside the
`BookShipment` response (`ColliResponse.LabelURL` as a data URI). The `FetchLabel`
method is not implemented — callers must save the label from the booking response.
The FedEx label reprint API requires a separate spec review.

**Pickup type.** The adapter sets `pickupType=USE_SCHEDULED_PICKUP` on shipment
bookings, which is compatible with accounts that have a standing collection
agreement. On-demand pickup scheduling is now available via `POST /api/pickups`.

**Pickup token.** The confirmation number returned by `BookPickup` is an opaque
pipe-delimited token (`{code}|{date}|{location}`) rather than the raw FedEx
confirmation code. The cancel endpoint requires all three values, and they
cannot be recovered from the code alone, so encoding them into the token avoids
external state. The `location` segment is empty for Ground (FDXG) pickups and
populated for Express (FDXE) pickups.

**Ground end-of-day close.** `CloseManifest` calls `PUT /ship/v1/endofday/`
with `closeReqType=GCDR` and `groundServiceCategory=GROUND`. The `date` field
in `ManifestRequest` sets `closeDate`; if omitted it defaults to today. The
response includes a base64-encoded MANIFEST PDF in `ManifestResponse.ManifestDocument`.
If no Ground shipments are open for the day, FedEx returns success with an
empty `closeDocuments` list — the adapter surfaces this as a warning, not an
error. FedEx Express (FDXE) accounts do not need a close call; calling
`CloseManifest` on an Express-only account is safe and returns a warning.

**Hold at Location (service point delivery).** Set `receiver.servicePointId`
to the FedEx `locationId` code (4–5 alphanumeric characters, e.g. "YBZA").
The adapter injects `HOLD_AT_LOCATION` into `specialServiceTypes` and populates
`holdAtLocationDetail.locationId` automatically. To look up valid location IDs
near a delivery address, call `POST /location/v1/locations` on the FedEx
Location Search API and filter by `transferOfPossessionType=HOLD_AT_LOCATION`.

**Customs.** International shipments populate `customsClearanceDetail` automatically
when `shipment.customs.items` is non-empty. Field mapping:

| Gateway field | FedEx field |
|---|---|
| `customs.incoterms` | `dutiesPayment.paymentType` — DDP → SENDER, all others → RECIPIENT |
| `customs.customsValue` + `customsCurrency` | `totalCustomsValue {amount, currency}` |
| `customs.invoiceNumber` | `commercialInvoice.customerReferences[type=INVOICE_NUMBER]` |
| `customs.invoiceDate` | `commercialInvoice.comments[0]` (no dedicated date field in FedEx) |
| `customs.exporterVatNumber` | `shipper.tins[tinType=FEDERAL]` |
| `customs.importerOfRecord` | `recipients[0].tins[tinType=BUSINESS_NATIONAL]` (EORI) |
| `customs.importerVatNumber` | `recipients[0].tins[tinType=FEDERAL]` |
| `customs.iossNumber` | ⚠️ **Not supported** — FedEx has no IOSS `tinType`; logged as warning and dropped |
| `customs.items[].description` | `commodities[].description` (required) |
| `customs.items[].hsCode` | `commodities[].harmonizedCode` (falls back to `customs.hsCode`) |
| `customs.items[].countryOfOrigin` | `commodities[].countryOfManufacture` (falls back to `customs.countryOfOrigin`) |
| `customs.items[].quantity` | `commodities[].quantity` + `quantityUnits: "EA"` |
| `customs.items[].netWeight` | `commodities[].weight {units: "KG"}` |
| `customs.items[].value` + `currency` | `commodities[].customsValue {amount, currency}` |

Fields with no FedEx equivalent (`natureOfCargo`, `goodsCategoryCode`, `transportMode`,
`licenceNumber`, `certificateNumber`) are silently omitted — FedEx does not expose
equivalent commodity-level fields in the Ship API v1.
