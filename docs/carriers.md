# Carrier Coverage

Carriers organised by country. Where a carrier operates across multiple
countries under a shared API (GLS, DHL, DPD, UPS, FedEx) it is listed under
its primary market and noted as multi-country.

Status column reflects the current state of the gateway implementation:

| Status | Meaning |
|---|---|
| Implemented | Fully implemented and in production |
| Not fully implemented yet | Integration exists but incomplete |
| Not implemented yet | No implementation |

---

## Denmark

| Carrier | Status | Notes |
|---|---|---|
| PostNord | Implemented | Covers DK, SE, NO, FI under a single API key. |
| GLS Denmark | Implemented | GLS ShipIT API covers most of Europe — see GLS entry under multi-country. |
| DAO | Implemented | Denmark-only parcel network. Strong home delivery coverage. |
| DHL Express | Not fully implemented yet | Express international. Separate from DHL eCommerce. Returns not yet supported. |
| DHL eCommerce Europe | Not fully implemented yet | 28-country B2C parcel product. |

---

## Norway

| Carrier | Status | Notes |
|---|---|---|
| Bring (Posten Norge) | Implemented | Norwegian post. API via Mybring. Covers NO, SE, DK, FI. |
| DHL Norway | Not implemented yet | Shares developer.dhl.com API with other DHL markets. |

---

## Sweden

| Carrier | Status | Notes |
|---|---|---|
| PostNord Sweden | Implemented | Same API and key as PostNord Denmark. |
| DHL Sweden | Not implemented yet | Shares developer.dhl.com API with other DHL markets. |

---

## Finland

| Carrier | Status | Notes |
|---|---|---|
| Posti | Not implemented yet | Finnish national postal carrier. |
| DHL Finland | Not implemented yet | Shares developer.dhl.com API with other DHL markets. |
| Matkahuolto | Not implemented yet | Finnish parcel and bus courier. Significant pickup point network in Finland, relevant for servicepoint delivery. |

---

## Germany

| Carrier | Status | Notes |
|---|---|---|
| DHL Parcel DE | Not fully implemented yet | Domestic German parcel product (DHL Paket) — separate from DHL Express and DHL eCommerce Europe. Pickup scheduling implemented (`dhl_parcel_de.go`); shipment booking, tracking, and labels not yet implemented. |
| DPD Germany | Not implemented yet | See DPD under multi-country. |
| Hermes Germany (HSI) | Not fully implemented yet | German home delivery network. API via HSI. No public documentation — integration based on directly obtained API specs. |

---

## United Kingdom

| Carrier | Status | Notes |
|---|---|---|
| Royal Mail | Not implemented yet | Dominant for C2C and lightweight B2C. Click and Drop API available. |
| DPD UK | Not fully implemented yet (Beta) | Separate UK entity from DPD mainland Europe. Key: `dpd_uk`. |
| Evri (formerly Hermes UK) | Not fully implemented yet (Beta) | High-volume low-cost B2C. Key: `evri`. No relation to Hermes Germany (HSI). |

---

## France

| Carrier | Status | Notes |
|---|---|---|
| La Poste / Colissimo | Not implemented yet | National postal carrier. Colissimo is the parcel product. API via developer.laposte.fr. |
| Chronopost | Not implemented yet | Express subsidiary of La Poste. Next-day domestic and European. |
| DPD France | Not implemented yet | See DPD under multi-country. |
| Mondial Relay | Not implemented yet | Click and collect / parcel locker network with 30,000+ points across France, Spain, Belgium, Portugal and Luxembourg. Not a home delivery carrier — pickup point delivery only. Widely used for B2C e-commerce in Western Europe. |
| UPS France | Not implemented yet | See UPS under multi-country. |

---

## Netherlands

| Carrier | Status | Notes |
|---|---|---|
| PostNL | Not implemented yet | Dutch national postal carrier. Also handles cross-border European B2C parcel delivery from NL as an origin. REST API well-documented at developer.postnl.nl. |
| GLS Netherlands | Not fully implemented yet (Beta) | GLS NL regional API (api-portal.gls.nl). Username/password auth. Carrier key: `gls_nl`. Distinct from the unified GLS Group adapter (`gls`). Booking, cancel, pickup, and manifest supported; no tracking or label reprint. |
| DPD Netherlands | Not implemented yet | See DPD under multi-country. |

---

## Belgium

