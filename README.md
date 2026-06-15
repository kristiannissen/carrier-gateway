# Carrier Gateway

## Why this exists

Ten years of involuntary proximity to freight will do things to a person.

Not ten years of choosing logistics as a craft — ten years of it being the unavoidable tax on running an e-commerce business. Parcels that needed to move, carriers that needed to be appeased, and a middleware provider sitting between you and the carriers that promised to make it simple.

It did not make it simple.

The pitch is always the same: one integration, all carriers, we handle the complexity. What you actually get is a proprietary abstraction layer with its own quirks, its own data model, its own versioning strategy, and a support organisation whose response times are calibrated for a world where your warehouse isn't waiting. Every carrier behaviour you need to understand you now understand twice — once as the carrier actually works, and once as the middleware interprets it. Bugs live in the gap between those two things, and when something breaks, you own the debug even though you own none of the code.

The bitter irony is that integrating directly with the carriers would have been straightforward by comparison. Carrier APIs are well-documented, stable, and mostly sensible. The complexity was never in the carriers. It was in the layer we were paying to protect us from them.

This project is what direct integration looks like when you do it properly. A single consistent API, adapters that absorb carrier-specific wire format details, and no middleware standing between your order management system and the carrier actually moving your parcel.

It is also an experiment in how software gets built. Almost the entire codebase was written by AI, specifically — working from design decisions and architectural direction provided by a human with strong opinions and hard-won context. The human provides the judgement. The AI executes. It turns out that combination produces software faster than either could alone, and the result is readable enough that the human can tell when the AI is wrong.

Whether it holds up is the interesting question.

---

## What is Carrier Gateway

A stateless Go microservice with the goal of providing a single consistent API for booking, tracking, and returning shipments across multiple Nordic and European carriers. Change the `carrier` field in your request — the rest of your integration stays the same.

```bash
# Book with PostNord today, switch to Bring tomorrow — same request shape
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{"carrier": "bring", "shipment": { ... }}'
```

---

## Supported carriers

| Key | Carrier | Status |
|---|---|---|
| `postnord` | PostNord (DK, SE, NO, FI) | Production |
| `bring` | Bring / Posten (NO, SE, DK, FI) | Production |
| `gls` | GLS (DE, DK, SE, NL, BE, FR, ES + more) | Production |
| `omniva` | Omniva (EE, LV, LT) | Production |
| `dao` | DAO (DK) | Beta |
| `dhl` | DHL eCommerce Europe (28 countries) | Beta |
| `dhl_express` | DHL Express (worldwide) | Beta |
| `dpd_uk` | DPD UK (GB) | Beta |
| `hermes` | Hermes Germany (DE) | Beta |
| `fedex` | FedEx (worldwide) | Beta |
| `evri` | Evri (GB) | Beta |
| `inpost` | InPost (PL, IT, GB) | Production |

Demo carriers return mock data and are not connected to any live API. DPD continental Europe is registered dynamically from `DPD_{COUNTRY}_API_TOKEN` env vars (e.g. `dpd_lt`, `dpd_at`). For full country coverage see [`docs/carriers.md`](docs/carriers.md). For a feature-by-feature breakdown across all carriers see [`docs/implementation-status.md`](docs/implementation-status.md).

---

## Design

### Strategy pattern via `CarrierAdapter`

Every carrier is a struct that implements a single five-method interface:

```go
type CarrierAdapter interface {
    BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error)
    TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error)
    FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error)
    CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error)
    UpdateShipment(ctx context.Context, req UpdateRequest) (*UpdateResponse, error)
}
```

The handler layer never imports a carrier package directly — it calls `registry.Select(carrier)` and gets back a `CarrierAdapter`. Adding a new carrier means implementing this interface and registering it; no other files change.

Unsupported operations return a typed `ErrNotSupported` error which the handler translates to `501 Not Implemented`. The caller gets a clear signal instead of an opaque error.

### Stateless by design

The gateway holds no database and no per-request state. Every call is self-contained. Webhook dispatch is fire-and-forget — the caller owns retry state. CN22/CN23 customs forms are generated on the fly. Idempotency keys are logged and forwarded to carriers that support them natively.

The only planned stateful feature — subscription-based tracking — is intentionally kept in a separate companion service so the gateway itself stays stateless. See [`docs/parcel-poller.md`](docs/parcel-poller.md).

