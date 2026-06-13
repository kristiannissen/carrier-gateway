# carrier-gateway

## Why this exists

Ten years of involuntary proximity to freight will do things to a person.

Not ten years of choosing logistics as a craft — ten years of it being the unavoidable tax on running an e-commerce business. Parcels that needed to move, carriers that needed to be appeased, and a middleware provider sitting between you and the carriers that promised to make it simple.

It did not make it simple.

The pitch is always the same: one integration, all carriers, we handle the complexity. What you actually get is a proprietary abstraction layer with its own quirks, its own data model, its own versioning strategy, and a support organisation whose response times are calibrated for a world where your warehouse isn't waiting. Every carrier behaviour you need to understand you now understand twice — once as the carrier actually works, and once as the middleware interprets it. Bugs live in the gap between those two things, and when something breaks, you own the debug even though you own none of the code.

The bitter irony is that integrating directly with the carriers would have been straightforward by comparison. Carrier APIs are well-documented, stable, and mostly sensible. The complexity was never in the carriers. It was in the layer we were paying to protect us from them.

This project is what direct integration looks like when you do it properly. A single consistent API, adapters that absorb carrier-specific wire format details, and no middleware standing between your order management system and the carrier actually moving your parcel.

It is also an experiment in how software gets built. Almost the entire codebase was written by AI — Claude, specifically — working from design decisions and architectural direction provided by a human with strong opinions and hard-won context. The human provides the judgement. The AI executes. It turns out that combination produces software faster than either could alone, and the result is readable enough that the human can tell when the AI is wrong.

Whether it holds up is the interesting question.

---

A stateless Go microservice that provides a single consistent API for booking, tracking, and returning shipments across multiple Nordic and European carriers. Change the `carrier` field in your request — the rest of your integration stays the same.

```bash
# Book with PostNord today, switch to Bring tomorrow — same request shape
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{"carrier": "bring", "shipment": { ... }}'
```

---

## Supported carriers

See [`docs/carriers.md`](docs/carriers.md) for full country coverage and [`docs/implementation-status.md`](docs/implementation-status.md) for a feature-by-feature breakdown.

| Key | Carrier | Status |
|---|---|---|
| `postnord` | PostNord (DK, SE, NO, FI) | Production |
| `bring` | Bring / Posten (NO, SE, DK, FI) | Production |
| `gls` | GLS (DE, DK, SE, NL, BE, FR, ES + more) | Production |
| `dao` | DAO (DK) | Beta |
| `dhl` | DHL eCommerce Europe (28 countries) | Beta |
| `fedex` | FedEx (worldwide) | Beta |
| `posti` | Posti (FI) | Demo — mock only |
| `inpost` | InPost (PL, UK, FR, IT) | Demo — mock only |

Demo carriers return mock data and are not connected to any live API.

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

Unsupported operations (e.g. GLS cancellations, FedEx label reprint) return a typed `ErrNotSupported` error which the handler translates to `501 Not Implemented` with a descriptive message. The caller gets a clear signal instead of an opaque error.

### Stateless by design

The gateway holds no database and no per-request state. Every call is self-contained:

- Booking requests carry all the data the carrier needs.
- Webhook dispatch (`POST /api/notifications`, `POST /api/trackings/{id}`) is fire-and-forget — the caller owns retry state.
- CN22/CN23 customs forms are generated on the fly and returned inline in the booking response.
- Idempotency keys are logged and forwarded to carriers that support them natively; deduplication for others is the caller's responsibility.

The only planned stateful feature — subscription-based tracking events — is intentionally kept in a separate companion service ([`docs/parcel-poller.md`](docs/parcel-poller.md)) so the gateway itself stays stateless.

### Mock-first development

Every carrier has a matching `mock_{carrier}.go` that satisfies `CarrierAdapter` and returns realistic fixture data. Mocks are selected in two ways:

- **`MOCK_MODE=true`** — all carriers use their mock adapter, regardless of credentials.
- **Missing credentials** — if a carrier's API key is absent and `MOCK_MODE` is not set, that carrier automatically falls back to its mock adapter. The health endpoint reports which mode each carrier is running in.

This means the server starts and returns sensible responses on day one, before any carrier account is open.

### Normalised statuses

Each carrier returns its own status vocabulary. The gateway maps every raw string to a shared `TrackingStatus` type:

