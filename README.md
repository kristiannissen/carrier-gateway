# logistics-gateway

A stateless Go microservice for booking and tracking shipments across multiple Nordic and European carriers through a single consistent API. Change the `carrier` field in your request and the rest of your integration stays the same.

---

## Supported carriers

| Key | Carrier | Countries |
|---|---|---|
| `postnord` | PostNord | DK, SE, NO, FI |
| `bring` | Bring | NO, SE, DK, FI |
| `gls` | GLS | DK, SE, DE, NL, and more |
| `dao` | DAO | DK |
| `posti` | Posti | FI |
| `inpost` | InPost | PL |

---

## Quick start

```bash
git clone https://github.com/kristiannissen/logistics-gateway.git
cd logistics-gateway
go mod download

# Run in mock mode — no carrier credentials needed
MOCK_MODE=true LOG_ENV=development go run ./cmd/api
```

The server starts on `http://localhost:8080`.

---

## Environment variables

| Variable | Description | Required | Default |
|---|---|---|---|
| `PORT` | HTTP server port | No | `8080` |
| `LOG_ENV` | Set to `development` for console logging and debug payload dumps | No | — |
| `MOCK_MODE` | Set to `true` to use mock adapters — no real carrier credentials needed | No | `false` |
| `POSTNORD_API_KEY` | PostNord API key | No | — |
| `BRING_API_KEY` | Bring API key | No | — |
| `BRING_CUSTOMER_ID` | Bring customer ID | No | — |
| `GLS_API_KEY` | GLS API key | No | — |
| `GLS_CONTRACT_ID` | GLS contract ID | No | — |
| `DAO_API_KEY` | DAO API key | No | — |
| `DAO_CUSTOMER_ID` | DAO customer ID | No | — |
| `POSTI_API_KEY` | Posti API key | No | — |
| `INPOST_API_KEY` | InPost API key | No | — |

When a carrier's key is absent and `MOCK_MODE` is not set, that carrier falls back to its mock adapter automatically.

### `.env` example

```env
PORT=8080
LOG_ENV=development
MOCK_MODE=false
POSTNORD_API_KEY=your-postnord-key
BRING_API_KEY=your-bring-key
BRING_CUSTOMER_ID=your-bring-customer-id
GLS_API_KEY=your-gls-key
GLS_CONTRACT_ID=your-gls-contract-id
DAO_API_KEY=your-dao-key
DAO_CUSTOMER_ID=your-dao-customer-id
POSTI_API_KEY=your-posti-key
INPOST_API_KEY=your-inpost-key
```

---

## Docker

```bash
# Build
docker build -t logistics-gateway .

# Run with an env file
docker run -p 8080:8080 --env-file .env logistics-gateway

# Run in mock mode
docker run -p 8080:8080 -e MOCK_MODE=true -e LOG_ENV=development logistics-gateway
```

---

## API reference

### Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/bookings` | Book a shipment |
| `GET` | `/api/trackings/{trackingNumber}` | Track a shipment |
| `GET` | `/api/health` | Health check |

Every request receives an `X-Request-ID` header in the response. Pass it in your request to forward your own correlation ID — useful for tying gateway logs to your own system's logs.

---

### POST /api/bookings

Books a shipment with the specified carrier. The request body is identical regardless of carrier — only the `carrier` field changes.

#### Address fields

| Field | Type | Description | Required |
|---|---|---|---|
| `name` | string | Contact name | Yes |
| `street` | string | Street name only — no house number | Yes |
| `houseNumber` | string | House number — required for GLS, DAO, InPost (except France) | No |
| `supplement` | string | Building, floor, apartment, attention line | No |
| `city` | string | City | Yes |
| `postalCode` | string | Postal code | Yes |
| `country` | string | ISO 3166-1 alpha-2 country code | Yes |
| `phone` | string | Phone number | No |
| `email` | string | Email address | No |

#### Colli fields