### Mock-first development

Every carrier has a matching `mock_{carrier}.go` that satisfies `CarrierAdapter`. Mocks are selected in two ways:

- **`MOCK_MODE=true`** — all carriers use their mock adapter.
- **Missing credentials** — if a carrier's API key is absent and `MOCK_MODE` is not set, that carrier falls back to its mock adapter automatically.

The health endpoint reports which mode each carrier is running in.

### Normalised statuses

Each carrier returns its own status vocabulary. The gateway maps every raw string to a shared `TrackingStatus` type: `booked`, `picked_up`, `in_transit`, `out_for_delivery`, `delivered`, `failed`, `returned`, `delayed`, `unknown`. The raw carrier string is preserved in `originalStatus` for debugging. Callers should only branch on `normalizedStatus`.

### Webhook notifications

All outgoing webhooks are signed with HMAC-SHA256 (`X-Signature: sha256=<hex>`) and only dispatched to `https://` endpoints. Dispatch can be triggered at booking, on a tracking poll, or on demand via `POST /api/notifications`.

---

## Quick start

```bash
git clone https://github.com/kristiannissen/carrier-gateway.git
cd carrier-gateway
go mod download

# Start with all carriers in mock mode — no credentials needed
MOCK_MODE=true LOG_ENV=development go run ./cmd/api
```

The server starts on `http://localhost:8080`.

```bash
curl http://localhost:8080/api/health
```

---

## Environment variables

| Variable | Description | Default |
|---|---|---|
| `PORT` | HTTP server port | `8080` |
| `LOG_ENV` | `development` for console logging and debug payload dumps | — |
| `MOCK_MODE` | `true` to force all carriers to use mock adapters | `false` |

Carrier-specific credentials are documented in each carrier's feature mapping file under `docs/`. For carriers without a dedicated file: Omniva uses `OMNIVA_USERNAME`, `OMNIVA_PASSWORD`, `OMNIVA_CUSTOMER_CODE`, `OMNIVA_AGENT_ID`; Evri uses `EVRI_CLIENT_ID`, `EVRI_CLIENT_SECRET`; Speedy uses `SPEEDY_USERNAME`, `SPEEDY_PASSWORD`, and optionally `SPEEDY_SERVICE_ID` (default `505`).

When a carrier's credentials are absent and `MOCK_MODE` is not set, that carrier falls back to its mock adapter. The `GET /api/health` response shows which mode each carrier is running in.

---

## Docker

```bash
docker build -t carrier-gateway .
docker run -p 8080:8080 --env-file .env carrier-gateway

# Mock mode
docker run -p 8080:8080 -e MOCK_MODE=true -e LOG_ENV=development carrier-gateway
```

Carrier credentials for `.env` are listed in each carrier's feature mapping file under [`docs/`](docs/). The three global variables (`PORT`, `LOG_ENV`, `MOCK_MODE`) are the only ones needed to run in mock mode.

---

## API reference

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/bookings` | Book a shipment |
| `DELETE` | `/api/bookings/{trackingNumber}` | Cancel a shipment |
| `PATCH` | `/api/bookings/{trackingNumber}` | Update contact, weight, or service point |
| `GET` | `/api/trackings/{trackingNumber}` | Track a shipment |
| `POST` | `/api/trackings/{trackingNumber}` | Track and dispatch webhook on status change |
| `POST` | `/api/notifications` | Dispatch a webhook notification directly |
| `GET` | `/api/labels/{trackingNumber}` | Fetch a shipping label |
| `GET` | `/api/pickups/availability` | Check pickup availability for a carrier |
| `GET` | `/api/pickups/cutoff-time` | Same-day pickup cutoff time (InPost) |
| `POST` | `/api/pickups` | Book a pickup |
| `GET` | `/api/pickups` | List pickups, paged (InPost) |
| `GET` | `/api/pickups/{confirmationNumber}` | Get pickup by ID (InPost) |
| `PUT` | `/api/pickups/{confirmationNumber}` | Update a pickup |
| `DELETE` | `/api/pickups/{confirmationNumber}` | Cancel a pickup |
| `POST` | `/api/manifests` | Close end-of-day manifest |
| `POST` | `/api/returns` | Book a return |
| `GET` | `/api/returns/{id}` | Get return shipment info (InPost) |
| `GET` | `/api/health` | Health check — uptime, mock mode, per-carrier status |

Every response includes `X-Request-ID`. Pass it in your request to forward a correlation ID.

### POST /api/bookings

```bash
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "postnord",
    "shipment": {
      "sender": {
        "name": "Unisport Group",
        "street": "Industrivej",
        "houseNumber": "10",
        "city": "Copenhagen",
        "postalCode": "2300",
        "country": "DK",
        "phone": "+4512345678",
        "email": "logistics@unisport.dk"
      },
      "receiver": {
        "name": "Anna Svensson",
        "street": "Storgatan",
        "houseNumber": "1",
        "city": "Stockholm",
        "postalCode": "11122",
        "country": "SE",
        "phone": "+46701234567",
        "email": "anna@example.com"
      },
      "deliveryType": "home",
      "totalWeight": 2.5,
      "colli": [
        {
          "id": "box-001",
          "weight": 2.5,
          "dimensions": { "length": 30, "width": 20, "height": 10 },
          "items": [
            { "description": "Football boots", "weight": 0.8, "quantity": 1, "value": 129.95 }
          ]
        }
      ]
    },
    "idempotencyKey": "order-98765"
  }'
