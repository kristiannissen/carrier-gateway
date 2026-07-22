# Speedy Adapter

Speedy (`https://api.speedy.bg/v1`) is a Bulgarian courier operating in SE Europe. This adapter implements five gateway interfaces.

## Implemented interfaces

| Interface | Methods | Notes |
|---|---|---|
| `CarrierAdapter` | `BookShipment`, `TrackShipment`, `FetchLabel`, `CancelShipment`, `UpdateShipment` | All supported. `UpdateShipment` is a partial update (see below) |
| `ManifestAdapter` | `BookPickup` | UpdatePickup / CancelPickup / CloseManifest / GetPickupAvailability → `ErrNotSupported` |
| `PickupQuerier` | `GetCutoffTime` | GetPickupByID / ListPickups → `ErrNotSupported` |
| `ReturnAdapter` | `BookReturn`, `FetchReturnLabel` | Return booked as a standard outbound shipment |
| `ReturnQuerier` | `GetReturnShipment` | Calls `POST /shipment/{id}/secondary` |

## Configuration

| Env var | Required | Description |
|---|---|---|
| `SPEEDY_USERNAME` | Yes | API username |
| `SPEEDY_PASSWORD` | Yes | API password |
| `SPEEDY_SERVICE_ID` | No | Default courier service code (integer). Defaults to `505` (standard). |

If `SPEEDY_USERNAME` or `SPEEDY_PASSWORD` is unset, the adapter falls back to `MockSpeedyAdapter`.

## Authentication

Speedy uses HTTP Basic-style credentials embedded in every JSON request body — no OAuth token flow is needed. The `userName` and `password` fields are sent on every call.

## API endpoints used

| Gateway method | Speedy endpoint |
|---|---|
| `BookShipment` | `POST /shipment` |
| `CancelShipment` | `POST /shipment/cancel` |
| `UpdateShipment` | `POST /shipment/update/properties` (partial update only) |
| `TrackShipment` | `POST /track` |
| `FetchLabel` / `FetchReturnLabel` | `POST /print` |
| `BookPickup` | `POST /pickup` |
| `GetCutoffTime` | `POST /pickup/terms` |
| `GetReturnShipment` | `POST /shipment/{id}/secondary` |

## Label formats

| Format | Speedy value | Paper size |
|---|---|---|
| `PDF` | `pdf` | `A6` |
| `ZPL` / `ZPLGK` | `zpl` | `A6` |

PNG and EPL are not supported.

## Address handling

The adapter maps gateway `Address` structs to Speedy's **type 2 (foreign) address format** (`addressLine1` + `siteName` + `postCode`). This covers all countries supported by Speedy. For BG/RO shipments requiring the full Speedy type 1 nomenclature (street/complex IDs), extend `speedyAddr()` in `speedy.go`.

## Tracking status codes

Operation codes from `TrackedParcel.operations[].operationCode` are mapped in `status.go` under the `"speedy"` key. The mapping covers the most common codes; expand it from Speedy's Appendix 1 (`api.support@speedy.bg`) when observed in production.

## Post-booking updates

`UpdateShipment` uses Speedy's partial-update endpoint,
`POST /shipment/update/properties`, rather than the full-replace
`POST /shipment/update` — the full form requires resending the entire
original shipment (recipient, service, content, payment), which this
stateless gateway does not retain. The partial form only needs an `id` and a
`properties` key-value map.

Currently mapped fields: `ReceiverPhone` → `recipient.phone1.number`,
`ReceiverEmail` → `recipient.email`, `Weight` → `content.totalWeight`.
`ServicePointID` has no equivalent in the Speedy address model and is not
mapped. Speedy's docs describe the map's keys only as "matching
CreateShipmentRequest URL parameter names" without a worked example for these
fields — the dotted paths above are inferred from the JSON structure of
`speedyCreateRequest` and should be verified against the Speedy sandbox
before relying on them in production. Updates are only accepted before the
shipment has been requested for pickup or picked up.

## Gaps (not supported by the Speedy API)

- **UpdatePickup / CancelPickup** — no endpoint exists in the Speedy API.
- **CloseManifest** — no manifest close endpoint exists.
- **GetPickupAvailability** — use `GetCutoffTime` instead.
- **GetPickupByID / ListPickups** — no retrieve/list pickup endpoints exist.
