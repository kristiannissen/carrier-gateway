# Carrier Coverage

Carriers organised by region and country. Where a carrier operates across
multiple countries under a shared API (GLS, DHL, DPD, FedEx) it is listed under
its primary market and noted as multi-country.

The gateway's focus is **North-West Europe (NWE)** and **Central & Eastern
Europe (CEE)**.

Status column reflects the current state of the gateway implementation:

| Status | Meaning |
|---|---|
| Implemented | Core booking, tracking, and labels working in production |
| Partial | Adapter exists; one or more core operations return 501 |
| Not implemented | No adapter or adapter is a stub only |

---

## Denmark

| Carrier | Key | Status | Notes |
|---|---|---|---|
| PostNord | `postnord` | Partial | Covers DK, SE, NO, FI under a single API key. BookShipment, CancelShipment, UpdateShipment (phone/email only, SE-only and now reuses `CarrierMessageID` from the original booking as PostNord's docs require), TrackShipment, FetchLabel implemented. BookPickup is not wired despite `/v3/pickups/ids` existing in the API — a genuine gap, not a carrier limitation. |
| GLS Denmark | `gls` | Production | ShipIT API covers most of Europe — see GLS under Multi-country. |
| DAO | `dao` | Implemented | Denmark-only parcel network. Strong home delivery coverage. |
| DHL eCommerce Europe | `dhl_ecommerce` | Partial | BookShipment, TrackShipment, FetchLabel implemented. Cancel and update not supported via API — contact DHL customer service. |
| DHL Express | `dhl_express` | Partial | BookShipment, TrackShipment, FetchLabel implemented. CancelShipment and UpdateShipment not supported via API. |

---

## Norway

| Carrier | Key | Status | Notes |
|---|---|---|---|
| Bring (Posten Norge) | `bring` | Implemented | Norwegian post. API via Mybring. Covers NO, SE, DK, FI. |

---

## Sweden

| Carrier | Key | Status | Notes |
|---|---|---|---|
| PostNord Sweden | `postnord` | Partial | Same API and key as PostNord Denmark. |

---

## Finland

| Carrier | Key | Status | Notes |
|---|---|---|---|
| Posti | — | Not implemented | Finnish national postal carrier. |
| Matkahuolto | `matkahuolto` | Partial | Finnish parcel and bus courier. BookShipment, TrackShipment, FetchLabel, CancelShipment, and returns implemented. UpdateShipment not supported (returns 501). Label is bundled in the booking response — no separate fetch endpoint. See [matkahuolto-feature-mapping.md](matkahuolto-feature-mapping.md). |

---

## Germany

| Carrier | Key | Status | Notes |
|---|---|---|---|
| DHL Parcel DE | `dhl_parcel_de` | Partial | Pickup scheduling only (BookPickup, UpdatePickup, CancelPickup implemented). Shipment booking, tracking, and labels not yet implemented. |
| DHL eCommerce Europe | `dhl_ecommerce` | Partial | See Denmark entry. |
| Hermes Germany (HSI) | `hermes` | Partial | BookShipment, TrackShipment, FetchLabel, and returns implemented. CancelShipment and UpdateShipment are genuine carrier limitations, not implementation gaps — not supported via HSI API. No public documentation — integration based on directly obtained API specs. |
| DPD Germany | `dpd_de` | Not implemented | See DPD under Multi-country. Adapter registered dynamically via `DPD_DE_API_TOKEN`. |

---

## United Kingdom

| Carrier | Key | Status | Notes |
|---|---|---|---|
| DHL eCommerce UK | `dhl_ecommerce_uk` | Partial | BookShipment, TrackShipment, FetchLabel, CancelShipment, BookPickup implemented. UpdateShipment not supported via API. Separate product and adapter from DHL eCommerce Europe — uses `api.dhl.com/parceluk`. |
| DPD UK | `dpd_uk` | Beta | BookShipment and FetchLabel implemented. TrackShipment, CancelShipment, and UpdateShipment return 501 — code comments describe these endpoints as "not yet confirmed" against the real API rather than confirmed absent, and no `APIdocs/` file exists for `api.dpd.co.uk`, so these primary-method gaps count as genuine implementation gaps (unresearched), not verified carrier limitations. Stays Beta until researched. Separate UK entity from DPD mainland Europe. |
| Evri (formerly Hermes UK) | `evri` | Partial | BookShipment and FetchLabel implemented. TrackShipment, CancelShipment, and UpdateShipment are confirmed carrier limitations — not offered by the Evri Classic API. No relation to Hermes Germany (HSI). |
| Royal Mail | — | Not implemented | Dominant for C2C and lightweight B2C. Click and Drop API available. |

---

## Netherlands

| Carrier | Key | Status | Notes |
|---|---|---|---|
| GLS Netherlands | `gls_nl` | Production | Regional API (api-portal.gls.nl). BookShipment, CancelShipment, BookPickup, CancelPickup, and CloseManifest implemented. TrackShipment, FetchLabel (reprint), and UpdateShipment are confirmed carrier limitations — not offered by this regional portal API (the label is returned inline at booking instead of via reprint; tracking is only available via the unified GLS Group adapter). No genuine implementation gaps remain. Distinct from the unified GLS Group adapter (`gls`). |
| PostNL | `postnl` | Partial | BookShipment, TrackShipment, FetchLabel, BookReturn, and FetchReturnLabel implemented. CancelShipment and UpdateShipment return 501 (not supported by PostNL PNP v4 API). Supports domestic NL parcel, letterbox, packet, and letter product types. EU and non-EU international shipments via `internationalShipmentData.bundle`. Add-ons: insurance, stated-address-only, return-when-not-home, signature, delivery-code, age-check (16+/18+), evening delivery, guaranteed-before, dangerous goods LQ. Returns via `/shipment/delivery/v4/return/generate` (NL domestic only). Credentials: `POSTNL_API_KEY`, `POSTNL_CUSTOMER_NUMBER`, `POSTNL_CUSTOMER_CODE`. |
| DPD Netherlands | `dpd_nl` | Production | Own SOAP API (`internal/adapter/dpd_nl.go`), not the Baltic/multi-country DPD adapter — this row previously said "Not implemented" in error. BookShipment, CancelShipment (returns 501 as a confirmed API limitation), UpdateShipment (confirmed limitation), TrackShipment, and FetchLabel (served from an in-process cache populated at booking) are all implemented or genuinely unsupported by the API. No pickup/manifest endpoints exist in ShipmentService v3.5 (limitation, not a gap). Operational caveat: SOAP endpoint URLs are hardcoded defaults, not yet cross-checked against the live WSDLs — verify before go-live. |

---

## Belgium

| Carrier | Key | Status | Notes |
|---|---|---|---|
| bpost | — | Not implemented | Belgian national postal carrier. Also operates as bpost International for cross-border. |
| DPD Belgium | `dpd_be` | Not implemented | See DPD under Multi-country. |

---

## France

| Carrier | Key | Status | Notes |
|---|---|---|---|
| La Poste / Colissimo | — | Not implemented | National postal carrier. Colissimo is the parcel product. API via developer.laposte.fr. |
| DPD France | `dpd_fr` | Not implemented | See DPD under Multi-country. |
| Mondial Relay | — | Not implemented | Pickup point / parcel locker network with 30,000+ points across FR, ES, BE, PT, LU. Pickup point delivery only — not a home delivery carrier. |

---

## Austria

| Carrier | Key | Status | Notes |
|---|---|---|---|
| Österreichische Post | — | Not implemented | Austrian national postal carrier. |
| DPD Austria | `dpd_at` | Not implemented | See DPD under Multi-country. |
| DHL Austria | — | Not implemented | Shares developer.dhl.com API with other DHL markets. |

---

## Poland

| Carrier | Key | Status | Notes |
|---|---|---|---|
| InPost | `inpost` | Implemented | Parcel locker network. Shipping, labels, tracking, pickups (PL only), and returns (PL/IT/GB) implemented. Pickup query endpoints (GetPickupByID, ListPickups, GetCutoffTime) and return query endpoint (GetReturnShipment) also implemented. OAuth 2.1. CancelShipment and UpdateShipment return 501 (not supported by the InPost API). |
| DPD Poland | `dpd_pl` | Not implemented | See DPD under Multi-country. |
| Poczta Polska | — | Not implemented | Polish national postal carrier. No public API documentation confirmed. |

---

## Baltic states

| Carrier | Key | Status | Notes |
|---|---|---|---|
| Omniva | `omniva` | Implemented | Estonian post. Parcel locker network covering EE, LV, LT. BookShipment, TrackShipment, FetchLabel, CancelShipment, UpdateShipment, BookPickup, CancelPickup, and GetPickupAvailability implemented. Post-delivery return flow (`/shipments/omniva-return`) also implemented as `BookReturn`. See `docs/omniva-fitgap.md` for remaining gaps. |
| DPD Baltic | `dpd_lt` / `dpd_lv` / `dpd_ee` | Implemented | BookShipment, TrackShipment, FetchLabel, BookPickup implemented. CancelShipment, UpdateShipment, UpdatePickup, CancelPickup return 501. Registered per country via `DPD_{COUNTRY}_API_TOKEN`. |

---

## Czech Republic & Slovakia

| Carrier | Key | Status | Notes |
|---|---|---|---|
| GLS Czech / Slovak | `gls` | Implemented | Covered by the multi-country GLS ShipIT adapter. |
| DPD Czech Republic | `dpd_cz` | Not implemented | See DPD under Multi-country. |
| DPD Slovakia | `dpd_sk` | Not implemented | See DPD under Multi-country. |
| Zásilkovna / Packeta | — | Not implemented | Major CEE parcel and pickup-point network. Covers CZ, SK, PL, HU, RO and more. Widely used for cross-border CEE e-commerce. REST API well-documented. |

---

## Hungary

| Carrier | Key | Status | Notes |
|---|---|---|---|
| GLS Hungary | `gls` | Implemented | Covered by the multi-country GLS ShipIT adapter. |
| DPD Hungary | `dpd_hu` | Not implemented | See DPD under Multi-country. |
| Magyar Posta | — | Not implemented | Hungarian national postal carrier. API availability unclear. |

---

## Romania

| Carrier | Key | Status | Notes |
|---|---|---|---|
| GLS Romania | `gls` | Implemented | Covered by the multi-country GLS ShipIT adapter. |
| Cargus | — | Not implemented | Major Romanian courier, owned by DPD Group. Operates independently in RO. REST API available. |
| FAN Courier | — | Not implemented | Largest independent Romanian courier. No official public API — integration requires partnership agreement. |

---

## Balkans (Croatia, Slovenia, Bulgaria)

| Carrier | Key | Status | Notes |
|---|---|---|---|
| GLS Croatia / Slovenia | `gls` | Implemented | Covered by the multi-country GLS ShipIT adapter. |
| DPD Croatia / Slovenia | `dpd_hr` / `dpd_si` | Not implemented | See DPD under Multi-country. |
| Econt | `econt` | Production | BookShipment, CancelShipment, UpdateShipment, TrackShipment, FetchLabel (PDF), BookPickup, and GetPickupByID implemented. UpdatePickup, CancelPickup, CloseManifest, GetPickupAvailability, ListPickups, and GetCutoffTime return 501 (no such endpoints exist on the Econt API). No return endpoint. Basic Auth via `ECONT_USERNAME` / `ECONT_PASSWORD`. Covers BG and cross-border via Econt Express. |
| Speedy | `speedy` | Production | BookShipment, CancelShipment, TrackShipment, FetchLabel (PDF + ZPL), BookPickup, BookReturn, FetchReturnLabel, and GetReturnShipment implemented. UpdateShipment (a primary method) is now implemented as a partial update via `POST /shipment/update/properties` (`APIdocs/speedy_api.md` §2.1.7 / `speedy_api.rtf` — property key names are inferred from Speedy's own field paths and not yet confirmed against the sandbox). UpdatePickup, CancelPickup, CloseManifest, GetPickupByID, and ListPickups return 501 and are confirmed carrier limitations (secondary, don't block Production). Credentials embedded in request body (`SPEEDY_USERNAME` / `SPEEDY_PASSWORD`). Covers BG, RO, and neighbouring Balkan markets. |

---

## Spain & Portugal

| Carrier | Key | Status | Notes |
|---|---|---|---|
| SEUR | — | Not implemented | Major Spanish express carrier, owned by DPD Group. Operates independently in ES — not covered by the DPD multi-country adapter. |
| Correos Express | — | Not implemented | Express subsidiary of Correos. More e-commerce oriented than Correos itself. |
| DPD Portugal | `dpd_pt` | Not implemented | See DPD under Multi-country. |
| CTT (Correios de Portugal) | — | Not implemented | Portuguese national postal carrier. |
| Mondial Relay Iberia | — | Not implemented | See Mondial Relay under France. |

---

## Italy

| Carrier | Key | Status | Notes |
|---|---|---|---|
| GLS Italy | `gls` | Implemented | Covered by the multi-country GLS ShipIT adapter. |
| BRT (Bartolini) | — | Not implemented | Major Italian courier, owned by DPD Group. Operates independently in IT. |
| Poste Italiane | — | Not implemented | Italian national postal carrier. API documentation limited. |

---

## Multi-country carriers

Carriers with a single API covering multiple European markets.

| Carrier | Coverage | Key | Status | Notes |
|---|---|---|---|---|
| GLS | DE, DK, SE, NL, BE, FR, ES, PT, IT, AT, IE, HR, SI, SK, CZ, HU and more | `gls` | Production | ShipIT API v1. Single credentials, country selected by address. UpdateShipment is a confirmed carrier limitation (no update/modify/amend endpoint in the ShipIT v1 spec). Pickup scheduling (`sporadiccollection`, via `BookPickup`) and manifest close (`endofday`, via `CloseManifest`, operationally required) are now wired. `UpdatePickup`, `CancelPickup`, and `GetPickupAvailability` are confirmed carrier limitations — no such operations exist in the ShipIT API spec. No genuine implementation gaps remain. |
| DPD | LT, LV, EE, DE, FR, NL, BE, AT, PL, CZ, SK, HU, RO, BG, HR, SI and more | `dpd_{country}` | Partial | BookShipment and BookPickup implemented for Baltic markets. Other countries registered dynamically via `DPD_{COUNTRY}_API_TOKEN` env vars — adapter code works but individual country tokens must be configured. DPD UK, SEUR (ES), Cargus (RO), and BRT (IT) are separate entities within DPD Group and need distinct adapters. |
| DHL eCommerce Europe | 28 European countries | `dhl_ecommerce` | Partial | eConnect API. BookShipment, TrackShipment, FetchLabel implemented. Cancel and update are confirmed carrier limitations (no such endpoints in eConnect). Remaining secondary gaps: pickup scheduling and manifest close status unconfirmed. |
| DHL Express | Worldwide | `dhl_express` | Partial | MyDHL API. BookShipment, TrackShipment, FetchLabel implemented. CancelShipment and UpdateShipment are confirmed carrier limitations. Update/cancel pickup exist in the MyDHL API (`PATCH /pickups`, `DELETE /pickups/{id}`) but are not wired — a genuine secondary gap; the feature-mapping doc previously claimed these were done in error. |
| FedEx | Worldwide | `fedex` | Partial | FedEx Ship API v1. BookShipment, BookPickup, CancelPickup, CloseManifest, GetPickupAvailability implemented. FetchLabel reprint is a genuine secondary implementation gap (endpoint exists, spec review still pending). UpdateShipment is a confirmed carrier limitation. |
| InPost | PL (shipping + pickups + returns), IT + GB (returns) | `inpost` | Implemented | InPost Group API 2025. BookShipment, FetchLabel, TrackShipment, BookPickup (PL), CancelPickup (PL), GetPickupByID, ListPickups, GetCutoffTime, BookReturn (PL/IT/GB), FetchReturnLabel, GetReturnShipment implemented. OAuth 2.1. CancelShipment and UpdateShipment return 501 (not supported by the API). |

---

## Last-mile and same-day carriers

Last-mile and same-day delivery carriers — including Airmee, Instabee, and
others operating in this space — are under consideration. These carriers
operate differently from traditional parcel networks and may require a
distinct integration approach. No timeline has been set.
