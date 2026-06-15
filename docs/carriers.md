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
| PostNord | `postnord` | Implemented | Covers DK, SE, NO, FI under a single API key. |
| GLS Denmark | `gls` | Implemented | ShipIT API covers most of Europe — see GLS under Multi-country. |
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
| PostNord Sweden | `postnord` | Implemented | Same API and key as PostNord Denmark. |

---

## Finland

| Carrier | Key | Status | Notes |
|---|---|---|---|
| Posti | — | Not implemented | Finnish national postal carrier. |
| Matkahuolto | — | Not implemented | Finnish parcel and bus courier. Significant pickup point network in Finland — relevant for servicepoint delivery. |

---

## Germany

| Carrier | Key | Status | Notes |
|---|---|---|---|
| DHL Parcel DE | `dhl_parcel_de` | Partial | Pickup scheduling only (BookPickup, UpdatePickup, CancelPickup implemented). Shipment booking, tracking, and labels not yet implemented. |
| DHL eCommerce Europe | `dhl_ecommerce` | Partial | See Denmark entry. |
| Hermes Germany (HSI) | `hermes` | Partial | BookShipment, TrackShipment, FetchLabel, and returns implemented. CancelShipment and UpdateShipment not supported via HSI API. No public documentation — integration based on directly obtained API specs. |
| DPD Germany | `dpd_de` | Not implemented | See DPD under Multi-country. Adapter registered dynamically via `DPD_DE_API_TOKEN`. |

---

## United Kingdom

| Carrier | Key | Status | Notes |
|---|---|---|---|
| DHL eCommerce UK | `dhl_ecommerce_uk` | Partial | BookShipment, TrackShipment, FetchLabel, CancelShipment, BookPickup implemented. UpdateShipment not supported via API. Separate product and adapter from DHL eCommerce Europe — uses `api.dhl.com/parceluk`. |
| DPD UK | `dpd_uk` | Partial | BookShipment and FetchLabel implemented. TrackShipment, CancelShipment, and UpdateShipment return 501. Separate UK entity from DPD mainland Europe. |
| Evri (formerly Hermes UK) | `evri` | Partial | BookShipment and FetchLabel implemented. TrackShipment, CancelShipment, and UpdateShipment return 501. No relation to Hermes Germany (HSI). |
| Royal Mail | — | Not implemented | Dominant for C2C and lightweight B2C. Click and Drop API available. |

---

## Netherlands

| Carrier | Key | Status | Notes |
|---|---|---|---|
| GLS Netherlands | `gls_nl` | Partial | Regional API (api-portal.gls.nl). BookShipment, CancelShipment, BookPickup, CancelPickup, and CloseManifest implemented. TrackShipment, FetchLabel (reprint), and UpdateShipment return 501. Distinct from the unified GLS Group adapter (`gls`). |
| PostNL | — | Not implemented | Dutch national postal carrier. Also handles cross-border European B2C parcel delivery from NL as an origin. REST API at developer.postnl.nl. |
| DPD Netherlands | `dpd_nl` | Not implemented | See DPD under Multi-country. |

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
| InPost | `inpost` | Partial | Parcel locker network. BookShipment, FetchLabel, and TrackShipment implemented. CancelShipment and UpdateShipment return 501. Dominant in PL, expanding into UK, FR, IT. ShipX API. |
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
| Econt | — | Not implemented | Leading Bulgarian parcel carrier. Covers BG, RO, GR, SR, MK. REST API available. |
| Speedy | — | Not implemented | Second-largest Bulgarian courier. Covers BG and neighbouring Balkan markets. |

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
| GLS | DE, DK, SE, NL, BE, FR, ES, PT, IT, AT, IE, HR, SI, SK, CZ, HU and more | `gls` | Implemented | ShipIT API v1. Single credentials, country selected by address. Pickup scheduling not yet wired (`sporadiccollection`). Manifest close (`endofday`) not yet wired — operationally required. |
| DPD | LT, LV, EE, DE, FR, NL, BE, AT, PL, CZ, SK, HU, RO, BG, HR, SI and more | `dpd_{country}` | Partial | BookShipment and BookPickup implemented for Baltic markets. Other countries registered dynamically via `DPD_{COUNTRY}_API_TOKEN` env vars — adapter code works but individual country tokens must be configured. DPD UK, SEUR (ES), Cargus (RO), and BRT (IT) are separate entities within DPD Group and need distinct adapters. |
| DHL eCommerce Europe | 28 European countries | `dhl_ecommerce` | Partial | eConnect API. BookShipment, TrackShipment, FetchLabel implemented. Cancel and update not supported via API. |
| DHL Express | Worldwide | `dhl_express` | Partial | MyDHL API. BookShipment, TrackShipment, FetchLabel implemented. CancelShipment and UpdateShipment not supported via API. |
| FedEx | Worldwide | `fedex` | Partial | FedEx Ship API v1. BookShipment, BookPickup, CancelPickup, CloseManifest, GetPickupAvailability implemented. FetchLabel reprint pending (spec not yet available). UpdateShipment not supported. |
| InPost | PL, UK, FR, IT (expanding) | `inpost` | Partial | ShipX API. BookShipment, FetchLabel, TrackShipment implemented. CancelShipment and UpdateShipment return 501. |

---

## Last-mile and same-day carriers

Last-mile and same-day delivery carriers — including Airmee, Instabee, and
others operating in this space — are under consideration. These carriers
operate differently from traditional parcel networks and may require a
distinct integration approach. No timeline has been set.