| Carrier | Status | Notes |
|---|---|---|
| bpost | Not implemented yet | Belgian national postal carrier. Also operates as bpost International for cross-border. |
| DPD Belgium | Not implemented yet | See DPD under multi-country. |

---

## Spain

| Carrier | Status | Notes |
|---|---|---|
| Correos | Not implemented yet | Spanish national postal carrier. Public API availability unclear — integration complexity likely high. |
| Correos Express | Not implemented yet | Express subsidiary of Correos. More e-commerce oriented than Correos itself and better documented. Preferable to Correos for B2C. |
| SEUR | Not implemented yet | Major Spanish express carrier, owned by DPD Group. Shares infrastructure with DPD but operates independently in Spain. |
| Mondial Relay Spain | Not implemented yet | See Mondial Relay under France. |

---

## Italy

| Carrier | Status | Notes |
|---|---|---|
| Poste Italiane | Not implemented yet | Italian national postal carrier. API via OpenAPI console — public documentation limited. |
| BRT (Bartolini) | Not implemented yet | Major Italian courier, owned by DPD Group. Operates independently in Italy. No public API documentation confirmed. |
| GLS Italy | Not implemented yet | See GLS under multi-country. |

---

## Austria

| Carrier | Status | Notes |
|---|---|---|
| Österreichische Post | Not implemented yet | Austrian national postal carrier. No public API documentation confirmed. |
| DHL Austria | Not implemented yet | Shares developer.dhl.com API with other DHL markets. |
| DPD Austria | Not implemented yet | See DPD under multi-country. |

---

## Poland

| Carrier | Status | Notes |
|---|---|---|
| InPost | Not fully implemented yet | Parcel locker network. Dominant in Poland, expanding into UK, France, Italy. ShipX API. Currently mock/demo only. |
| DHL Poland | Not implemented yet | Shares developer.dhl.com API with other DHL markets. |
| Poczta Polska | Not implemented yet | Polish national postal carrier. No public API documentation confirmed. |

---

## Portugal

| Carrier | Status | Notes |
|---|---|---|
| CTT (Correios de Portugal) | Not implemented yet | Portuguese national postal carrier. Unofficial API client exists on GitHub — no official documentation confirmed. |
| DPD Portugal | Not implemented yet | See DPD under multi-country. |
| Mondial Relay Portugal | Not implemented yet | See Mondial Relay under France. |

---

## Baltic states

| Carrier | Status | Notes |
|---|---|---|
| Omniva | Implemented | Estonian Post. Parcel locker network covering Estonia, Latvia and Lithuania. Relevant for Nordic businesses shipping to the Baltics. |

---

## Multi-country carriers

Carriers with a single API covering multiple European markets.

| Carrier | Coverage | Status | Notes |
|---|---|---|---|
| GLS | DE, DK, SE, NL, BE, FR, ES, PT, IT, AT, IE, HR, SI, SK, CZ, HU and more | Implemented | ShipIT API is consistent across all GLS countries. Single credentials, country selected by shipper/consignee address. |
| DHL Express | Worldwide | Not fully implemented yet | MyDHL API. Time-definite international. Separate product from DHL Paket and DHL eCommerce. Returns not yet supported. |
| DHL eCommerce Europe | 28 European countries | Not fully implemented yet | eConnect API. B2C parcel product for cross-border European delivery. |
| DPD | DE, FR, NL, BE, AT, PL, CZ, SK, HU, RO, BG, HR, SI, LT, LV, EE and more | Not fully implemented yet (Beta) | Registered dynamically per country via `DPD_{COUNTRY}_API_TOKEN` env vars (e.g. key `dpd_lt`). DPD UK, SEUR (ES) and BRT (IT) are separate entities within the DPD Group. |
| UPS | Worldwide | Not implemented yet | UPS Developer Kit. Global carrier. Consistent API across all markets. |
| FedEx | Worldwide | Not fully implemented yet | FedEx Ship API v1. Booking implemented; returns not yet supported. |
| InPost | PL, UK, FR, IT (expanding) | Not fully implemented yet | Parcel locker network. ShipX API. Currently mock only. |
| PostNL International | Europe-wide from NL/BE origin | Not implemented yet | Cross-border European parcel product. Relevant for shipments originating from the Netherlands or Belgium. |

---

## Last-mile and same-day carriers

Last-mile and same-day delivery carriers — including Airmee, Instabee, and
others operating in this space — are under consideration. These carriers
operate differently from traditional parcel networks and may require a
distinct integration approach. No timeline has been set.
