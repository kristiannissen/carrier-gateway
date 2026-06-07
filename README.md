# logistics-gateway

A stateless Go microservice for booking and tracking shipments across multiple Nordic and European carriers through a single consistent API. Change the `carrier` field in your request and the rest of your integration stays the same.

---

## Supported carriers

| Key | Carrier | Countries | Status |
|---|---|---|---|
| `postnord` | PostNord | DK, SE, NO, FI | Production |
| `bring` | Bring | NO, SE, DK, FI | Production |
| `gls` | GLS | DK, SE, DE, NL, and more | Production |
| `dao` | DAO | DK | **Beta** |
| `posti` | Posti | FI | Production |
| `inpost` | InPost | PL | Production |

DAO is in beta — bookings work but label printing is not yet available. Labels must be downloaded from the DAO portal directly.

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
| `POSTNORD_CUSTOMER_NUMBER` | PostNord account number (partyId) | No | — |
| `POSTNORD_APPLICATION_ID` | PostNord application ID (integer assigned by portal) | No | — |
| `BRING_API_KEY` | Bring API key | No | — |
| `BRING_CUSTOMER_ID` | Bring customer ID | No | — |
| `GLS_API_KEY` | GLS OAuth2 client ID | No | — |
| `GLS_CLIENT_SECRET` | GLS OAuth2 client secret | No | — |
| `GLS_CONTRACT_ID` | GLS shipper contact ID | No | — |
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
POSTNORD_CUSTOMER_NUMBER=150011208
POSTNORD_APPLICATION_ID=your-application-id
BRING_API_KEY=your-bring-key
BRING_CUSTOMER_ID=your-bring-customer-id
GLS_API_KEY=your-gls-client-id
GLS_CLIENT_SECRET=your-gls-client-secret
GLS_CONTRACT_ID=your-gls-contact-id
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
| `GET` | `/api/labels/{trackingNumber}` | Fetch a shipping label |
| `GET` | `/api/health` | Health check |

Every request receives an `X-Request-ID` header in the response. Pass it in your request to forward your own correlation ID — useful for tying gateway logs to your own system's logs.

---

### POST /api/bookings

Books a shipment with the specified carrier. The request body is identical regardless of carrier — only the `carrier` field changes.

#### Address fields

| Field | Type | Description | Required |
|---|---|---|---|
| `name` | string | Contact name | Yes |
| `street` | string | Street name only — no house number | Yes* |
| `houseNumber` | string | House number — required for GLS, DAO, InPost (except France) | No |
| `supplement` | string | Building, floor, apartment, attention line | No |
| `city` | string | City | Yes* |
| `postalCode` | string | Postal code | Yes* |
| `country` | string | ISO 3166-1 alpha-2 country code | Yes |
| `state` | string | State/province/territory code — required for US, CA, BR, AU; optional but validated for DE | No |
| `servicePointId` | string | Carrier service point ID — when set, street/city/postalCode are optional for the receiver | No |
| `phone` | string | Phone number | No |
| `email` | string | Email address | No |

\* Not required when `servicePointId` is set on the receiver address.

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

### GET /api/labels/{trackingNumber}

Fetches the shipping label for a booked shipment and returns it as base64-encoded data ready for printing.

| Query parameter | Description | Default |
|---|---|---|
| `carrier` | Carrier key | `postnord` |
| `format` | Label format: `PDF`, `PNG`, `ZPL`, `EPL`, `ZPLGK` | `PDF` |

#### Label format support by carrier

| Carrier | PDF | PNG | ZPL | EPL | ZPLGK |
|---|---|---|---|---|---|
| PostNord | Yes | Yes | Yes | Yes | Yes |
| Bring | Yes | No | No | No | No |
| GLS | Yes | No | Yes | No | Yes |
| DAO | No | No | No | No | No |
| Posti | Yes | No | No | No | No |
| InPost | Yes | No | No | No | No |

DAO label printing is not yet available — labels must be downloaded from the DAO portal.

