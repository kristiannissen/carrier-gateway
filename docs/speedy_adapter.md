# Speedy Adapter

Speedy (`https://api.speedy.bg/v1`) is a Bulgarian courier operating in SE Europe. This adapter implements five gateway interfaces.

## Implemented interfaces

| Interface | Methods | Notes |
|---|---|---|
| `CarrierAdapter` | `BookShipment`, `TrackShipment`, `FetchLabel`, `CancelShipment` | All supported |
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

## Gaps (not supported by the Speedy API)

- **UpdatePickup / CancelPickup** — no endpoint exists in the Speedy API.
- **CloseManifest** — no manifest close endpoint exists.
- **GetPickupAvailability** — use `GetCutoffTime` instead.
- **GetPickupByID / ListPickups** — no retrieve/list pickup endpoints exist.
- **UpdateShipment** — use `/shipment/update` directly or the Speedy portal.