| Field | Type | Description | Required |
|---|---|---|---|
| `id` | string | Unique identifier for this package | Yes |
| `reference` | string | Your own reference e.g. barcode | No |
| `weight` | float | Weight in kg | Yes |
| `dimensions.length` | float | Length in cm | No |
| `dimensions.width` | float | Width in cm | No |
| `dimensions.height` | float | Height in cm | No |
| `items` | array | Line items in this package | Yes |

#### Book a single-package shipment

```bash
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -H "X-Request-ID: my-correlation-id-001" \
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
      "totalWeight": 2.5,
      "colli": [
        {
          "id": "box-001",
          "weight": 2.5,
          "dimensions": { "length": 30, "width": 20, "height": 10 },
          "items": [
            {
              "description": "Football boots",
              "weight": 0.8,
              "quantity": 1,
              "value": 129.95,
              "sku": "FB-BOOT-42"
            }
          ]
        }
      ]
    },
    "idempotencyKey": "order-98765"
  }'
```

#### Switch carrier — nothing else changes

```bash
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "bring",
    "shipment": { ... }
  }'
```

#### Multi-package shipment

```bash
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "gls",
    "shipment": {
      "sender": {
        "name": "Unisport Group",
        "street": "Industrivej",
        "houseNumber": "10",
        "city": "Copenhagen",
        "postalCode": "2300",
        "country": "DK"
      },
      "receiver": {
        "name": "Klaus Müller",
        "street": "Hauptstraße",
        "houseNumber": "42",
        "city": "Berlin",
        "postalCode": "10115",
        "country": "DE"
      },
      "totalWeight": 12.0,
      "colli": [
        {
          "id": "box-001",
          "weight": 5.0,
          "dimensions": { "length": 40, "width": 30, "height": 20 },
          "items": [{ "description": "Jerseys", "weight": 0.3, "quantity": 10, "value": 49.95 }]
        },
        {
          "id": "box-002",
          "weight": 7.0,
          "dimensions": { "length": 50, "width": 40, "height": 30 },
          "items": [{ "description": "Shin guards", "weight": 0.2, "quantity": 20, "value": 19.95 }]
        }
      ]
    }
  }'
```

#### With supplement address line

```bash
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "posti",
    "shipment": {
      "sender": { ... },
      "receiver": {
        "name": "Marie Dupont",
        "street": "Rue de Rivoli",
        "supplement": "Bâtiment B, 3ème étage",
        "city": "Paris",
        "postalCode": "75001",
        "country": "FR"
      },
      "totalWeight": 1.5,
      "colli": [...]
    }
  }'
```

#### InPost — street and house number required as separate fields

```bash
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "inpost",
    "shipment": {
      "sender": { ... },
      "receiver": {
        "name": "Jan Kowalski",
        "street": "Marszałkowska",
        "houseNumber": "10",
        "city": "Warsaw",
        "postalCode": "00-001",
        "country": "PL",
        "phone": "+48123456789"
      },
      "totalWeight": 1.0,
      "colli": [
        {
          "id": "box-001",
          "weight": 1.0,
          "dimensions": { "length": 20, "width": 15, "height": 10 },
          "items": [{ "description": "Football", "weight": 0.5, "quantity": 1, "value": 24.95 }]
        }
      ]
    }
  }'
```

#### Successful response

```json
{
  "shipmentId": "shipment_482910",
  "trackingNumber": "PN482910123DK",
  "labelUrl": "https://api.postnord.com/labels/PN482910123DK.pdf",
  "carrier": "postnord",
  "cost": 125.50,
  "currency": "DKK",
  "serviceLevel": "1700",
  "status": "booked",
  "colli": [
    {
      "id": "1",
      "reference": "box-001",
      "trackingNumber": "PN482910123DK-1",
      "labelUrl": "https://api.postnord.com/labels/PN482910123DK-1.pdf",
      "status": "booked"
    }
  ]
}
```

---

### GET /api/trackings/{trackingNumber}

