# Implementation Status

Cross-carrier summary of feature coverage. Each carrier has a dedicated
feature mapping file in this folder with full detail.

---

## Carriers

| Carrier | Status | Coverage | File |
|---|---|---|---|
| PostNord | Implemented | DK, SE, NO, FI | [postnord-feature-mapping.md](postnord-feature-mapping.md) |
| Bring | Implemented | NO, SE, DK, FI | [bring-feature-mapping.md](bring-feature-mapping.md) |
| GLS | Implemented | DE, DK, SE, NL, BE, FR, ES, PT, IT, AT + more | [gls-feature-mapping.md](gls-feature-mapping.md) |
| GLS NL (regional) | Not fully implemented yet (Beta) | NL, BE + other GLS national portals | [gls-nl-feature-mapping.md](gls-nl-feature-mapping.md) |
| DAO | Implemented | DK only | [dao-feature-mapping.md](dao-feature-mapping.md) |
| DHL Express | Not fully implemented yet (Beta) | Worldwide | [dhl-express-feature-mapping.md](dhl-express-feature-mapping.md) |
| DHL eCommerce Europe | Not fully implemented yet (Beta) | 28 European countries | [dhl-ecommerce-feature-mapping.md](dhl-ecommerce-feature-mapping.md) |
| DPD | Not fully implemented yet (Beta) | Pan-European | [dpd-group-feature-mapping.md](dpd-group-feature-mapping.md) |
| DPD NL | Not fully implemented yet (Beta) | NL | [dpd-nl-feature-mapping.md](dpd-nl-feature-mapping.md) |
| DPD UK | Not fully implemented yet (Beta) | GB | — |
| Hermes Germany | Not fully implemented yet (Beta) | DE only | [hermes-feature-mapping.md](hermes-feature-mapping.md) |
| FedEx | Implemented | Worldwide | [fedex-feature-mapping.md](fedex-feature-mapping.md) |
| Evri | Not fully implemented yet (Beta) | GB | — |
| DHL eCommerce UK | Not fully implemented yet (Beta) | GB | [dhl-ecommerce-feature-mapping.md](dhl-ecommerce-feature-mapping.md) |
| Omniva | Implemented | EE, LV, LT | — |
| InPost | Implemented | PL (shipping + pickups + returns), IT + GB (returns) | [inpost-feature-mapping.md](inpost-feature-mapping.md) |

---

## Core adapter features

✅ = Implemented and live  ⚠️ = Partial or caveated  ❌ = Not supported or not wired  ❓ = Unknown / not confirmed

| Feature | PostNord | Bring | GLS | GLS NL | DAO | DHL Express | DHL eCom EU | DHL eCom UK | DPD | Hermes | FedEx | InPost |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **Book shipment** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Cancel shipment** | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ✅ | ❌ |
| **Update shipment** | ⚠️ | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Tracking + events** | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ |
| **Labels** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Return labels** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Idempotency (native)** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |

---

## Pickup scheduling

| Feature | PostNord | Bring | GLS | GLS NL | DAO | DHL Express | DHL eCom EU | DHL eCom UK | DPD | Hermes | FedEx | InPost |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **Book pickup** | ⚠️ | ✅ | ❌ | ✅ | ❌ | ⚠️ | ❓ | ✅ | ✅ | ❓ | ✅ | ✅ PL only |
| **Update pickup** | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ❓ | ❌ | ❌ | ❓ | ❌ | ❌ |
| **Cancel pickup** | ❌ | ❌ | ❌ | ✅ | ❌ | ✅ | ❓ | ❌ | ✅ | ❓ | ✅ | ✅ PL only |
| **Pickup availability** | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❓ | ❌ | ❌ | ❓ | ✅ | ❌ |

**PostNord pickup note:** Domestic DK/SE/FI only. Requires item IDs from booking response.
**DHL Express pickup note:** Implicit at booking (returns `dispatchConfirmationNumber`). Standalone `POST /api/pickups` not yet wired.
**GLS pickup note:** `POST /rs/sporadiccollection` exists in ShipIT API but not yet wired.
**GLS NL pickup note:** `POST /CreatePickup` — requires three address blocks; all default to the single pickup address when only one is provided.
**FedEx pickup note:** Update not supported — cancel-and-rebook. Confirmation number is an opaque token encoding code + date + Express location.