```

Response (domestic — optional fields omitted when empty):

```json
{
  "shipmentId": "1234567890",
  "trackingNumber": "00073215400599388772",
  "carrier": "postnord",
  "status": "booked",
  "colli": [
    {
      "id": "00073215400599388772",
      "trackingNumber": "00073215400599388772",
      "labelUrl": "JVBERi0xLj...",
      "status": "booked"
    }
  ]
}
```

For cross-border shipments with customs, warnings, or notifications the response additionally includes `addOnWarnings`, `customsWarnings`, `notificationsSent`, `notificationsFailed`, `cnFormType`, and `cnDocument` — all omitted when empty.

#### Key booking fields

| Field | Type | Notes |
|---|---|---|
| `carrier` | string | Required. `postnord`, `bring`, `gls`, `dao`, `dhl`, `fedex` |
| `shipment.sender` / `shipment.receiver` | object | `name`, `street`, `houseNumber`, `city`, `postalCode`, `country` required. `servicePointId` on receiver routes to a pickup point. |
| `shipment.totalWeight` | float | kg — must equal the sum of all colli weights |
| `shipment.colli` | array | At least one package with `id`, `weight`, and `items` |
| `shipment.deliveryType` | string | `home`, `business`, `servicepoint`, or `return` |
| `shipment.addOns` | array | `sms_notification`, `email_notification`, `flex_delivery`, `signature_required`, `cash_on_delivery`, `insurance` — see [`docs/implementation-status.md`](docs/implementation-status.md) for per-carrier support |
| `shipment.customs` | object | Required for non-EU destinations — see [Cross-border](#cross-border-shipments-and-customs) |
| `shipment.brand` | string | InPost: merchant brand name forwarded to the InPost API |
| `shipment.returnAddress` | object | InPost PL: override the return-to-sender address (same shape as `sender`) |
| `shipment.valueAddedServices` | array | InPost: `[{"id": "...", "value": "..."}]` — e.g. SMS notification, insurance |
| `notifications` | object | `webhookUrl`, `webhookSecret`, `events` — dispatches a `booked` event on success |
| `idempotencyKey` | string | Max 64 characters |

### GET /api/trackings/{trackingNumber}

```bash
curl "http://localhost:8080/api/trackings/00073215400599388772?carrier=postnord"
```

```json
{
  "shipmentId": "1234567890",
  "trackingNumber": "00073215400599388772",
  "carrier": "postnord",
  "status": "INFORMED",
  "normalizedStatus": "booked",
  "originalStatus": "INFORMED",
  "estimatedDelivery": "2026-06-12",
  "events": [
    {
      "timestamp": "2026-06-07T18:37:36",
      "status": "INFORMED",
      "normalizedStatus": "booked",
      "location": "Copenhagen, DK",
      "details": "Shipment registered"
    }
  ]
}
```

### POST /api/trackings/{trackingNumber}

Track and dispatch a webhook when the normalised status has changed.

```bash
curl -X POST "http://localhost:8080/api/trackings/00073215400599388772" \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "postnord",
    "previousStatus": "booked",
    "notifications": {
      "webhookUrl": "https://your-service.example.com/hooks/shipments",
      "webhookSecret": "my-secret",
      "events": ["delivered", "failed", "out_for_delivery"]
    }
  }'