```bash
# Fetch a PDF label (default)
curl "http://localhost:8080/api/labels/PN482910123DK?carrier=postnord"

# Fetch a ZPL label for a thermal printer
curl "http://localhost:8080/api/labels/PN482910123DK?carrier=postnord&format=ZPL"

# Fetch a GLS label in ZPLGK format
curl "http://localhost:8080/api/labels/GLS123456789DK?carrier=gls&format=ZPLGK"
```

#### Response

```json
{
  "trackingNumber": "PN482910123DK",
  "carrier": "postnord",
  "format": "PDF",
  "data": "JVBERi0xLj...",
  "mimeType": "application/pdf"
}
```

`data` is a base64-encoded string. Decode it to get the raw label bytes:

```bash
# Decode and save as a file
curl -s "http://localhost:8080/api/labels/PN482910123DK?carrier=postnord" \
  | jq -r '.data' \
  | base64 -d > label.pdf

# Decode a ZPL label and send directly to a thermal printer
curl -s "http://localhost:8080/api/labels/PN482910123DK?carrier=postnord&format=ZPL" \
  | jq -r '.data' \
  | base64 -d > /dev/usb/lp0
```

---

### Service point delivery

To ship to a carrier service point (parcel shop, pickup point, or locker), set `servicePointId` on the receiver address. Street, city, and postal code are optional when a service point ID is provided.

Each carrier uses a different field name internally — the gateway maps `servicePointId` to the correct wire field automatically:

| Carrier | Wire field |
|---|---|
| PostNord | `servicePointId` |
| Bring | `pickupPointId` |
| Posti | `pickupPointId` |
| GLS | `parcelShopId` |
| DAO | `lockerId` |
| InPost | `targetLocker` |

```bash
# PostNord service point delivery
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
        "country": "SE",
        "phone": "+46701234567",
        "email": "anna@example.com",
        "servicePointId": "sp_123"
      },
      "totalWeight": 1.0,
      "colli": [
        {
          "id": "box-001",
          "weight": 1.0,
          "dimensions": { "length": 20, "width": 15, "height": 10 },
          "items": [
            { "description": "Football boots", "weight": 1.0, "quantity": 1 }
          ]
        }
      ]
    }
  }'
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
│   │   ├── labels.go             # GET /api/labels/{trackingNumber}
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

## Idempotency

Sending the same booking request twice — due to a network timeout, a retry loop, or a client crash — should not create two shipments. The gateway supports the `Idempotency-Key` header to let your system identify and deduplicate retries.

### How it works

Include an `Idempotency-Key` header on any `POST /api/bookings` request:

```bash
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-98765-attempt-1" \
  -d '{ "carrier": "postnord", "shipment": { ... } }'
