# Git commit message

```
feat(econt): add Econt Express carrier adapter (beta)

Implements the full CarrierAdapter interface for Econt Express — Bulgaria's
primary carrier — and wires it into the gateway as a beta carrier.

## What's included

### internal/adapter/econt.go
- EcontAdapter with Basic Auth round-tripper (ECONT_USERNAME / ECONT_PASSWORD)
- BookShipment: POST LabelService.createLabel.json with mode=create
- TrackShipment: POST ShipmentService.getShipmentStatuses.json; maps
  shortDeliveryStatusEn to normalizedStatus; converts trackingEvents to
  TrackingEvent using destinationType for fine-grained status normalization
- FetchLabel: two-step call — getShipmentStatuses to retrieve pdfURL, then
  HTTP GET the PDF; PDF only (no ZPL/EPL on Econt)
- CancelShipment: POST LabelService.deleteLabels.json; returns clear error
  when the shipment has already been accepted by Econt
- UpdateShipment: checkPossibleShipmentEditions → updateLabel; returns error
  when no editions are available (shipment in transit); supports phone, email,
  weight, servicePointId (→ receiverOfficeCode)
- Wire-format types kept package-private; all Econt JSON paths as constants

### internal/adapter/mock_econt.go
- MockEcontAdapter with deterministic canned responses for all five methods
- FetchLabel returns a minimal base64 PDF stub
- UpdateShipment echoes which fields were non-empty in UpdatedFields

### internal/adapter/econt_test.go
- httptest-based tests for all five live adapter methods including error paths
- Mock adapter tests covering format rejection and field echo
- Builder helper tests for ServicePointID routing, COD services, email-on-delivery

### internal/adapter/adapter.go
- "econt" capabilities entry: Beta=true, SupportsCancellation=true, SupportsUpdate=true
- InitAdapters block: ECONT_USERNAME + ECONT_PASSWORD guard; optional ECONT_BASE_URL
  override for test environment (https://demo.econt.com/ee/services)

### internal/adapter/status.go
- "econt" entry in normalizedStatuses keyed on shortDeliveryStatusEn values
  ("Prepared in eEcont" → booked, "Delivered" → delivered, etc.)

### internal/validation/package.go
- "econt" limits: 50 kg max weight, 200×200×180 cm (pack type envelope)

### internal/validation/carrier_customs.go
- "econt" customs rule entry (no item cap enforced pre-flight; TARIC codes
  validated server-side by Econt)

### internal/validation/restricted.go
- "econt" restricted goods: explosives/ammunition/weapons/flammables blocked;
  lithium batteries and perishables warned

### docs/econt-feature-mapping.md (previously committed)
- Fit/gap analysis, status mapping table, environment variables, implementation checklist

### README.md
- econt row added to the supported carriers table (Beta)

## Known constraints (documented in econt-feature-mapping.md)
- No manifest endpoint — CloseManifest not implemented (returns ErrNotSupported)
- No push webhooks from Econt — polling via getShipmentStatuses required
- Cancellation only possible before acceptance; post-acceptance state must be
  managed via the Econt portal
- Label fetch is two HTTP calls (getShipmentStatuses + PDF download); store
  BookingResponse.LabelURL to avoid the extra round-trip

## Environment variables
  ECONT_USERNAME   e-Econt username (required for production mode)
  ECONT_PASSWORD   e-Econt password (required for production mode)
  ECONT_BASE_URL   override base URL (optional; default https://ee.econt.com/services)
```
