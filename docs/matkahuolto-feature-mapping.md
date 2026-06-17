# Matkahuolto Feature Mapping

Finnish parcel and bus courier. Operates an extensive pickup-point network across
Finland and offers home delivery, business delivery, and international services.

API documentation: shipment interface v2.24 (Feb 2025), tracking interface v1.3 (Jun 2024).

---

## Environment variables

| Variable | Required | Description |
|---|---|---|
| `MATKAHUOLTO_USER_ID` | Yes | Matkahuolto account number (without leading zeros) |
| `MATKAHUOLTO_PASSWORD` | Yes | Password for the account |
| `MATKAHUOLTO_SENDER_ID` | No | Sender account number; defaults to `MATKAHUOLTO_USER_ID` when empty |

Contact the Matkahuolto sales department to obtain credentials.

---

## Endpoints used

| Operation | Endpoint | Method | Auth |
|---|---|---|---|
| Book / cancel shipment | `https://extservices.matkahuolto.fi/mpaketti/mhshipmentxml` | POST | Basic Auth |
| Track shipment | `https://extservices.matkahuolto.fi/mpaketti/public/tracking` | GET | Basic Auth |

Test environment: replace `extservices` with `extservicestest` in the URLs above.
Set `BaseURL` and `TrackingURL` on the adapter struct directly in tests.

---

## Operations

| Operation | Status | Notes |
|---|---|---|
| `BookShipment` | ✅ Implemented | XML POST; returns tracking number + base64 PDF label |
| `TrackShipment` | ✅ Implemented | GET by shipment/parcel ID; events returned newest-first |
| `FetchLabel` | ✅ Implemented | Returns PDF cached at booking time; no separate label endpoint |
| `CancelShipment` | ✅ Implemented | MessageType=D; requires booking context cached at book time |
| `UpdateShipment` | ❌ Not supported | MessageType=C requires full original payload; returns 501 |
| `BookPickup` | ❌ Not available | Pickup is requested via `Pickup=Y` field at booking time |
| `CloseManifest` | ❌ Not available | No manifest endpoint in the Matkahuolto API |
| `BookReturn` | ✅ Implemented | Set `deliveryType: "return"` — uses ShipmentType=R + product 81/91 |

---

## Product code mapping

| `deliveryType` | Finland (FI) | International |
|---|---|---|
| `home` | 34 — Kotijakelu | 97 — Ulkomaan Kotijakelu |
| `business` | 30 — Jakopaketti | 96 — Ulkomaan Jakopaketti |
| `servicepoint` | 80 — Lähellä-paketti | 95 — Ulkomaan Lähellä-paketti |
| `return` | 81 — Asiakaspalautus | 91 — Ulkomaan Asiakaspalautus |
| _(empty)_ | 80 (default) | 95 (default) |

Products marked `*` in the Matkahuolto spec (30, 58, 81, 91, 96) are not intended to
be visible to consumers. Map `"business"` and `"return"` accordingly.

---

## Label handling

The Matkahuolto shipment API returns the PDF label base64-encoded inside the
`<ShipmentPdf>` XML element of the booking response. There is no separate
label-fetch endpoint.

The adapter caches the label in memory keyed by tracking number and returns it
from `FetchLabel`. **Callers should persist the label from the booking response**
(available in `colli[0].labelUrl`), as the cache is process-local and does not
survive restarts.

Only `PDF` format is supported. Requests for `ZPL`, `EPL2`, or `DPL` return 501.

---

## Tracking event codes

| Code | Description | Normalised status |
|---|---|---|
| 02 | Electronic advance notice received | `booked` |
| 08 | Picked up from sender | `picked_up` |
| 10 | Received at departing parcel point | `picked_up` |
| 12 | Consolidated | `in_transit` |
| 15 | Received for carriage | `in_transit` |
| 25 | Loaded for main transport | `in_transit` |
| 35 | Received at destination terminal | `in_transit` |
| 40 | Waiting to be loaded for delivery | `in_transit` |
| 41 | Waiting to be loaded for parcel point | `in_transit` |
| 45 | Loaded for delivery | `out_for_delivery` |
| 46 | Loaded for delivery to parcel point | `out_for_delivery` |
| 47 | Delivered to parcel point | `in_transit` |
| 48 | Received at parcel point | `in_transit` |
| 50 | Ready to be collected | `out_for_delivery` |
| 55 | First arrival notification sent | `in_transit` |
| 56 | Second arrival notification sent | `in_transit` |
| 57 | Manual arrival notification sent | `in_transit` |
| 60 | Handed over to the recipient | `delivered` |
| 61 | Handed over to the proxy | `delivered` |
| 62 | Handover cancelled | `failed` |
| 65 | COD paid to sender | `delivered` |
| 70 | Returned uncollected | `returned` |
| 97 | Delivery attempt unsuccessful | `failed` |
| 104 | Deviation added | `delayed` |

---

## Known limitations

- **UpdateShipment**: The change message (MessageType=C) requires resubmitting the
  complete original payload. The stateless gateway does not cache all booking fields,
  so this operation returns `501 Not Implemented`. Cancel and rebook to change details.

- **CancelShipment**: Requires booking context cached in process memory. Shipments
  booked outside this adapter instance (e.g. directly via the Matkahuolto portal or
  a previous process restart) cannot be cancelled via the API.

- **FetchLabel**: Same process-local cache constraint as cancellation. Persist labels
  from the booking response for long-term access.

- **Max 10 tracking IDs per request**: The tracking API enforces a hard limit of 10
  shipment/parcel IDs per GET request. The current adapter calls it one at a time
  (via `TrackShipment`), which is within limits.

- **Tracking by date range**: The tracking API supports `from`/`to` timestamp
  parameters in addition to `ids`. This is not exposed via the gateway's
  `TrackShipment` interface (which takes a single tracking number), but can be
  called directly if bulk polling is needed.