```

The key can also be sent in the request body as `idempotencyKey`. If both are present they must match — a mismatch returns `422`.

The key must be 64 characters or fewer. A good key is something that uniquely identifies the booking attempt from your system — an order ID, a combination of order ID and attempt number, or a UUID you generate before sending.

### What the gateway does

The middleware reads the header, injects it into the request body, and stores it on the request context. Every log entry for that request carries the key as a structured field, so you can search your logs for all activity related to a specific key.

For carriers that accept an idempotency key natively (currently PostNord), the key is forwarded in the wire payload. The carrier's API then guarantees that a second request with the same key returns the original booking rather than creating a new one.

### Carrier support

| Carrier | Native idempotency | Behaviour |
|---|---|---|
| PostNord | Yes | Key forwarded to carrier API; server-side deduplication |
| Bring | No | Key logged; deduplication is your responsibility |
| GLS | No | Key logged; deduplication is your responsibility |
| DAO | No | Key logged; deduplication is your responsibility |
| Posti | No | Key logged; deduplication is your responsibility |
| InPost | No | Key logged; deduplication is your responsibility |

### Your responsibility

For carriers without native idempotency support, **the gateway does not store responses or detect duplicate keys**. You need to implement deduplication in your system before calling the gateway. The typical approaches:

**In-memory** — suitable for single-instance deployments or serverless functions where each invocation is short-lived. Store a map of key → response in your process. Fast but lost on restart.

**Redis** — suitable for distributed deployments. Set a key with a TTL matching your retry window (e.g. 24 hours). Before booking, check if the key exists; if it does, return the stored response.

**Database** — suitable when you need an audit trail. Store key, response, carrier, and timestamp in a bookings table. Query before each booking attempt.

Regardless of which approach you use, the pattern is the same:

```
1. Generate a stable key for this booking attempt (e.g. "order-{id}")
2. Check your store — if the key exists, return the stored response
3. Call POST /api/bookings with Idempotency-Key: {key}
4. Store the response against the key
5. Return the response to your caller
```

The key should remain stable across retries of the same logical booking. If you want to book the same order again after a cancellation, use a different key.

---

## Cross-border shipments and customs

Shipping within the EU is straightforward — goods move freely with no customs declarations required for most shipments. Shipping to Norway is different. Norway is not an EU member, which means every commercial shipment crossing that border is a customs event, regardless of carrier.

The `customs` block on a shipment captures everything a carrier and customs authority need to process a cross-border shipment. It is optional for domestic and intra-EU B2C shipments below the de minimis threshold. It is required — and validated before the request reaches the carrier — for everything else.

### Fields

```json
"customs": {
  "incoterms": "DDP",
  "hsCode": "61091000",
  "customsValue": 500.0,
  "customsCurrency": "DKK",
  "importerOfRecord": "NO123456789",
  "importerVatNumber": "SE1234567890",
  "exporterVatNumber": "12345678",
  "shipmentType": "B2B"
}
```

**`incoterms`** — The trade term that defines who pays for shipping, insurance, and import duties, and where responsibility transfers from seller to buyer. The most common values for e-commerce:

| Term | Meaning |
|---|---|
| `DDP` | Delivered Duty Paid — the seller covers everything including import duties. The buyer receives the parcel with no extra charges. |
| `DAP` | Delivered at Place — the seller covers transport but the buyer pays import duties on arrival. |
| `EXW` | Ex Works — buyer collects from the seller's premises and handles all transport and duties themselves. |

Other accepted values: `FCA`, `CPT`, `CIP`, `DPU`, `FAS`, `FOB`, `CFR`, `CIF`.

For shipments to Norway, `DDP` is the most buyer-friendly option and avoids surprise charges at delivery. Required for all non-EU destinations.

**`hsCode`** — Harmonized System code. A 6–10 digit number that classifies what is inside the parcel. Every product in international trade has one. Customs authorities use it to determine the applicable duty rate and whether any restrictions apply.

Examples:
- `61091000` — T-shirts of cotton
- `64041100` — Sports footwear with outer soles of rubber
- `95066290` — Footballs

You can look up HS codes at [customs.dk](https://toldsatser.dk) (Denmark) or [taric.ec.europa.eu](https://ec.europa.eu/taxation_customs/dds2/taric) (EU). Required for non-EU destinations and EU shipments with a customs value above 150 EUR.

**`customsValue`** and **`customsCurrency`** — The declared value of the shipment in the specified currency (ISO 4217 code, e.g. `DKK`, `NOK`, `EUR`). This is what customs uses to calculate duties and VAT. It should reflect the actual commercial value, not a discounted or zero value.

**`importerOfRecord`** — The Norwegian VAT number or EORI number of the entity responsible for clearing the goods through Norwegian customs. For B2B shipments this is typically the buyer's Norwegian business registration number (9 digits, e.g. `NO123456789`). Required for all shipments to Norway.

**`importerVatNumber`** — The VAT registration number of the buyer, required for B2B shipments within the EU. Without it the seller cannot zero-rate the VAT on the invoice. Format varies by country:

| Country | Format | Example |
|---|---|---|
| Denmark | 8 digits | `12345678` |
| Sweden | SE + 10 digits | `SE1234567890` |
| Finland | 8 digits | `12345678` |
| Germany | DE + 9 digits | `DE123456789` |
| Norway | 9 digits | `123456789` |

**`exporterVatNumber`** — The VAT registration number of the sender. Required for non-EU destinations so the receiving country's customs authority can identify the exporting business.

**`shipmentType`** — Either `B2B` (business to business) or `B2C` (business to consumer). This affects which rules apply — B2B shipments within the EU require a valid `importerVatNumber`; B2C shipments below the de minimis threshold are exempt from customs declarations.

---

### Rules by destination

#### Shipping to Norway (non-EU)

Norway applies a de minimis threshold of **350 NOK** for B2C shipments. Below this value no customs declaration is required. Above it, all fields are mandatory.

```bash
# B2C shipment below NOK de minimis — no customs fields needed
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "postnord",
    "shipment": {
      "sender": { ... },
      "receiver": { ... },
      "totalWeight": 0.5,
      "colli": [...],
      "customs": {
        "customsValue": 299.0,
        "customsCurrency": "NOK",
        "shipmentType": "B2C"
      }
    }
  }'