```

`booked` and `unknown` statuses never trigger dispatch.

### POST /api/notifications

Stateless webhook dispatch — no booking or tracking call required. Always returns `200 OK`; check `notificationsFailed` for delivery failures.

```bash
curl -X POST http://localhost:8080/api/notifications \
  -H "Content-Type: application/json" \
  -d '{
    "trackingNumber": "00073215400599388772",
    "carrier": "postnord",
    "event": "delivered",
    "notifications": {
      "webhook": {
        "url": "https://your-service.example.com/hooks/shipments",
        "secret": "my-secret"
      }
    }
  }'
```

### DELETE /api/bookings/{trackingNumber}

```bash
curl -X DELETE "http://localhost:8080/api/bookings/00073215400599388772?carrier=postnord"
```

Per-carrier cancellation support is in [`docs/implementation-status.md`](docs/implementation-status.md).

### PATCH /api/bookings/{trackingNumber}

Updates contact details, weight, or service point after booking. Per-carrier support is in [`docs/implementation-status.md`](docs/implementation-status.md).

```bash
curl -X PATCH "http://localhost:8080/api/bookings/00057126960000003016?carrier=dao" \
  -H "Content-Type: application/json" \
  -d '{"phone": "+4587654321", "email": "new@example.com", "weight": 2.3}'
```

### GET /api/labels/{trackingNumber}

```bash
# Decode and save as PDF
curl -s "http://localhost:8080/api/labels/00073215400599388772?carrier=postnord" \
  | jq -r '.data' | base64 -d > label.pdf

# ZPL direct to thermal printer
curl -s "http://localhost:8080/api/labels/00073215400599388772?carrier=postnord&format=ZPL" \
  | jq -r '.data' | base64 -d > /dev/usb/lp0