---

## Manifest / end-of-day

| Carrier | Manifest close | Manifest document | Notes |
|---|---|---|---|
| PostNord | ❌ | ❌ | Handled by EDI scan at collection |
| Bring | ❌ | ❌ | No API support |
| GLS | ❌ | ❌ | **Required** — `POST /rs/shipments/endofday` exists but not wired. Must be called before driver arrives. |
| GLS NL | ✅ | ❌ | Per-unit `POST /ConfirmLabel` — CloseManifest iterates over all tracking numbers. No manifest PDF. |
| DAO | ❌ | ❌ | No API support |
| DHL Express | ❌ | ✅ | Post-collection only via `GET /shipments/{id}/get-image?typeCode=MANIFEST` |
| DHL eCommerce EU | ❓ | ❓ | Not confirmed |
| DHL eCommerce UK | ❌ | ❌ | No manifest API — shipments processed automatically by DHL |
| DPD | ❌ | ❌ | No API support — pickup order acts as handover instruction |
| Hermes | ❓ | ❓ | Not confirmed |
| FedEx | ✅ | ✅ | Ground only — `PUT /ship/v1/endofday/`. Express accounts do not require a close. |
| InPost | N/A | N/A | Locker network — no manifest |

---

## Add-ons

| Add-on | PostNord | Bring | GLS | GLS NL | DAO | DHL Express | DHL eCom EU | DHL eCom UK | DPD | Hermes | FedEx | InPost |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| SMS notification | ✅ | ✅ | ❌ | ❌ | ⚠️ | ⚠️ | ❓ | ❌ | ✅ | ✅ | ❌ | ❌ |
| Email notification | ✅ | ✅ | ❌ | ✅ | ⚠️ | ✅ | ❓ | ⚠️ | ✅ | ✅ | ✅ | ❌ |
| Flex delivery | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ | ⚠️ | ❌ | ❓ | ❌ | ❌ | ❌ |
| Signature required | ✅ | ✅ | ❌ | ❌ | ❌ | ⚠️ | ⚠️ | ✅ | ❓ | ✅ | ✅ | ❌ |
| Cash on delivery | ❌ | ✅ | ❌ | ❌ | ❌ | ⚠️ | ✅ | ❌ | ✅ | ✅ | ⚠️ | ❌ |
| Insurance | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ❓ | ❌ | ✅ | ❌ |

**DHL eCom EU `flex_delivery` and `signature_required`** are accepted but silently skipped — logged as warnings. Marked ⚠️ until properly wired.

**DHL eCom UK email notification** is automatic when `consigneeAddress.email` is set — DHL sends pre-delivery notifications without an explicit add-on mapping. Marked ⚠️.

---

## Customs support

| Carrier | Customs | Notes |
|---|---|---|
| PostNord | ✅ | v3 EDI customs block — HS codes, EORI, VAT, Incoterms, line items |
| Bring | ✅ | NatureOfCargo, Incoterms, HS codes, EORI/VAT. Required for NO shipments from EU. |
| GLS | ✅ | customs-consignments-v3 API. CN22/CN23 generation. |
| DAO | ❌ | Denmark domestic only |
| DHL Express | ✅ | Full customs block — Incoterms, IOSS, EORI, invoice number/date, line items |
| DHL eCommerce EU | ✅ | Customs data block for cross-border |
| DHL eCommerce UK | ✅ | `customsDetails[]` per piece — HS code (8-digit), DDP/DAP, IOSS, EORI/VAT registrations. Windsor Framework (GB→NI) not wired |
| DPD | ✅ | customs inter block with EORI/VAT in shipment payload |
| Hermes | ❌ | Germany domestic only |
| FedEx | ✅ | `customsClearanceDetail` wired — commodities, HS codes, EORI/VAT, Incoterms, declared value. IOSS passed via `shipper.tins[usage=IOSS]`. |
| InPost | ✅ | Shipment-level + per-parcel customs; GB subdivision codes; max 10 line items |

---

## Service point / locker delivery