# B2B shipment — full declaration required
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "bring",
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
        "name": "Sport AS",
        "street": "Karl Johans gate",
        "houseNumber": "1",
        "city": "Oslo",
        "postalCode": "0154",
        "country": "NO"
      },
      "totalWeight": 5.0,
      "colli": [
        {
          "id": "box-001",
          "weight": 5.0,
          "dimensions": { "length": 40, "width": 30, "height": 20 },
          "items": [{ "description": "Football boots", "weight": 0.5, "quantity": 5, "value": 129.95, "sku": "FB-BOOT-42" }]
        }
      ],
      "customs": {
        "incoterms": "DDP",
        "hsCode": "64041100",
        "customsValue": 649.75,
        "customsCurrency": "DKK",
        "importerOfRecord": "NO123456789",
        "exporterVatNumber": "12345678",
        "shipmentType": "B2B"
      }
    }
  }'
```

Note: alcohol (HS chapters 22xx) and tobacco (HS chapters 24xx) require a special import permit for Norway. The gateway rejects these at validation time:

```json
{ "error": "validation failed", "details": "HS code 220410 (chapter 22) requires a special import permit for Norway" }
```

#### Shipping within the EU (e.g. DK → SE, DK → DE, DK → FI)

The EU applies a de minimis threshold of **150 EUR** for B2C shipments. Below this value no customs declaration is required and the parcel moves freely. Above it, an HS code is required.

For B2B shipments a valid `importerVatNumber` is always required — this is what allows the seller to zero-rate VAT on the invoice.

```bash
# B2C below de minimis — no customs block needed at all
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "postnord",
    "shipment": {
      "sender": { ... },
      "receiver": { "country": "SE", ... },
      "totalWeight": 0.3,
      "colli": [...]
    }
  }'

# B2B — importer VAT number required
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "gls",
    "shipment": {
      "sender": { "country": "DK", ... },
      "receiver": { "country": "DE", ... },
      "totalWeight": 3.0,
      "colli": [...],
      "customs": {
        "customsValue": 299.0,
        "customsCurrency": "EUR",
        "hsCode": "95066290",
        "importerVatNumber": "DE123456789",
        "shipmentType": "B2B"
      }
    }
  }'
```

#### Åland Islands (AX)

The Åland Islands are Finnish territory but are outside the EU VAT area. Shipments to `AX` are always rejected with a hard error — contact your carrier directly for Åland routing.

#### Currency mismatch and flaggedForReview

De minimis thresholds are defined in specific currencies (NOK for Norway, EUR for the EU). If you provide a customs value in a different currency the gateway cannot determine whether the threshold applies without a live exchange rate. The shipment is accepted and booked, but `flaggedForReview: true` is set in the response so your system knows to verify the customs declaration manually.

```json
{
  "trackingNumber": "PN482910123DK",
  "carrier": "postnord",
  "status": "booked",
  "flaggedForReview": true
}
```

Apache 2.0 — see [LICENSE](LICENSE).