| Value | Meaning |
|---|---|
| `booked` | Booked but not yet collected |
| `picked_up` | Collected from sender |
| `in_transit` | Moving through carrier network |
| `out_for_delivery` | On the delivery vehicle |
| `delivered` | Delivered to recipient |
| `failed` | Delivery attempt failed |
| `returned` | Being returned to sender |
| `delayed` | Delayed relative to original ETA |
| `unknown` | Status not in mapping table |

The raw carrier string is preserved in `originalStatus` for debugging. Callers should only branch on `normalizedStatus`.

### Webhook notifications

The gateway signs all outgoing webhooks with HMAC-SHA256 (`X-Signature: sha256=<hex>`) and stamps every request with the event name (`X-Event-Type`). Webhooks are only dispatched to `https://` endpoints — plain HTTP is rejected at request time, not silently dropped.

Dispatch can be triggered at three points:

1. **At booking** — add a `notifications` block to the booking request; a `booked` event fires on success.
2. **At poll** — `POST /api/trackings/{trackingNumber}` with `previousStatus` and a `notifications` block; a notification fires when the status has advanced.
3. **On demand** — `POST /api/notifications` for ad-hoc dispatch without a booking or tracking call.

---

## DX focus

These decisions were made specifically to reduce integration friction:

- **One request shape.** Every carrier uses the same `BookingRequest` / `BookingResponse` structure. Switching carriers is a one-field change.
- **Inline labels.** Labels are returned as base64 in the booking response for most carriers — no second HTTP call to fetch them.
- **Soft failures, not silent ones.** `addOnWarnings` and `customsWarnings` in the booking response surface partial failures (e.g. DAO SMS notification rejected after booking) without rolling back the shipment. The tracking number is always valid when returned.
- **Mock without credentials.** `MOCK_MODE=true` starts the server with full mock coverage. Individual carriers fall back to mock automatically when their key is absent.
- **501 for unsupported, not 400.** Operations a carrier does not support return `501 Not Implemented` with the carrier name and the unsupported operation in the error body — not a generic 400 that looks like a bad request.
- **Correlation IDs.** Every response carries `X-Request-ID`. Pass your own in the request to propagate a trace ID end-to-end.
- **Customs forms inline.** CN22/CN23 declarations are generated automatically for cross-border shipments and returned as base64 plain text in `cnDocument` — one step instead of two.
- **Structured logging.** All logs use `zap` in JSON mode by default; `LOG_ENV=development` switches to coloured console output. Sensitive fields (`Authorization`, `apiKey`, `secret`, `token`, `password`) are redacted before writing.
- **Development payload dumps.** `LOG_ENV=development` logs full request and response bodies at `DEBUG` level, making it straightforward to inspect what was sent to and received from each carrier API.

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
# Verify it is up
curl http://localhost:8080/api/health
```

---

## Environment variables

| Variable | Description | Default |
|---|---|---|
| `PORT` | HTTP server port | `8080` |
| `LOG_ENV` | `development` for console logging and debug payload dumps | — |
| `MOCK_MODE` | `true` to force all carriers to use mock adapters | `false` |
| `POSTNORD_API_KEY` | PostNord API key | — |
| `POSTNORD_CUSTOMER_NUMBER` | PostNord account number (partyId) | — |
| `POSTNORD_APPLICATION_ID` | PostNord application ID | — |
| `BRING_API_KEY` | Bring API key | — |
| `BRING_CUSTOMER_ID` | Mybring login email | — |
| `BRING_CUSTOMER_NUMBER` | Bring customer account number | — |
| `GLS_API_KEY` | GLS OAuth2 client ID | — |
| `GLS_CLIENT_SECRET` | GLS OAuth2 client secret | — |
| `GLS_CONTRACT_ID` | GLS shipper contact ID | — |
| `DAO_API_KEY` | DAO API key | — |
| `DAO_CUSTOMER_ID` | DAO customer ID | — |
| `DHL_CLIENT_ID` | DHL eConnect OAuth2 client ID | — |
| `DHL_CLIENT_SECRET` | DHL eConnect OAuth2 client secret | — |
| `DHL_CUSTOMER_ID` | DHL customerIdentification | — |
| `DHL_TRACKING_API_KEY` | DHL Unified Tracking API subscription key | — |
| `FEDEX_CLIENT_ID` | FedEx OAuth2 client ID | — |
| `FEDEX_CLIENT_SECRET` | FedEx OAuth2 client secret | — |
| `FEDEX_ACCOUNT_NUMBER` | FedEx account number | — |

When a carrier's key is absent and `MOCK_MODE` is not set, that carrier falls back to its mock adapter automatically. The `GET /api/health` response shows which mode each carrier is running in.

---

## Docker

```bash
docker build -t carrier-gateway .
docker run -p 8080:8080 --env-file .env carrier-gateway

