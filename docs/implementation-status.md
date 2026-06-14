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
| DAO | Implemented | DK only | [dao-feature-mapping.md](dao-feature-mapping.md) |
| DHL Express | Not fully implemented yet (Beta) | Worldwide | [dhl-express-feature-mapping.md](dhl-express-feature-mapping.md) |
| DHL eCommerce Europe | Not fully implemented yet (Beta) | 28 European countries | [dhl-ecommerce-feature-mapping.md](dhl-ecommerce-feature-mapping.md) |
| DPD | Not fully implemented yet (Beta) | Pan-European | [dpd-group-feature-mapping.md](dpd-group-feature-mapping.md) |
| DPD UK | Not fully implemented yet (Beta) | GB | — |
| Hermes Germany | Not fully implemented yet (Beta) | DE only | [hermes-feature-mapping.md](hermes-feature-mapping.md) |
| FedEx | Not fully implemented yet (Beta) | Worldwide | [fedex-feature-mapping.md](fedex-feature-mapping.md) |
| Evri | Not fully implemented yet (Beta) | GB | — |
| DHL eCommerce UK | Not fully implemented yet (Beta) | GB | [dhl-ecommerce-feature-mapping.md](dhl-ecommerce-feature-mapping.md) |
| Omniva | Implemented | EE, LV, LT | — |
| InPost | Not fully implemented yet (Demo) | PL, UK, FR, IT | [inpost-feature-mapping.md](inpost-feature-mapping.md) |

---

## Core adapter features

✅ = Implemented and live  ⚠️ = Partial or caveated  ❌ = Not supported or not wired  ❓ = Unknown / not confirmed

| Feature | PostNord | Bring | GLS | DAO | DHL Express | DHL eCom EU | DHL eCom UK | DPD | Hermes | FedEx | InPost |
|---|---|---|---|---|---|---|---|---|---|---|---|
| **Book shipment** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ Demo |
| **Cancel shipment** | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❌ | ✅ | ❌ |
| **Update shipment** | ⚠️ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Tracking + events** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ⚠️ Demo |
| **Labels** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ⚠️ Demo |
| **Return labels** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ |
| **Idempotency (native)** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

---

## Pickup scheduling

| Feature | PostNord | Bring | GLS | DAO | DHL Express | DHL eCom EU | DHL eCom UK | DPD | Hermes | FedEx | InPost |
|---|---|---|---|---|---|---|---|---|---|---|---|
| **Book pickup** | ⚠️ | ✅ | ❌ | ❌ | ⚠️ | ❓ | ✅ | ✅ | ❓ | ✅ | N/A |
| **Update pickup** | ❌ | ❌ | ❌ | ❌ | ✅ | ❓ | ❌ | ❌ | ❓ | ❌ | N/A |
| **Cancel pickup** | ❌ | ❌ | ❌ | ❌ | ✅ | ❓ | ❌ | ✅ | ❓ | ✅ | N/A |
| **Pickup availability** | ❌ | ❌ | ❌ | ❌ | ❌ | ❓ | ❌ | ❌ | ❓ | ✅ | N/A |

**PostNord pickup note:** Domestic DK/SE/FI only. Requires item IDs from booking response.
**DHL Express pickup note:** Implicit at booking (returns `dispatchConfirmationNumber`). Standalone `POST /api/pickups` not yet wired.
**GLS pickup note:** `POST /rs/sporadiccollection` exists in ShipIT API but not yet wired.
**FedEx pickup note:** Update not supported — cancel-and-rebook. Confirmation number is an opaque token encoding code + date + Express location.

---

## Manifest / end-of-day

| Carrier | Manifest close | Manifest document | Notes |
|---|---|---|---|
| PostNord | ❌ | ❌ | Handled by EDI scan at collection |
| Bring | ❌ | ❌ | No API support |
| GLS | ❌ | ❌ | **Required** — `POST /rs/shipments/endofday` exists but not wired. Must be called before driver arrives. |
| DAO | ❌ | ❌ | No API support |
| DHL Express | ❌ | ✅ | Post-collection only via `GET /shipments/{id}/get-image?typeCode=MANIFEST` |
| DHL eCommerce EU | ❓ | ❓ | Not confirmed |
| DHL eCommerce UK | ❌ | ❌ | No manifest API — shipments processed automatically by DHL |
| DPD | ❌ | ❌ | No API support — pickup order acts as handover instruction |
| Hermes | ❓ | ❓ | Not confirmed |
| FedEx | ❌ | ❌ | No API support |
| InPost | N/A | N/A | Locker network — no manifest |

---

## Add-ons

| Add-on | PostNord | Bring | GLS | DAO | DHL Express | DHL eCom EU | DHL eCom UK | DPD | Hermes | FedEx | InPost |
|---|---|---|---|---|---|---|---|---|---|---|---|
| SMS notification | ✅ | ✅ | ❌ | ⚠️ | ⚠️ | ❓ | ❌ | ✅ | ✅ | ❌ | ❌ |
| Email notification | ✅ | ✅ | ❌ | ⚠️ | ✅ | ❓ | ⚠️ | ✅ | ✅ | ❌ | ❌ |
| Flex delivery | ✅ | ✅ | ❌ | ❌ | ✅ | ⚠️ | ❌ | ❓ | ❌ | ❌ | ❌ |
| Signature required | ✅ | ✅ | ❌ | ❌ | ⚠️ | ⚠️ | ✅ | ❓ | ✅ | ❌ | ❌ |
| Cash on delivery | ❌ | ✅ | ❌ | ❌ | ⚠️ | ✅ | ❌ | ✅ | ✅ | ❌ | ❌ |
| Insurance | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ❓ | ❌ | ❌ | ❌ |

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
| FedEx | ❌ | Not yet wired — Ship API supports it |
| InPost | ❌ | Not wired |

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
| InPost | ✅ | `service.targetLocker` (locker code) |

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

4. **FedEx customs** — international FedEx shipments require customs data that
   the adapter does not yet map. Shipments from EU to non-EU destinations will
   be rejected or miss customs documentation.

5. **DHL Express standalone pickup booking** — `POST /api/pickups` for DHL
   Express is not wired. Pickup is currently triggered only via the booking
   call.

6. **InPost go-live** — remove `Demo: true` flag and run integration tests
   against ShipX staging.

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