```bash
# Default carrier is postnord
curl http://localhost:8080/api/trackings/PN482910123DK

# Specify carrier
curl "http://localhost:8080/api/trackings/BR123456789NO?carrier=bring"

# With correlation ID
curl "http://localhost:8080/api/trackings/GLS123456789DK?carrier=gls" \
  -H "X-Request-ID: my-correlation-id-002"
```

#### Response

```json
{
  "trackingNumber": "PN482910123DK",
  "carrier": "postnord",
  "status": "In Transit",
  "estimatedDelivery": "2026-06-07",
  "events": [
    {
      "timestamp": "2026-06-05T08:30:00Z",
      "status": "Picked Up",
      "location": "Copenhagen, DK",
      "details": "Package picked up at sender location"
    },
    {
      "timestamp": "2026-06-05T14:00:00Z",
      "status": "In Transit",
      "location": "Malmö, SE",
      "details": "Package arrived at Malmö hub"
    }
  ],
  "colli": [
    {
      "id": "1",
      "trackingNumber": "PN482910123DK-1",
      "status": "In Transit",
      "events": [...]
    }
  ]
}
```

---

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
    "gls": "mock",
    "dao": "mock",
    "posti": "mock",
    "inpost": "mock"
  }
}
```

`carriers` shows each registered carrier and whether it is running against the real API (`production`) or the built-in mock (`mock`). A carrier shows `mock` either because `MOCK_MODE=true` is set or because its API key is not configured.

---

## Edge cases and validation

### Postal codes

| Country | Format | Example | Error |
|---|---|---|---|
| DK | 4 digits | `2300` | `invalid Danish postal code: 230` |
| NO | 4 digits | `0158` | `invalid Norwegian postal code: 158` |
| SE | 5 digits | `11122` | `invalid Swedish postal code: 1112` |
| FI | 5 digits | `00100` | `invalid Finnish postal code: 0010` |
| PL | `NN-NNN` | `00-001` | `invalid Polish postal code: 00001` |

### House number requirements

`houseNumber` must be provided as a distinct field for GLS, DAO, and InPost. Embedding the house number in `street` will pass to the carrier but may cause delivery failures. France is exempt — house numbers are not always present in French addresses.

```bash
# Missing houseNumber for GLS
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "gls",
    "shipment": {
      "receiver": {
        "name": "Test",
        "street": "Hauptstraße 42",
        "city": "Berlin",
        "postalCode": "10115",
        "country": "DE"
      }, ...
    }
  }'