# Mock mode
docker run -p 8080:8080 -e MOCK_MODE=true -e LOG_ENV=development carrier-gateway
```

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
| `shipment.addOns` | array | `sms_notification`, `email_notification`, `flex_delivery`, `signature_required`, `cash_on_delivery`, `insurance` |
| `shipment.customs` | object | Required for non-EU destinations — see [Cross-border](#cross-border-shipments-and-customs) |
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

`booked` and `unknown` statuses never trigger dispatch. `notificationsSent` and `notificationsFailed` appear in the response only when a dispatch was attempted.

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

| Carrier | Support |
|---|---|
| PostNord | ✅ Before collection |
| Bring | ✅ Before collection |
| GLS | ✅ Before collection |
| DAO | ✅ Before first terminal scan |
| DHL | ❌ eConnect portal or customer service |
| FedEx | ✅ Cancels all packages in the shipment |

### PATCH /api/bookings/{trackingNumber}

Updates contact details, weight, or service point after booking.

```bash
curl -X PATCH "http://localhost:8080/api/bookings/00057126960000003016?carrier=dao" \
  -H "Content-Type: application/json" \
  -d '{"phone": "+4587654321", "email": "new@example.com", "weight": 2.3}'
```

| Carrier | phone/email | weight | servicePointId |
|---|---|---|---|
| PostNord | ✅ (SE only) | ❌ | ❌ |
| DAO | ✅ | ✅ | ✅ |
| Others | ❌ | ❌ | ❌ |

### GET /api/labels/{trackingNumber}

```bash
# Decode and save as PDF
curl -s "http://localhost:8080/api/labels/00073215400599388772?carrier=postnord" \
  | jq -r '.data' | base64 -d > label.pdf

# ZPL direct to thermal printer
curl -s "http://localhost:8080/api/labels/00073215400599388772?carrier=postnord&format=ZPL" \
  | jq -r '.data' | base64 -d > /dev/usb/lp0
```

Supported formats: `PDF` (all carriers), `ZPL` and `ZPLGK` (PostNord, GLS). FedEx label fetch is not yet implemented — store the label from the booking response.

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
    "dao": "beta",
    "dhl": "beta",
    "fedex": "beta",
    "posti": "production",
    "inpost": "production"
  }
}
```

When `MOCK_MODE=true` all carriers report `"mock"`.

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

For cross-border non-EU shipments the gateway auto-generates a CN22 (≤ €22) or CN23 form and returns it base64-encoded in `cnDocument`:

```bash
curl -s -X POST http://localhost:8080/api/bookings -H "Content-Type: application/json" \
  -d '{ ... }' | jq -r '.cnDocument' | base64 -d > customs-declaration.txt
```

#### Carrier-specific item limits

| Carrier | Max line items | EORI/VAT required for non-EU |
|---|---|---|
| DHL | 99 | Yes — `exporterVatNumber` required |
| PostNord | 5 | No |
| GLS | No limit (server-side) | No |

