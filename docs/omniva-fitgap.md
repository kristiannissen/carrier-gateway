# Omniva API — Fit/Gap Analysis

Comparing the Omniva OMX API (v1.7, docs `omniva_api.pdf` + `omniva_openapi.json`) against the carrier-gateway `CarrierAdapter` interface and shared domain model.

---

## Summary

| Area | Fit | Gap |
|---|---|---|
| Book shipment (B2C) | Partial | mainService, deliveryChannel, servicePackage missing |
| Book shipment (C2C) | None | No C2C concept in gateway |
| Omniva return shipment | None | Different from gateway's return DeliveryType |
| Cancel shipment | Full | — |
| Update shipment | Partial | Weight update not supported by Omniva |
| Fetch label | Partial | PDF only; no email delivery option |
| Track by barcode | Full | — |
| Track by eventID / timestamp | None | Gateway has no cursor-based poll |
| Courier pickup create/cancel | Partial | Interface exists; pickup availability endpoint missing |
| Additional services | Partial | Several Omniva-specific add-ons missing |
| Customs | Partial | goodsCategoryCode, licence/cert/invoice numbers missing |

---

## Endpoint mapping

| Omniva endpoint | Gateway method | Status |
|---|---|---|
| `POST /shipments/business-to-client` | `BookShipment` | Partial |
| `POST /shipments/client-to-client` | — | Gap |
| `POST /shipments/omniva-return` | — | Gap |
| `POST /shipments` (update) | `UpdateShipment` | Partial |
| `POST /shipments/package-labels` | `FetchLabel` | Partial |
| `POST /shipments/cancel` | `CancelShipment` | Fit |
| `GET /shipments` (eventID / time poll) | — | Gap |
| `GET /shipments/{barCode}` | `TrackShipment` | Fit |
| `POST /courierorders/create-pickup-order` | pickups handler | Fit |
| `POST /courierorders/cancel-pickup-order` | pickups handler | Fit |
| `POST /courierorders/pickup-availability` | — | Gap |
| `GET /courierorders/{courierOrderNumber}` | — | Gap |

---

## Detailed gaps

### 1. No mainService / deliveryChannel / servicePackage on BookingRequest

Omniva requires three product selectors per shipment:
- `mainService` — PARCEL, LETTER, or PALLET
- `deliveryChannel` — COURIER, POST_OFFICE, or PARCEL_MACHINE (conditional, market-dependent)
- `servicePackage` — ECONOMY, STANDARD, PREMIUM (parcels) or PROCEDURAL_DOCUMENT, REGISTERED_LETTER, REGISTERED_MAXILETTER (letters)

The gateway's `Shipment.DeliveryType` (`home`, `business`, `servicepoint`, `return`) does not map cleanly to this three-axis model. An Omniva adapter must either:
- Accept these three values in a carrier-specific extension field on the request, or
- Derive them from `DeliveryType` + `ServicePointID` with a hard-coded default (e.g. always PARCEL + ECONOMY)

`servicePackage.allowedStoringPeriod` (0 / 15 / 30 days for PROCEDURAL_DOCUMENT) has no gateway equivalent at all.

### 2. No C2C shipment type

`POST /shipments/client-to-client` is a distinct Omniva concept: the sender is never billed, fees are always paid by the receiver. The gateway has no `Shipment.BillingModel` or equivalent. A new field or a booking-level flag is needed.

### 3. No Omniva return flow (`/shipments/omniva-return`)

The gateway's `DeliveryType: "return"` generates a return label at booking time. Omniva's return flow is different: it is called *after delivery*, references the original barcode, and requires the original shipment to be in DELIVERED status. No adapter method or handler covers this lifecycle step. A new method on `CarrierAdapter` (or a standalone endpoint on the gateway) is needed.

### 4. Cursor-based tracking poll (eventID and time-based methods)

The gateway's `TrackShipment(trackingNumber string)` maps to Omniva's barcode-based method (`GET /shipments/{barCode}`). Omniva strongly recommends the eventID-based method for volumes above 50 shipments/day: the caller stores the last `eventId` from the response and passes `fromTrackEventId` on the next poll.

The gateway has no equivalent of a stateful tracking cursor, no `GET /events?fromEventId=` endpoint, and no background poller. This is the recommended integration pattern for production volumes — it will need to be designed from scratch.

Omniva's rate limiter: 5 requests / 5 minutes per account on the TRT endpoint. The adapter must respect this and propagate 429s correctly.

### 5. `paidByReceiver` flag

Omniva supports receiver-billed shipments for PARCEL and PALLET. No equivalent in `Shipment` or `BookingRequest`. Needed for C2C and some B2C scenarios.

### 6. `returnAllowed` flag

When true, Omniva generates a return code sent to the recipient as a notification. The receiver can then initiate a sender-paid return without calling the gateway. No equivalent in `BookingRequest`.

### 7. Missing AddOn types

The gateway's `AddOnType` enum covers `sms_notification`, `email_notification`, `flex_delivery`, `signature_required`, `cash_on_delivery`, `insurance`. Omniva adds:

| Omniva addService | Notes |
|---|---|
| `DELIVERY_TO_A_SPECIFIC_PERSON` | Requires personal ID code (`DELIVERY_TO_SPECIFIC_PERSON_PERSONAL_CODE`); mandatory for PARCEL_MACHINE |
| `DELIVERY_TO_PRIVATE_PERSON` | Triggers contact detail requirements (mobile or email mandatory) |
| `FRAGILE` | Allowed in consolidated shipments |
| `DOCUMENT_RETURN` | Triggers consolidated shipment rules |
| `MULTIPLE_PARCELS_DELIVERY_TOGETHER` | New in v1.1; triggers consolidated shipment rules |