# Error response
{ "error": "validation failed", "details": "house number is required for gls shipments to DE" }
```

### Carrier weight and dimension limits

| Carrier | Max weight | Max dimensions | Max girth |
|---|---|---|---|
| PostNord | 30 kg | L+W+H ≤ 300 cm | 2×(W+H)+L ≤ 300 cm |
| Bring | 30 kg | L ≤ 250 cm, W ≤ 120 cm, H ≤ 100 cm | — |
| GLS | 40 kg | L ≤ 270 cm, W ≤ 120 cm, H ≤ 120 cm | 2×(W+H)+L ≤ 400 cm |
| DAO | 35 kg | L ≤ 250 cm, W ≤ 120 cm, H ≤ 120 cm | — |
| Posti | 30 kg | L ≤ 200 cm, W ≤ 100 cm, H ≤ 100 cm | 2×(W+H)+L ≤ 300 cm |

PostNord also enforces a maximum of 5 colli per shipment.

### Idempotency

Pass `idempotencyKey` in the request body to deduplicate bookings on your side. Keys must be 64 characters or fewer. Omitting the key is valid — the request is processed normally.

```bash
# Key too long
{ "error": "validation failed", "details": "idempotency key must be 64 characters or fewer" }
```

---

## Payload logging

In development (`LOG_ENV=development`), full request and response bodies are logged at `DEBUG` level. In production the logger runs at `INFO` by default so no payload data is written — there is zero cost when debug logging is off.

Sensitive fields are scrubbed before any log entry is written:

| Data | Treatment |
|---|---|
| `Authorization` header | SHA-256 hash |
| JSON fields: `password`, `token`, `apiKey`, `secret` | `[redacted]` |

To enable payload logging in production for a debugging session:

```bash
LOG_LEVEL=debug ./logistics-gateway
```

---

## Input formats

The booking endpoint accepts JSON (default), XML, and UN/EDIFACT IFTMIN. Set the `Content-Type` header accordingly:

| Format | Content-Type |
|---|---|
| JSON | `application/json` |
| XML | `application/xml` |
| EDIFACT | `application/edifact` |

---

## Development

### Project structure

```
logistics-gateway/
├── cmd/
│   └── api/
│       └── main.go               # HTTP server entry point
├── internal/
│   ├── adapter/
│   │   ├── adapter.go            # CarrierAdapter interface, Registry, shared types
│   │   ├── postnord.go           # PostNord adapter
│   │   ├── bring.go              # Bring adapter
│   │   ├── gls.go                # GLS adapter
│   │   ├── dao.go                # DAO adapter
│   │   ├── posti.go              # Posti adapter
│   │   ├── inpost.go             # InPost adapter
│   │   ├── mock_*.go             # Mock adapters for testing and MOCK_MODE
│   │   └── *_test.go             # Adapter tests
│   ├── handler/
│   │   ├── handler.go            # Shared config, loggerFor, writeError
│   │   ├── bookings.go           # POST /api/bookings
│   │   ├── trackings.go          # GET /api/trackings/{trackingNumber}
│   │   └── health.go             # GET /api/health
│   ├── middleware/
│   │   ├── request_id.go         # X-Request-ID propagation
│   │   └── logging.go            # Debug payload logging with scrubbing
│   ├── parser/
│   │   ├── parser.go             # Content-Type routing
│   │   ├── json.go               # JSON parser
│   │   ├── xml.go                # XML parser
│   │   └── edifact.go            # UN/EDIFACT IFTMIN parser
│   ├── router/
│   │   └── router.go             # Route definitions and middleware wiring
│   ├── logger/
│   │   └── logger.go             # Zap logger constructor
│   └── validation/
│       ├── address.go            # Postal codes, street, house number rules
│       ├── package.go            # Per-carrier weight, dimensions, girth
│       └── idempotency.go        # Idempotency key rules
├── Dockerfile
├── go.mod
├── go.sum
└── README.md
```

### Running tests

```bash
# All tests
go test ./...

# With race detector
go test -race ./...

# With coverage
go test -cover ./...

# Specific package
go test ./internal/adapter/...
```

### How the adapter tests work

Every real adapter test spins up an `httptest.Server` that captures the raw request body and verifies the exact wire payload the carrier expects. This means the tests catch mapping errors that would only surface at runtime against a live API — wrong field names, missing nesting levels, incorrect unit conversions, and so on.

For example, the Bring test asserts that the payload uses `weightInKg`, `lengthInCm`, `widthInCm`, and `heightInCm` (Bring's unit-suffixed keys), that the sender is nested under `from` rather than `sender`, and that the receiver is under `to`. The PostNord test verifies that colli weights are converted from kg to grams before they leave the gateway. The InPost test checks that `streetName` and `houseNumber` arrive as separate fields.

Each carrier's test file is effectively a contract — if a carrier changes their API, the test fails before any code reaches production.

### Adding a carrier

1. Create `internal/adapter/{carrier}.go` implementing `CarrierAdapter`
2. Create `internal/adapter/mock_{carrier}.go`
3. Create `internal/adapter/{carrier}_test.go`
4. Add the carrier block to `InitAdapters` in `adapter.go`
5. Add a limits entry in `internal/validation/package.go`

The handler, router, and validation layer require no changes.

---

## License

Apache 2.0 — see [LICENSE](LICENSE).
