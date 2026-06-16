# Econt Feature Mapping

Econt Express API v1.0. Base URL: `https://ee.econt.com/services` (POST only, Basic auth).
Test URL: `https://demo.econt.com/ee/services` — credentials `iasp-dev` / `1Asp-dev`.

**Currency note:** From 01.01.2026 the functional currency is EUR. COD and declared-value services require an explicit `currency` field. Waybills previously created in BGN are auto-converted.

**Geography:** Bulgaria primary market; international coverage via Econt offices in BG, GR, RO and other EU/non-EU countries reachable through the carrier's network.

---

## Fit — Core `CarrierAdapter` interface

| Gateway method | Econt endpoint | Notes |
|---|---|---|
| `BookShipment` | `LabelService.createLabel` (mode=`create`) | Full fit. Single label; supports validate/calculate/create modes in one call. |
| `TrackShipment` | `ShipmentService.getShipmentStatuses` | Full fit. Returns `trackingEvents` array with timestamps, location, event type, plus `shortDeliveryStatusEn` for normalized mapping. |
| `FetchLabel` | No dedicated endpoint — `pdfURL` returned inside `ShipmentStatus` | Partial fit. Store `pdfURL` from booking or status response; fetch via HTTP. Not a direct label-by-tracking-number call. |
| `CancelShipment` | `LabelService.deleteLabels` | Full fit. Accepts multiple shipment numbers; returns per-number error results. |
| `UpdateShipment` | `LabelService.updateLabel` / `updateLabels` | Full fit. Use `checkPossibleShipmentEditions` first to know what updates are allowed for an accepted shipment. |

---

## Fit — Additional gateway capabilities

| Feature | Econt support | Endpoint |
|---|---|---|
| Batch booking | Yes | `LabelService.createLabels` — takes array of `ShippingLabel`; supports async with email result |
| Courier pickup booking | Yes | `ShipmentService.requestCourier` — time window, shipment type, attach prepared AWBs |
| Pickup status | Yes | `ShipmentService.getRequestCourierStatus` — statuses: unprocess / process / taken / reject |
| Address validation | Yes | `AddressService.validateAddress` |
| Service point / office lookup | Yes | `NomenclaturesService.getOffices`, `AddressService.getNearestOffices` |
| Nearest offices (geo) | Yes | `AddressService.getNearestOffices` |
| Service day/time check | Yes | `AddressService.addressServiceTimes` |
| Cash on delivery (COD) | Yes | `ShippingLabelServices.cdAmount` + `cdType` + `cdCurrency` + `cdPayOptions` — supports office, door, bank payout |
| Declared value / insurance | Yes | `ShippingLabelServices.declaredValueAmount` + `declaredValueCurrency` |
| Email notification on delivery | Yes | `ShippingLabel.emailOnDelivery` |
| SMS notification | Yes | `ShippingLabel.smsOnDelivery` (boolean flag); `ShippingLabelServices.smsNotification` |
| Return shipment | Yes (instruction-based) | `ReturnInstructionParams` embedded in `Instruction` on the original label — configures destination, payer, days-until-return, reject actions |
| Delivery receipt (signed) | Yes | `ShippingLabelServices.deliveryReceipt` (DC), `digitalReceipt` (EDC), `goodsReceipt` (DC-CP) |
| Pay after inspect / test | Yes | `ShippingLabel.payAfterAccept`, `payAfterTest` |
| Partial delivery | Yes | `ShippingLabel.partialDelivery` |
| Customs TARIC codes | Yes | `ShippingLabel.customsList` (cn, description, sum, currency) + `customsInvoice` |
| Packing list | Yes | `ShippingLabel.packingListType` + `packingList` (inventory num, description, weight, count, price) |
| Per-pack dimensions | Yes | `ShippingLabel.packs` array of `PackElement` (w/h/l/weight) |
| Priority delivery window | Yes | `ShippingLabelServices.priorityTimeFrom` / `priorityTimeTo` |
| Holiday / specific date delivery | Yes | `ShippingLabel.holidayDeliveryDay` |
| AWB list (own shipments) | Yes | `ShipmentService.getMyAWB` — paginated, filterable by date range and sender/receiver/all |
| Client profile lookup | Yes | `ProfileService.getClientProfiles` — returns addresses, COD payment options, instruction templates |
| COD agreement setup | Yes | `ProfileService.createCDAgreement` |
| Shipment grouping | Yes | `LabelService.grouping` / `groupingCancelation` — group multiple AWBs under one master label |
| Three-way logistics | Yes (separate service) | `ThreeWayLogisticsService.threeWayLogistics` — orchestrates supplier → receiver flows |
| Payment / COD report | Yes | `PaymentReportService.PaymentReport` — date-range query returning COD collected/paid rows |
| Country / city data | Yes | `NomenclaturesService.getCountries`, `getCities` — includes service days, zones, express availability |
| Street / quarter data | Yes | `NomenclaturesService.getStreets`, `getQuarters` |
| ITU code assignment | Yes | `ShipmentService.setITUCode` — links truck registration to AWB |

---

## Gaps

