# Ufficio Postale (Poste Italiane) — Feature Mapping

API: **Ufficio Postale v1.2.2** via openapi.it  
Auth: Bearer token (`UFFICIOPOSTALE_API_KEY`)  
Coverage: Italy domestic (sender must be IT); select products support a limited international recipient list  
Implementation status: **Beta** (booking and tracking only)

---

## Summary

Ufficio Postale is a document-mailing service: the caller submits a letter body
(plain text, HTML, PDF URL, or base64 PDF) and Poste Italiane prints and posts
it on the sender's behalf. This makes it fundamentally different from parcel
carriers — there is no physical label, no parcel pickup, and no post-booking
update or cancellation.

`BookShipment` and `TrackShipment` are implemented. `FetchLabel`,
`CancelShipment`, and `UpdateShipment` all return `ErrNotSupported`.

---

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `UFFICIOPOSTALE_API_KEY` | Yes | Bearer token issued by openapi.it |
| `UFFICIOPOSTALE_SANDBOX` | No | Set to `true` to route requests to `test.ws.ufficiopostale.com` |

---

## Product selection

The postal product is chosen via `Shipment.ServiceTier`:

| `ServiceTier` | Product | Tracked |
|---|---|---|
| `""` or `"raccomandata"` | Raccomandata (default) | Yes — `NumeroRaccomandata` |
| `"raccomandata_smart"` | Raccomandata Smart | Yes |
| `"ordinaria"` | Posta Ordinaria | No |
| `"atti_giudiziari"` | Atti Giudiziari | Yes |

Posta Prioritaria (`/prioritarie/`) is deprecated since 01/05/2026 and is not exposed.

---

## Document content

The letter body that Poste Italiane will print is taken from `Shipment.ShipmentComment`.
Accepted values (as defined by the Ufficio Postale API):

- Plain text
- HTML (with `<style>` and header tags)
- A URL pointing to a webpage, PDF, or image (`http://…` or `https://…`)
- A base64-encoded PDF prefixed with `data:application/pdf;base64,`

If `ShipmentComment` is empty, the adapter falls back to concatenating
`Colli[].Items[].Description` values, then to the string `"Documento"`.
In production, always set `ShipmentComment` to the actual letter content.

---

## Address mapping

| Gateway field | Ufficio Postale field | Notes |
|---|---|---|
| `Address.Name` | `nome` + `cognome` or `ragione_sociale` | Split on first space; single token → `ragione_sociale` |
| `Address.Street` | `indirizzo` | Street name only |
| `Address.HouseNumber` | `civico` | Separate field |
| `Address.City` | `comune` | |
| `Address.PostalCode` | `cap` | 5-digit Italian CAP |
| `Address.State` | `provincia` | 2-letter Italian province code (e.g. `RM`, `MI`) |
| `Address.Country` | `nazione` | ISO 3166-1 alpha-2 |
| `Address.Email` | `email` | Optional |

---

## Feature fit/gap

### Booking

| Feature | Status | Notes |
|---|---|---|
| Book shipment | ✅ Implemented | POST to product endpoint; `autoconfirm: true` |
| Cancel shipment | ❌ Not available | No cancellation endpoint in the API |
| Update shipment | ❌ Not available | PATCH accepts only `confirmed` boolean |
| Idempotency key | ❌ Not native | No server-side deduplication key supported |
| Multi-colli | ❌ Not applicable | Letter service: one document per mailing |
| Return booking | ❌ Not applicable | Document-mailing service has no return concept |

### Labels

| Feature | Status | Notes |
|---|---|---|
| Fetch label | ❌ Not available | Poste Italiane prints and attaches the postal document internally |
| Label at booking | ❌ Not available | No label URL or data in any booking response |
| Accettazione receipt | ⚠️ Not wired | `GET /{product}/{id}/accettazione` returns a PDF postal receipt; not surfaced via `FetchLabel` because it is not a carrier label |

### Tracking

| Feature | Status | Notes |
|---|---|---|
| Current status | ✅ Implemented | `GET /tracking/{NumeroRaccomandata}` |
| Event history | ✅ Implemented | Timestamped events with Italian descriptions and type codes |
| Normalised status | ✅ Implemented | Type codes mapped to gateway `TrackingStatus` |
| Estimated delivery | ❌ Not available | No ETA field in the tracking response |
| Tracking for Ordinaria | ❌ Not available | Posta Ordinaria and bulk products have no tracking data |

### Status code mapping

| API type code | Italian description | Normalised status |
|---|---|---|
| `00` | Accettato Online | `booked` |
| `01` | Consegnato | `delivered` |
| `03` | Non Consegnabile | `failed` |
| `30` | In Giacenza | `failed` |
| `40` | Inesitato | `failed` |
| `91` | Mancata consegna per forza maggiore | `failed` |
| `93` | Accettato CAN / CAD | `in_transit` |
| `100` | Accettato Online | `booked` |
| `110` | Spedizione Stampata | `booked` |
| other | — | `unknown` |

---

## Known limitations

- **No label**: This is a managed-print postal service; callers never handle a physical label.
- **No cancellation**: Once a mailing is confirmed (`autoconfirm: true`), it cannot be voided via API.
- **No update**: Recipient address and document cannot be changed after creation.
- **Italy-domestic**: Most products restrict the sender to Italy. Cross-border support is limited and product-specific.
- **Document field**: `BookingRequest` has no dedicated document field; use `Shipment.ShipmentComment` for the letter body.
- **Province required**: Italian addresses should populate `Address.State` with the 2-letter province code (e.g. `RM` for Rome). Without it the API may reject the request.