| Carrier | Support | Field |
|---|---|---|
| PostNord | ✅ | `receiver.servicePointId` → `servicePointId` |
| Bring | ✅ | `receiver.servicePointId` → `pickupPointId` |
| GLS | ✅ | `receiver.servicePointId` → `parcelShopId` (ShopDeliveryService) |
| DAO | ✅ | `receiver.servicePointId` → `shopid` |
| DHL Express | ✅ | `receiver.servicePointId` → `onDemandDelivery.servicePointId` (6-char code) |
| DHL eCommerce EU | ✅ | `deliveryType=parcelshop/parcelstation/postOffice` |
| DHL eCommerce UK | ✅ | `receiver.servicePointId` → `consigneeAddress.locationId` + `addressType=servicePoint` |
| DPD | ✅ | `pudo.pudoId` in shipment payload |
| Hermes | ✅ | HSI routing API |
| FedEx | ✅ | `receiver.servicePointId` → `HOLD_AT_LOCATION` + `holdAtLocationDetail.locationId` (Hold at Location) |
| InPost | ✅ | `receiver.servicePointId` → `destination.pointId` (locker ID) |

---

## Developer experience

| Feature | Status | Notes |
|---|---|---|
| Built-in docs (`GET /docs`) | ✅ | Endpoint index + freight terminology glossary. No external docs required. |
| Per-endpoint docs (`GET /docs/{slug}`) | ✅ | Full description, field list, example payload, and curl command. Slugs match endpoint names, e.g. `/docs/bookings`. |
| Terminology glossary (`GET /docs/terminology`) | ✅ | 16 freight terms (COD, POD, AWB, Incoterms, HS Code, IOSS, de minimis, VAS, etc.) with one-line and extended definitions. |
| Plain-text terminal output | ✅ | Send `Accept: text/plain` for curl-help-style output — term names left-aligned, definitions inline. |
| Decoupled payloads | ✅ | Example JSON bodies are shown separately from their curl commands so they can be copied directly into any HTTP client. |

---

## Priority gaps

Issues that affect production operations and should be addressed next.

1. **GLS manifest (end-of-day close)** — `POST /rs/shipments/endofday` must be
   wired before GLS can be used in production. Without it the driver will not
   collect the parcels.

2. **GLS pickup (sporadic collection)** — `POST /rs/sporadicollection` exists
   in the API but is not wired.

3. **FedEx label reprint** — `FetchLabel` returns `ErrNotSupported`. Labels
   must be stored from the booking response. Spec review pending.

4. **FedEx COD** — `shipmentCODDetail` is wired but FedEx only supports COD on
   Ground services. Express shipments with `AddOnCashOnDelivery` will be
   rejected by FedEx at the API level.

5. **DHL Express standalone pickup booking** — `POST /api/pickups` for DHL
   Express is not wired. Pickup is currently triggered only via the booking
   call.

6. **InPost go-live** — set `INPOST_CLIENT_ID`, `INPOST_CLIENT_SECRET`, `INPOST_ORG_ID`
   and run integration tests against `stage-api.inpost-group.com` before switching to
   production. Adapter is fully implemented.

7. **DPD tracking** — `GET /shipments/{id}` returns a numeric internal status
   code only. Full event tracking requires the separate DPD Tracking API
   (separate credentials).

8. **DHL eCommerce UK — cancellation postal code** — `CancelShipment` requires
   the consignee postal code alongside the shipment ID, but the `CarrierAdapter`
   interface only exposes the tracking number. The adapter caches the postal code
   at booking time; shipments booked in a different process or after a restart
   cannot be cancelled via the API. Resolve by storing the postal code externally
   (e.g. in the order database) and injecting it, or by extending the interface.

9. **DHL eCommerce UK — Windsor Framework (GB → NI)** — shipments from Great
   Britain to Northern Ireland require a `clearanceDeclaration` block
   (`C2C`/`C2B`/`B2C`/`B2B` Green/Red Lane). No `Customs` fields map to this
   yet. Must be added before routing GB→NI lanes through this adapter.

10. **DHL eCommerce UK — amendment** — `UpdateShipment` returns 501. The
    `/shipping/v1/amendment` endpoint supports post-booking address and weight
    changes but its schema is incompatible with `UpdateRequest`. Wire when the
    interface is extended.