| Gap | Impact | Notes |
|---|---|---|
| **No direct label fetch endpoint** | Medium | `pdfURL` is returned in `ShipmentStatus`. The adapter must call `getShipmentStatuses` and then GET the PDF URL. `FetchLabel` can be implemented as a two-step HTTP call but it is not a single API call like other carriers. |
| **No manifest / end-of-day close** | Low | Econt has no manifest endpoint. Courier collection is confirmed via `requestCourier` + `getRequestCourierStatus`. The gateway manifest endpoint (`POST /api/manifests`) would return `501 Not Implemented` for Econt. |
| **No push webhooks from Econt** | Medium | Econt is poll-only. Status updates require calling `getShipmentStatuses`. The parcel-poller companion service is needed to turn this into event-driven tracking. |
| **No labelless return flow** | Low | Returns are pre-configured as instructions at label creation time, not booked separately after the fact. `POST /api/returns` would need to create a new label with return instructions rather than calling a return-specific endpoint. |
| **Basic auth only** | Low | Username + password in every request. No API key, no OAuth, no token rotation. Add `ECONT_USERNAME` and `ECONT_PASSWORD` env vars. SSL client certificate auth is explicitly not supported by Econt. |
| **SOAP + JSON, not REST** | Low | All calls are HTTP POST to service-specific paths. The adapter uses JSON mode. No REST-style resource URLs — tracking number is a body parameter, not a path segment. |
| **No explicit shipment cancellation post-acceptance** | Medium | `deleteLabels` works only before the shipment is accepted by Econt. Once accepted, `checkPossibleShipmentEditions` must be used to determine available update types; full cancel may not be possible. `CancelShipment` should check acceptance state and return a clear error if the AWB is already in transit. |
| **No service point search by coordinates alone** | Low | `getNearestOffices` requires a validated address, not raw lat/lng. Office data includes `isAPS` (automated post station) and `isMPS` (mobile post station) flags. |
| **Three-way logistics not mapped to gateway model** | Informational | `threeWayLogistics` is a distinct flow (requester → supplier → receiver) with no equivalent in the current gateway interface. Out of scope unless Unisport needs drop-shipping orchestration via Econt. |
| **Payment report not mapped** | Informational | Useful for COD reconciliation but has no gateway endpoint. Could be a future `/api/reports` extension or a direct integration point for the finance system. |

---

## Shipment types supported

| Econt type | Description | Equivalent gateway usage |
|---|---|---|
| `document` | Documents ≤ 0.5 kg | `deliveryType: home`, low-weight colli |
| `pack` | Parcel ≤ 50 kg | Standard e-commerce parcel |
| `pallet` | 80×120×180 cm, ≤ 1000 kg | Freight / B2B |
| `cargo` | Oversized pallet ≤ 200×200×180 cm, ≤ 500 kg | Heavy freight |
| `documentpallet` | Pallet + documents | Mixed freight |
| `big_letter` / `small_letter` | Letter formats | Low-value flat items |
| `money_transfer` / `pp` | Financial instruments | Out of scope for gateway |

---

## Normalized status mapping

| Econt `shortDeliveryStatusEn` | Gateway `normalizedStatus` |
|---|---|
| Prepared in eEcont | `booked` |
| Accepted in Econt | `picked_up` |
| In route | `in_transit` |
| In courier / In delivery courier's office | `out_for_delivery` |
| Arrived in office / In pick up courier | `in_transit` |
| Accepted in office | `in_transit` |
| Arrival departure from hub | `in_transit` |
| Delivered | `delivered` |
| Is returning to sender / Returned to sender | `returned` |
| Cancelled after sending / Cancelled before sending | `failed` |

---

## Environment variables

| Variable | Description |
|---|---|
| `ECONT_USERNAME` | e-Econt username (Basic auth) |
| `ECONT_PASSWORD` | e-Econt password (Basic auth) |
| `ECONT_BASE_URL` | Override base URL (default: `https://ee.econt.com/services`) |

---

## Implementation checklist

Follows the standard "Adding a carrier" steps from the README.

- [ ] `internal/adapter/econt.go` — implement `CarrierAdapter`
- [ ] `internal/adapter/mock_econt.go`
- [ ] `internal/adapter/econt_test.go`
- [ ] Register in `InitAdapters` in `adapter.go` with `ECONT_USERNAME` / `ECONT_PASSWORD` guard
- [ ] Add `capabilities` entry in `adapter.go`
- [ ] Add status mappings in `status.go` (key: `shortDeliveryStatusEn`)
- [ ] Add limits entry in `validation/package.go` (max weight 50 kg pack, 1000 kg pallet)
- [ ] Add `carrierCustomsRules` entry in `validation/carrier_customs.go` (TARIC codes required for cross-border)
- [ ] Add restricted goods entries in `validation/restricted.go`
- [ ] Wire customs fields in `adapter/customs.go`
- [ ] `FetchLabel`: implement as `getShipmentStatuses` → extract `pdfURL` → HTTP GET → base64
- [ ] `CancelShipment`: call `deleteLabels`; handle already-accepted state via `checkPossibleShipmentEditions`
- [ ] Pickup: implement `requestCourier` + `getRequestCourierStatus` behind `/api/pickups`
- [ ] Manifest: return `ErrNotSupported`