### 8. COD gaps

The gateway's `AddOn` covers `CODAmount`, `CODCurrency`, `CODAccountNumber`. Omniva also requires:
- `COD_RECEIVER` — name of person who receives the COD payment (distinct from `CODAccountNumber` holder)
- `COD_REFERENCE_NO` — payment reference number; validated per EE bank rules when account is Estonian IBAN

### 9. Consolidated shipments

Omniva allows grouping sub-parcels under a main shipment when using COD, DOCUMENT_RETURN, or MULTIPLE_PARCELS_DELIVERY_TOGETHER. Each sub-parcel has its own barcode/partnerShipmentId and may carry a FRAGILE add-on. The gateway's `Colli` array represents multiple packages per booking but has no concept of an Omniva-style consolidated shipment with a primary + secondary barcode structure.

### 10. Sender `altName` field

Omniva's `senderAddressee.altName` overrides the partner's registered name on the printed label and in notification messages. The gateway's `Address.Name` is the sender name; there is no secondary/override name field.

### 11. Sender `useSenderAddressForReturn`

Controls whether returns go to the sender's address or to the pre-configured handover location. No equivalent in `Address` or `Shipment`.

### 12. `partnerShipmentId`

Omniva allows the caller to supply its own ID instead of an Omniva barcode, and the response echoes it back for correlation. The gateway's `Colli.ID` could serve this role, but the mapping is not explicit and the response `ColliResponse` does not carry it through.

### 13. `shipmentComment`

Free-text delivery instruction (128 chars). No equivalent in `Shipment`. Closest is `AddOn.Instructions` (flex_delivery only).

### 14. Customs — `goodsCategoryCode`

Omniva requires `goodsCategoryCode` (SALE_OF_GOODS, GIFT, COMMERCIAL_SAMPLE, DOCUMENTS, RETURNED_GOODS, OTHER) for non-EU destinations. The gateway's `Customs` struct has no category enum. Note: US destinations do not allow the GIFT category via OMX.

### 15. Customs — additional declaration fields

Missing from the gateway's `Customs` struct:
- `licenceNumber` — export licence reference
- `certificateNumber` — phytosanitary or other certificate
- `invoiceNumber` — commercial invoice number (gateway has this; confirm it is wired through)
- `senderCustomsReference`
- `importersReference`
- `categoryExplanation` — required when `goodsCategoryCode` is OTHER

### 16. Fetch label — PDF only / email delivery

Omniva's `/package-labels` endpoint returns PDF only. The gateway's `FetchLabel` supports PDF, PNG, ZPL, EPL, ZPLGK. An Omniva adapter must reject non-PDF format requests or silently cap them at PDF.

Omniva also supports `sendAddressCardTo: "EMAIL"` — the label is emailed directly rather than returned in the response body. The gateway's `FetchLabel` interface has no email-delivery path; this mode would require a separate operation.

### 17. Pickup availability endpoint

`POST /courierorders/pickup-availability` returns available timeslots for a given address before creating a pickup order. The gateway has no equivalent discovery call — callers create pickups directly. This is needed to avoid the error `com.omniva.phoenix.no.availability.zone.found.error`.

### 18. Get courier order (`GET /courierorders/{courierOrderNumber}`)

No equivalent in the gateway. The gateway's pickup handler can create and cancel but not retrieve a previously booked pickup order.

### 19. Dimensions unit mismatch

Omniva's wire format uses **metres** for length/width/height. The gateway's `Dimensions` struct uses **centimetres**. The adapter must divide by 100 on output.

### 20. `X-Integration-Agent-Id` header

Omniva requires this header on all requests to identify the integration platform. The adapter must inject it from config. No shared mechanism exists in the gateway for per-carrier custom request headers.

### 21. `fileId` on booking request

Optional client-generated identifier for request-level log correlation (distinct from `partnerShipmentId`). No equivalent; low priority but useful for debugging.

---

## What fits without changes

- **Auth**: HTTP Basic Auth — gateway credential pattern covers this.
- **Cancel shipment**: `CancelShipment(trackingNumber)` maps directly to `POST /shipments/cancel`.
- **Track by barcode**: `TrackShipment(trackingNumber)` maps to `GET /shipments/{barCode}`.
- **Sender/receiver address**: `Address` struct covers name, street, houseNo, city, postcode, country, phone, email, and `ServicePointID` → `offloadPostcode` (parcel machine/post office).
- **Weight and dimensions**: Covered by `Colli` / `Dimensions` (unit conversion required, see gap 19).
- **COD core fields**: `AddOnCashOnDelivery` with `CODAmount`, `CODCurrency`, `CODAccountNumber` (IBAN) covers the primary COD fields.
- **Insurance**: `AddOnInsurance` with `InsuranceValue` maps to Omniva's INSURANCE additional service.
- **Customs items**: `CustomsItem.Description`, `.Quantity`, `.NetWeight`, `.Value`, `.HSCode`, `.CountryOfOrigin` map to Omniva's `shipmentItems` block.
- **Pickup create/cancel**: `pickups.go` handler covers both `POST /courierorders/create-pickup-order` and `POST /courierorders/cancel-pickup-order`.
- **Error handling**: Omniva's `resultCode: OK/ERROR` + `messageCode` response shape maps cleanly to the gateway's error propagation pattern.

---

*Generated 2026-06-13 from omniva_api.pdf v1.7 and omniva_openapi.json.*
