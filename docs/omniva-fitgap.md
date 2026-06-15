# Omniva — Feature Mapping

API: **Omniva OMX API v1.7**
Docs: `omniva_api.pdf` + `omniva_openapi.json`
Auth: HTTP Basic Auth (username + password)
Coverage: Estonia, Latvia, Lithuania — B2C, B2B, C2C, parcel machines, post offices, courier
Implementation status: **Not implemented**

---

## Summary

The Omniva OMX API covers the core B2C booking and tracking loop. Cancel and
barcode-based tracking map cleanly to the gateway interface. Labels return PDF
only; the gateway's ZPL/PNG formats are not supported.

Material gaps: the three-axis product model (`mainService` / `deliveryChannel`
/ `servicePackage`) does not map to `DeliveryType`; C2C shipments and the
Omniva post-delivery return flow (`/shipments/omniva-return`) have no gateway
equivalent; and the recommended high-volume tracking method uses a stateful
eventID cursor that the gateway has no concept of. Dimensions wire format uses
metres (gateway uses centimetres) — conversion required throughout.

All features below are unimplemented; the table reflects API capability vs.
the expected adapter behaviour once built.

---

## Feature fit/gap

### Booking

| Feature | Support | Omniva endpoint | Gap |
|---|---|---|---|
| Book shipment (B2C) | ✅ | `POST /shipments/business-to-client` | `mainService` / `deliveryChannel` / `servicePackage` not in `BookingRequest` — must be derived or passed via extension field |
| Book shipment (C2C) | ✅ | `POST /shipments/client-to-client` | No C2C concept in gateway; receiver-billed model (`paidByReceiver`) not in `Shipment` |
| Cancel shipment | ✅ | `POST /shipments/cancel` | None |
| Update shipment | ⚠️ | `POST /shipments` | Weight update not supported by Omniva |
| Post-delivery return | ✅ | `POST /shipments/omniva-return` | Distinct from `DeliveryType: "return"` — references original barcode, requires DELIVERED status. No adapter method or handler covers this lifecycle step |
| Consolidated shipments | ✅ | `POST /shipments/business-to-client` | Sub-parcel structure (primary + secondary barcode) not supported by gateway `Colli` array |
| `partnerShipmentId` | ✅ | `POST /shipments` | `Colli.ID` could serve this role but response does not echo it back |
| `returnAllowed` flag | ✅ | `POST /shipments` | Not in `BookingRequest` — enables sender-paid returns initiated by receiver |
| `paidByReceiver` flag | ✅ | `POST /shipments` | Not in `Shipment` — required for C2C and some B2C scenarios |

### Labels

| Feature | Support | Omniva endpoint | Gap |
|---|---|---|---|
| Fetch label | ✅ | `POST /shipments/package-labels` | PDF only — adapter must reject or cap non-PDF format requests |
| Email label delivery | ✅ | `POST /shipments/package-labels` (`sendAddressCardTo: "EMAIL"`) | No email-delivery path in gateway `FetchLabel` interface |

### Tracking

| Feature | Support | Omniva endpoint | Gap |
|---|---|---|---|
| Track by barcode | ✅ | `GET /shipments/{barCode}` | None |
| Cursor-based poll | ✅ | `GET /shipments?fromTrackEventId=` | Stateful tracking cursor not in gateway — no background poller. Omniva rate limit: 5 req / 5 min per account. **Recommended for > 50 shipments/day** |

### Pickup scheduling

| Feature | Support | Omniva endpoint | Gap |
|---|---|---|---|
| Book pickup | ✅ | `POST /courierorders/create-pickup-order` | Pickup availability check missing (see below) |
| Cancel pickup | ✅ | `POST /courierorders/cancel-pickup-order` | None |
| Get courier order | ✅ | `GET /courierorders/{courierOrderNumber}` | No gateway equivalent — cannot retrieve a previously booked pickup |
| Pickup availability | ✅ | `POST /courierorders/pickup-availability` | No gateway discovery call. Required to avoid `no.availability.zone.found.error` |

### Manifest / end-of-day

| Feature | Support | Omniva endpoint | Gap |
|---|---|---|---|
| Close manifest | ❌ | — | Not required or available |

### Add-ons

| Add-on | Support | Notes |
|---|---|---|
| COD | ✅ | Core fields map. Missing: `COD_RECEIVER` (name of payment recipient) and `COD_REFERENCE_NO` (validated against EE bank rules for Estonian IBANs) |
| Insurance | ✅ | `INSURANCE` addService — `AddOnInsurance.InsuranceValue` maps directly |
| SMS notification | ✅ | Included in `DELIVERY_TO_PRIVATE_PERSON` service; mobile mandatory when used |
| Email notification | ✅ | Included in `DELIVERY_TO_PRIVATE_PERSON` service; email mandatory when used |
| Delivery to specific person | ✅ | `DELIVERY_TO_A_SPECIFIC_PERSON` — requires personal ID code field not in gateway. Mandatory for PARCEL_MACHINE delivery |
| Delivery to private person | ✅ | `DELIVERY_TO_PRIVATE_PERSON` — triggers contact detail requirements (mobile or email). Not in `AddOnType` |
| Fragile | ✅ | `FRAGILE` — allowed in consolidated shipments. Not in `AddOnType` |
| Document return | ✅ | `DOCUMENT_RETURN` — triggers consolidated shipment rules. Not in `AddOnType` |
| Multiple parcels together | ✅ | `MULTIPLE_PARCELS_DELIVERY_TOGETHER` — triggers consolidated shipment rules. Not in `AddOnType` |
| Flex delivery | ❌ | Not available in Omniva OMX API |

