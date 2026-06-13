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
| DAO | Not fully implemented yet (Beta) | DK only | [dao-feature-mapping.md](dao-feature-mapping.md) |
| DHL Express | Not fully implemented yet (Beta) | Worldwide | [dhl-express-feature-mapping.md](dhl-express-feature-mapping.md) |
| DHL eCommerce Europe | Not fully implemented yet (Beta) | 28 European countries | [dhl-ecommerce-feature-mapping.md](dhl-ecommerce-feature-mapping.md) |
| DPD | Not fully implemented yet (Beta) | Pan-European | [dpd-group-feature-mapping.md](dpd-group-feature-mapping.md) |
| Hermes Germany | Not fully implemented yet (Beta) | DE only | [hermes-feature-mapping.md](hermes-feature-mapping.md) |
| FedEx | Not fully implemented yet (Beta) | Worldwide | [fedex-feature-mapping.md](fedex-feature-mapping.md) |
| InPost | Not fully implemented yet (Demo) | PL, UK, FR, IT | [inpost-feature-mapping.md](inpost-feature-mapping.md) |

---

## Core adapter features

✅ = Implemented and live  ⚠️ = Partial or caveated  ❌ = Not supported or not wired  ❓ = Unknown / not confirmed

| Feature | PostNord | Bring | GLS | DAO | DHL Express | DHL eCom | DPD | Hermes | FedEx | InPost |
|---|---|---|---|---|---|---|---|---|---|---|
| **Book shipment** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ Demo |
| **Cancel shipment** | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ✅ | ❌ | ✅ | ❌ |
| **Update shipment** | ⚠️ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| **Tracking + events** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ✅ | ✅ | ⚠️ Demo |
| **Labels** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ⚠️ | ⚠️ Demo |
| **Return labels** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ |
| **Idempotency (native)** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

---

## Pickup scheduling

| Feature | PostNord | Bring | GLS | DAO | DHL Express | DHL eCom | DPD | Hermes | FedEx | InPost |
|---|---|---|---|---|---|---|---|---|---|---|
| **Book pickup** | ⚠️ | ✅ | ❌ | ❌ | ⚠️ | ❓ | ✅ | ❓ | ❌ | N/A |
| **Update pickup** | ❌ | ❌ | ❌ | ❌ | ✅ | ❓ | ❌ | ❓ | ❌ | N/A |
| **Cancel pickup** | ❌ | ❌ | ❌ | ❌ | ✅ | ❓ | ✅ | ❓ | ❌ | N/A |

**PostNord pickup note:** Domestic DK/SE/FI only. Requires item IDs from booking response.
**DHL Express pickup note:** Implicit at booking (returns `dispatchConfirmationNumber`). Standalone `POST /api/pickups` not yet wired.
**GLS pickup note:** `POST /rs/sporadiccollection` exists in ShipIT API but not yet wired.

---

## Manifest / end-of-day

| Carrier | Manifest close | Manifest document | Notes |
|---|---|---|---|
| PostNord | ❌ | ❌ | Handled by EDI scan at collection |
| Bring | ❌ | ❌ | No API support |
| GLS | ❌ | ❌ | **Required** — `POST /rs/shipments/endofday` exists but not wired. Must be called before driver arrives. |
| DAO | ❌ | ❌ | No API support |
| DHL Express | ❌ | ✅ | Post-collection only via `GET /shipments/{id}/get-image?typeCode=MANIFEST` |
| DHL eCommerce | ❓ | ❓ | Not confirmed |
| DPD | ❌ | ❌ | No API support — pickup order acts as handover instruction |
| Hermes | ❓ | ❓ | Not confirmed |
| FedEx | ❌ | ❌ | No API support |
| InPost | N/A | N/A | Locker network — no manifest |

---

## Add-ons

| Add-on | PostNord | Bring | GLS | DAO | DHL Express | DHL eCom | DPD | Hermes | FedEx | InPost |
|---|---|---|---|---|---|---|---|---|---|---|
| SMS notification | ✅ | ✅ | ❌ | ⚠️ | ⚠️ | ❓ | ✅ | ✅ | ❌ | ❌ |
| Email notification | ✅ | ✅ | ❌ | ⚠️ | ✅ | ❓ | ✅ | ✅ | ❌ | ❌ |
| Flex delivery | ✅ | ✅ | ❌ | ❌ | ✅ | ✅ | ❓ | ❌ | ❌ | ❌ |
| Signature required | ✅ | ✅ | ❌ | ❌ | ⚠️ | ✅ | ❓ | ✅ | ❌ | ❌ |
| Cash on delivery | ❌ | ✅ | ❌ | ❌ | ⚠️ | ✅ | ✅ | ✅ | ❌ | ❌ |
| Insurance | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ | ❓ | ❌ | ❌ | ❌ |

---

## Customs support

| Carrier | Customs | Notes |
|---|---|---|
| PostNord | ✅ | v3 EDI customs block — HS codes, EORI, VAT, Incoterms, line items |
| Bring | ✅ | NatureOfCargo, Incoterms, HS codes, EORI/VAT. Required for NO shipments from EU. |
| GLS | ✅ | customs-consignments-v3 API. CN22/CN23 generation. |
| DAO | ❌ | Denmark domestic only |
| DHL Express | ✅ | Full customs block — Incoterms, IOSS, EORI, invoice number/date, line items |
| DHL eCommerce | ✅ | Customs data block for cross-border |
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
| DAO | ✅ | `receiver.servicePointId` → `shopid` or `lockerId` |
| DHL Express | ✅ | `receiver.servicePointId` → `onDemandDelivery.servicePointId` (6-char code) |
| DHL eCommerce | ✅ | `deliveryType=parcelshop/parcelstation/postOffice` |
| DPD | ✅ | `pudo.pudoId` in shipment payload |
| Hermes | ✅ | HSI routing API |
| FedEx | ❌ | Not wired |
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