VAT numbers are validated against format rules for DK, SE, FI, NO, DE, FR, NL, and PL. EU VAT numbers are additionally checked against the [VIES REST API](https://ec.europa.eu/taxation_customs/vies/) in parallel (2 second timeout). VIES failures are non-blocking — the booking proceeds with format-only validation and a `customsWarnings` entry.

Sea-only Incoterms (`FOB`, `FAS`, `CFR`, `CIF`) are rejected when `transportMode` is `air`, `road`, or `rail`.

---

## Restricted goods

Item descriptions are checked against per-carrier and per-destination prohibited and restricted goods lists. Blocked items return `400`; restricted items proceed with a `customsWarnings` entry.

Examples: explosives and firearms are blocked for all carriers. Lithium batteries are warned for all carriers (UN3480/UN3481 labelling required). Norway blocks HS code chapters 22 (alcohol) and 24 (tobacco) at validation time.

---

## Add-ons

| Type | PostNord | Bring | GLS | DAO | DHL |
|---|---|---|---|---|---|
| `sms_notification` | ✅ | ✅ | ❌ | ⚠️ post-booking | ❌ |
| `email_notification` | ✅ | ✅ | ❌ | ⚠️ post-booking | ❌ |
| `flex_delivery` | ✅ | ✅ | ✅ | ❌ | ✅ |
| `signature_required` | ✅ | ✅ | ✅ | ❌ | ✅ |
| `cash_on_delivery` | ❌ | ✅ | ❌ | ❌ | ❌ |
| `insurance` | ✅ | ❌ | ❌ | ❌ | ✅ |

DAO SMS and email are applied via a separate contact-update call after booking. If that call fails, `addOnWarnings` is populated and the booking is still valid. Retry via `PATCH /api/bookings/{trackingNumber}?carrier=dao`.

---

## Returns

Set `deliveryType: "return"`. Sender is the customer returning the parcel; receiver is the merchant — addresses are not swapped automatically.

| Carrier | Mechanism | Labelless |
|---|---|---|
| PostNord | `/rest/shipment/v3/returns/edi/labels/pdf` | Yes — `returnFunctionality: "labelless"`, QR code via SMS/email |
| Bring | `returnProduct.id: "9350"` | No |
| GLS | Shop Returns Customer Plus API v3 | No |
| DAO | `/DAOPakkeshop/returordre.php` | Yes — `returnFunctionality: "labelless"` (default) |
| DHL | `product: "ParcelEurope.return.network"` | Yes — QR code as base64 PNG (BE, BG, DE, ES, LU, PL, PT, SE) |
| FedEx | ❌ Not yet implemented | — |

---

## Idempotency

Pass `idempotencyKey` in the request body (max 64 characters).

| Carrier | Behaviour |
|---|---|
| PostNord | Forwarded as `shipmentReference` — server-side deduplication |
| Others | Logged; deduplication is the caller's responsibility |

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
│   │   ├── inpost.go           # Demo — mock only
│   │   ├── mock_*.go
│   │   └── *_test.go
│   ├── customs/
│   │   └── cn_forms.go         # Stateless CN22/CN23 form generation
│   ├── handler/
│   │   ├── handler.go
│   │   ├── bookings.go         # POST /api/bookings
│   │   ├── cancellations.go
│   │   ├── updates.go
│   │   ├── labels.go
│   │   ├── trackings.go        # GET + POST /api/trackings
│   │   ├── notifications.go    # POST /api/notifications
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
│       ├── address.go          # Postal codes, house number, state/province rules
│       ├── package.go          # Per-carrier weight and dimension limits
│       ├── idempotency.go
│       ├── customs.go          # Cross-border, de minimis, VAT format, VIES lookup
│       ├── carrier_customs.go  # Per-carrier item limits and EORI requirements
│       ├── countries.go        # EU / European country sets
│       └── restricted.go       # Per-carrier and per-destination prohibited goods
└── docs/
    ├── carriers.md                      # Full carrier coverage by country
    ├── implementation-status.md         # Feature matrix across all carriers
    ├── feature-roadmap.md               # Batch booking, pickup scheduling, manifest, tracking subscriptions
    ├── parcel-poller.md                 # Companion service design for subscription-based tracking
    ├── manifest-pickup-requirements.md  # Pickup and manifest endpoint spec
    └── *-feature-mapping.md            # Per-carrier detailed feature mapping
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

1. **Batch booking** — `POST /api/bookings/batch`, concurrent per-carrier fan-out, partial failure response
2. **Pickup scheduling** — `POST /api/pickups`, `PUT`, `DELETE` — tell the carrier when to collect
3. **Manifest** — `POST /api/manifests` — close the day and retrieve the handover document (required for GLS before driver arrival)
4. **Tracking subscriptions** — register parcels and receive webhooks as statuses change; requires a backing store (see [`docs/parcel-poller.md`](docs/parcel-poller.md))

Features 1–3 are fully stateless. Feature 4 requires a companion service with persistent state.

---

Apache 2.0 — see [LICENSE](LICENSE).