```

Supported formats: `PDF` (all carriers), `ZPL` and `ZPLGK` (PostNord, GLS, InPost), `EPL2` (InPost PL domestic), `DPL` (InPost PL domestic, pilot — contact InPost Integrations team before use). FedEx label fetch is not yet implemented — store the label from the booking response.

### GET /api/health

```bash
curl http://localhost:8080/api/health
```

```json
{
  "status": "ok",
  "uptime": "3h22m10s",
  "mockMode": false,
  "carriers": {
    "postnord": "production",
    "bring": "production",
    "gls": "production",
    "omniva": "production",
    "dao": "beta",
    "dhl": "beta",
    "dhl_express": "beta",
    "dpd_uk": "beta",
    "hermes": "beta",
    "fedex": "beta",
    "evri": "beta",
    "inpost": "mock"
  }
}
```

---

## Cross-border shipments and customs

A `customs` block is required for all non-EU destinations (NO, GB, CH, US, CA, AU, JP, CN etc.). The gateway rejects bookings with a missing `customs` block when the destination is a known non-EU country.

```json
"customs": {
  "incoterms": "DDP",
  "transportMode": "road",
  "hsCode": "61091000",
  "countryOfOrigin": "CN",
  "customsValue": 500.0,
  "customsCurrency": "DKK",
  "importerOfRecord": "NO123456789",
  "importerVatNumber": "SE1234567890",
  "exporterVatNumber": "12345678",
  "shipmentType": "B2B"
}
```

For cross-border non-EU shipments the gateway auto-generates a CN22 (≤ €22) or CN23 form and returns it base64-encoded in `cnDocument`. VAT numbers are validated against format rules for DK, SE, FI, NO, DE, FR, NL, and PL, and checked against VIES (non-blocking). Sea-only Incoterms (`FOB`, `FAS`, `CFR`, `CIF`) are rejected when `transportMode` is `air`, `road`, or `rail`.

Per-carrier item limits and EORI/VAT requirements are in [`docs/implementation-status.md`](docs/implementation-status.md).

---

## Restricted goods

Item descriptions are checked against per-carrier and per-destination prohibited and restricted goods lists. Blocked items return `400`; restricted items proceed with a `customsWarnings` entry. Explosives and firearms are blocked for all carriers. Lithium batteries produce a warning on all carriers (UN3480/UN3481 labelling required).

---

## Returns

Set `deliveryType: "return"`. Sender is the customer returning the parcel; receiver is the merchant — addresses are not swapped automatically. For labelless return support and per-carrier mechanisms see [`docs/implementation-status.md`](docs/implementation-status.md).

---

## Idempotency

Pass `idempotencyKey` in the request body (max 64 characters). PostNord forwards it as `referenceNo` (type `CU`) for server-side deduplication. Evri uses `clientUID` natively. For all other carriers, deduplication is the caller's responsibility.

---

## Project structure

```
carrier-gateway/
├── cmd/api/main.go
├── internal/
│   ├── adapter/
│   │   ├── adapter.go          # CarrierAdapter interface, Registry, shared types
│   │   ├── addon.go            # Add-on helpers
│   │   ├── status.go           # Raw → normalizedStatus mapping per carrier
│   │   ├── customs.go          # Customs wire-format helpers
│   │   ├── postnord.go
│   │   ├── bring.go
│   │   ├── gls.go
│   │   ├── dao.go
│   │   ├── dhl.go
│   │   ├── fedex.go
│   │   ├── posti.go            # Demo — mock only
│   │   ├── inpost.go           # InPost Group API 2025 (PL/IT/GB)
│   │   ├── inpost_pickups.go   # Pickup booking + query methods
│   │   ├── inpost_returns.go   # Return booking + query methods
│   │   ├── mock_*.go
│   │   └── *_test.go
│   ├── customs/
│   │   └── cn_forms.go         # Stateless CN22/CN23 form generation
│   ├── handler/
│   │   ├── handler.go
│   │   ├── bookings.go
│   │   ├── cancellations.go
│   │   ├── updates.go
│   │   ├── labels.go
│   │   ├── trackings.go
│   │   ├── notifications.go
│   │   ├── pickups.go
│   │   ├── returns.go
│   │   └── health.go
│   ├── middleware/
│   │   ├── request_id.go
│   │   ├── idempotency.go
│   │   └── logging.go
│   ├── notification/
│   │   ├── notification.go     # Event types, Payload, Preferences, Record
│   │   ├── service.go          # Fan-out dispatch
│   │   └── webhook.go          # HTTPS enforcement, HMAC signing, X-Event-Type
│   ├── router/router.go
│   ├── logger/logger.go
│   └── validation/
│       ├── address.go
│       ├── package.go
│       ├── idempotency.go
│       ├── customs.go
│       ├── carrier_customs.go
│       ├── countries.go
│       └── restricted.go
└── docs/
    ├── carriers.md                      # Full carrier coverage by country
    ├── implementation-status.md         # Feature matrix across all carriers
    ├── feature-roadmap.md               # Roadmap
    ├── parcel-poller.md                 # Companion service for tracking subscriptions
    ├── manifest-pickup-requirements.md  # Pickup and manifest endpoint spec
    └── *-feature-mapping.md             # Per-carrier detailed feature mapping
```

---

## Development

### Running tests

```bash
# All packages with race detector
go test -race -count=1 $(go list ./... | grep -v 'cmd/' | grep -v 'internal/router')

# With coverage
go test -cover ./...
```

### Pre-commit

```bash
go build ./...
go test -race -count=1 $(go list ./... | grep -v 'cmd/' | grep -v 'internal/router')
golangci-lint run
```

### Adding a carrier

1. Create `internal/adapter/{carrier}.go` implementing `CarrierAdapter`
2. Create `internal/adapter/mock_{carrier}.go`
3. Create `internal/adapter/{carrier}_test.go`
4. Add the carrier block to `InitAdapters` in `adapter.go` (env vars + fallback to mock)
5. Add a `capabilities` entry in `adapter.go`
6. Add status mappings in `status.go`
7. Add restricted goods entries in `validation/restricted.go`
8. Add a limits entry in `validation/package.go`
9. Wire customs fields in `adapter/customs.go` if the carrier supports cross-border
10. Add a `carrierCustomsRules` entry in `validation/carrier_customs.go`
11. Add a feature mapping file under `docs/`

The handler, router, and validation layer require no other changes.

---

## Roadmap

See [`docs/feature-roadmap.md`](docs/feature-roadmap.md) for the full spec. In priority order:

1. **Batch booking** — concurrent per-carrier fan-out, partial failure response
2. **Tracking subscriptions** — register parcels and receive webhooks as statuses change (requires companion service — see [`docs/parcel-poller.md`](docs/parcel-poller.md))

---

Apache 2.0 — see [LICENSE](LICENSE).