### Customs / cross-border

| Feature | Support | Notes |
|---|---|---|
| Customs items | ✅ | `description`, `quantity`, `netWeight`, `value`, `HSCode`, `countryOfOrigin` map to `shipmentItems` |
| `goodsCategoryCode` | ✅ | Required for non-EU — SALE_OF_GOODS, GIFT, COMMERCIAL_SAMPLE, DOCUMENTS, RETURNED_GOODS, OTHER. Not in gateway `Customs` struct. Note: US destinations disallow GIFT |
| `licenceNumber` | ✅ | Not in `Customs` struct |
| `certificateNumber` | ✅ | Not in `Customs` struct |
| `senderCustomsReference` | ✅ | Not in `Customs` struct |
| `importersReference` | ✅ | Not in `Customs` struct |
| `categoryExplanation` | ✅ | Required when `goodsCategoryCode` is OTHER. Not in `Customs` struct |

### Other

| Feature | Support | Notes |
|---|---|---|
| Service point delivery | ✅ | `ServicePointID` → `offloadPostcode` (parcel machine or post office) |
| Sender `altName` | ✅ | Overrides partner name on label and notifications. No secondary name field in `Address` |
| `useSenderAddressForReturn` | ✅ | Controls whether returns go to sender or pre-configured handover location. Not in `Address` or `Shipment` |
| `shipmentComment` | ✅ | 128-char delivery instruction. No equivalent in `Shipment`; `AddOn.Instructions` is flex_delivery only |
| `fileId` | ✅ | Client request correlation ID distinct from `partnerShipmentId`. Low priority |

---

## Endpoint mapping

| carrier-gateway | Omniva OMX API | Status |
|---|---|---|
| `POST /api/bookings` | `POST /shipments/business-to-client` | ❌ not implemented |
| `DELETE /api/bookings/{id}` | `POST /shipments/cancel` | ❌ not implemented |
| `PATCH /api/bookings/{id}` | `POST /shipments` (update) | ❌ not implemented (weight update unsupported by Omniva) |
| `GET /api/trackings/{id}` | `GET /shipments/{barCode}` | ❌ not implemented |
| `GET /api/labels/{id}` | `POST /shipments/package-labels` | ❌ not implemented (PDF only) |
| `POST /api/pickups` | `POST /courierorders/create-pickup-order` | ❌ not implemented |
| `PUT /api/pickups/{id}` | — | ❌ no Omniva endpoint |
| `DELETE /api/pickups/{id}` | `POST /courierorders/cancel-pickup-order` | ❌ not implemented |
| `POST /api/manifests` | — | ❌ not required |

### Omniva-only endpoints (no gateway equivalent)

| Omniva endpoint | Notes |
|---|---|
| `POST /shipments/client-to-client` | C2C shipment — receiver-billed; no gateway concept |
| `POST /shipments/omniva-return` | Post-delivery return; requires original shipment in DELIVERED state |
| `GET /shipments?fromTrackEventId=` | Cursor-based tracking poll; recommended for production volumes |
| `POST /courierorders/pickup-availability` | Timeslot discovery before booking a pickup |
| `GET /courierorders/{courierOrderNumber}` | Retrieve a previously booked courier order |

---

## Implementation notes

**Product model.** Omniva requires `mainService` (PARCEL / LETTER / PALLET),
`deliveryChannel` (COURIER / POST_OFFICE / PARCEL_MACHINE), and `servicePackage`
(e.g. ECONOMY / STANDARD / PREMIUM for parcels). The gateway's `DeliveryType`
does not cover this three-axis model. The adapter must either accept these
values in a carrier-specific extension field or derive them from `DeliveryType`
+ `ServicePointID` with sensible defaults (e.g. PARCEL + ECONOMY + COURIER).
`servicePackage.allowedStoringPeriod` (PROCEDURAL_DOCUMENT only) has no
gateway equivalent.

**Dimensions.** Omniva wire format uses **metres**. The gateway uses
**centimetres**. Divide length/width/height by 100 on every outbound request.

**`X-Integration-Agent-Id` header.** Required on all requests to identify the
integration platform. No shared mechanism exists for per-carrier custom request
headers — the adapter must inject this from config.

**Cursor-based tracking.** For production volumes (> 50 shipments/day) Omniva
strongly recommends `GET /shipments?fromTrackEventId=` rather than individual
barcode lookups. The gateway has no stateful cursor concept. Rate limit on the
barcode endpoint: 5 requests / 5 minutes per account — 429s must be propagated
correctly.

**Pickup flow.** Always call `POST /courierorders/pickup-availability` before
`POST /courierorders/create-pickup-order` to avoid the
`no.availability.zone.found.error` error. This endpoint has no gateway
analogue; the adapter will need to call it internally as a pre-flight check.

**C2C and post-delivery returns.** Both require new methods or extension points
on `CarrierAdapter`; they cannot be expressed through existing booking fields.

**Auth.** HTTP Basic Auth — the standard gateway credential pattern covers
this without changes.

---

*Generated 2026-06-13 from omniva_api.pdf v1.7 and omniva_openapi.json.*
