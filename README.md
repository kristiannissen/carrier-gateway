# logistics-gateway

A stateless Go microservice for booking, tracking, and returning shipments across multiple Nordic and European carriers through a single consistent API. Change the `carrier` field in your request and the rest of your integration stays the same.

---

## Supported carriers

| Key | Carrier | Countries | Booking | Tracking | Returns | Labels | Status |
|---|---|---|---|---|---|---|---|
| `postnord` | PostNord | DK, SE, NO, FI | ✅ | ✅ | ✅ | PDF, ZPL, ZPLGK | Production |
| `bring` | Bring | NO, SE, DK, FI | ✅ | ✅ | ✅ | PDF | Production |
| `gls` | GLS | DK, SE, DE, NL, and more | ✅ | ✅ | ❌ | PDF, ZPL, ZPLGK | Production |
| `dao` | DAO | DK | ✅ | ✅ | ✅ | PDF | Production |
| `posti` | Posti | FI | ✅ | ✅ | ❌ | PDF | Production |
| `inpost` | InPost | PL | ✅ | ✅ | ❌ | PDF | Production |

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
| `BRING_CUSTOMER_ID` | Mybring login email | No | — |
| `BRING_CUSTOMER_NUMBER` | Bring customer account number (for billing) | No | — |
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
BRING_CUSTOMER_ID=your-mybring-email
BRING_CUSTOMER_NUMBER=your-bring-customer-number
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

#### Shipment fields

| Field | Type | Description | Required |
|---|---|---|---|
| `carrier` | string | Carrier key (`postnord`, `bring`, `gls`, `dao`, `posti`, `inpost`) | Yes |
| `shipment.sender` | object | Sender address | Yes |
| `shipment.receiver` | object | Receiver address | Yes |
| `shipment.totalWeight` | float | Total shipment weight in kg — must equal the sum of all colli weights | Yes |
| `shipment.colli` | array | Array of packages | Yes |
| `shipment.deliveryType` | string | `home`, `business`, `servicepoint`, or `return` | No |
| `shipment.returnFunctionality` | string | For return bookings: `standard` or `labelless` (PostNord) / `withlabel` or `labelless` (DAO). Defaults to `standard`/`labelless`. | No |
| `shipment.addOns` | array | Optional service add-ons — see [Add-ons](#add-ons) | No |
| `shipment.customs` | object | Customs declaration — required for non-EU destinations | No |
| `idempotencyKey` | string | Deduplication key, max 64 characters | No |

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
| `servicePointId` | string | Carrier service point ID — when set, street/city/postalCode are optional for the receiver | No |
| `phone` | string | Phone number | No |
| `email` | string | Email address | No |

\* Not required when `servicePointId` is set on the receiver address.

#### Colli fields

| Field | Type | Description | Required |
|---|---|---|---|
| `id` | string | Unique identifier for this package | Yes |
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
      "deliveryType": "home",
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
              "value": 129.95
            }
          ]
        }
      ]
    },
    "idempotencyKey": "order-98765"
  }'
```

#### Successful response

```json
{
  "trackingNumber": "00073215400599388772",
  "labelUrl": "",
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

For PostNord and GLS, the label is returned inline as base64 in `colli[0].labelUrl`. For Bring, a URL is returned in `labelUrl`. For DAO, labels are fetched separately via `GET /api/labels`.

---

### Service point delivery

Set `deliveryType: "servicepoint"` and `servicePointId` on the receiver. The gateway maps `servicePointId` to the correct carrier-specific wire field:

| Carrier | Wire field | Notes |
|---|---|---|
| PostNord | `parties.deliveryParty.partyIdentification.partyId` with `partyIdType: "156"` | Service code `19` (MyPack Collect) |
| Bring | `recipient.pickupPointId` | Product `PICKUP_PARCEL` |
| GLS | `Service[ShopDelivery].ParcelShopID` | |
| DAO | `shopid` | Routes to `/DAOPakkeshop/leveringsordre.php` |
| Posti | `pickupPointId` | |
| InPost | `service.targetLocker` | |

```bash
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "postnord",
    "shipment": {
      "sender": { ... },
      "receiver": {
        "name": "Anna Svensson",
        "country": "SE",
        "phone": "+46701234567",
        "email": "anna@example.com",
        "servicePointId": "9814"
      },
      "deliveryType": "servicepoint",
      "totalWeight": 1.0,
      "colli": [...]
    }
  }'
```

---

### Return bookings

Set `deliveryType: "return"` to book a return shipment. The caller provides `sender` (the customer returning the parcel) and `receiver` (the merchant) — the gateway does not swap addresses automatically.

| Carrier | Mechanism | Labelless option | Notes |
|---|---|---|---|
| PostNord | Separate endpoint `/rest/shipment/v3/returns/edi/labels/pdf` | Yes — `returnFunctionality: "labelless"` | QR code sent via SMS/email add-ons |
| Bring | `returnProduct.id: "9350"` added to standard booking | No | Single API call returns both outgoing and return labels |
| GLS | ❌ Not supported | — | Returns an error |
| DAO | Separate endpoint `/DAOPakkeshop/returordre.php` | Yes — `returnFunctionality: "labelless"` (default) | Labelless code returned in `colli[0].labelUrl` |

```bash
# PostNord standard return (customer prints label at service point)
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "postnord",
    "shipment": {
      "sender": {
        "name": "Anna Svensson",
        "street": "Storgatan 1",
        "city": "Stockholm",
        "postalCode": "11122",
        "country": "SE",
        "phone": "+46701234567",
        "email": "anna@example.com"
      },
      "receiver": {
        "name": "Unisport Group",
        "street": "Industrivej",
        "houseNumber": "10",
        "city": "Copenhagen",
        "postalCode": "2300",
        "country": "DK"
      },
      "deliveryType": "return",
      "returnFunctionality": "standard",
      "totalWeight": 1.0,
      "colli": [...]
    }
  }'

# PostNord labelless return (customer writes code on parcel)
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "postnord",
    "shipment": {
      ...
      "deliveryType": "return",
      "returnFunctionality": "labelless",
      "addOns": [
        { "type": "sms_notification" }
      ]
    }
  }'

# DAO return — labelless by default
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "dao",
    "shipment": {
      "sender": { "name": "Customer", ... },
      "receiver": { "name": "Unisport Group", ... },
      "deliveryType": "return",
      "returnFunctionality": "withlabel",
      "totalWeight": 0.5,
      "colli": [...]
    }
  }'
```

---

### Add-ons

Optional services are specified in the `addOns` array on the shipment. Each add-on has a `type` and an optional `instructions` field for flex delivery.

| Type | Description | PostNord | Bring | GLS | DAO |
|---|---|---|---|---|---|
| `sms_notification` | SMS notification to receiver | Via `contact.smsNo` | Service code `1091` | `InfoService` with `NotificationType: "SMS"` | Post-booking call to `OpdaterKontaktOplysning.php` |
| `email_notification` | Email notification to receiver | Via `contact.emailAddress` | Service code `1091` (same as SMS) | `InfoService` with `NotificationType: "EMAIL"` | Post-booking call to `OpdaterKontaktOplysning.php` |
| `flex_delivery` | Deliver without recipient present | `additionalServiceCode: "A7"` | Service code `0041` | `FlexDelivery` service | ❌ Not supported |
| `signature_required` | Recipient must sign on delivery | `additionalServiceCode: "A2"` | Service code `1131` | `DirectSignature` service | ❌ Not supported |
| `cash_on_delivery` | Collect payment on delivery. Requires `codAmount`, `codCurrency`, `codAccountNumber` on the add-on. | ❌ Not supported | Service code `1000` + `cashOnDelivery` companion object | ❌ Not supported | ❌ Not supported |
| `insurance` | Declare insured value. Requires `insuranceValue` and `insuranceCurrency` on the add-on. | `additionalServiceCode: "A8"` | ❌ Not supported | ❌ Not supported | ❌ Not supported |

Posti and InPost accept the `addOns` array but currently ignore it with a debug log — documentation for their add-on APIs is not yet available.

A few carrier-specific notes worth knowing:

PostNord notifications work differently from the others. SMS and email are contact fields on the consignee block, not discrete service codes. `sms_notification` and `email_notification` act as validation guards — if requested but the receiver has no phone or email, the booking is rejected.

DAO's two-step add-on flow has an important constraint: the `OpdaterKontaktOplysning.php` call must happen before the parcel reaches the first DAO terminal. If it fails after a successful booking, the shipment is created without notifications and a warning is logged. The booking is not rolled back.

For return bookings on PostNord, `sms_notification` and `email_notification` trigger QR code delivery — the return code is sent to the customer via SMS or email rather than requiring a label printout.

```bash
# Booking with SMS + email notification and flex delivery
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "bring",
    "shipment": {
      "sender": { ... },
      "receiver": {
        "name": "Anna Svensson",
        "phone": "+46701234567",
        "email": "anna@example.com",
        ...
      },
      "deliveryType": "home",
      "totalWeight": 1.5,
      "colli": [...],
      "addOns": [
        { "type": "sms_notification" },
        { "type": "email_notification" },
        { "type": "flex_delivery", "instructions": "Leave behind the green shed" }
      ]
    }
  }'

# Signature required (PostNord and Bring)
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "postnord",
    "shipment": {
      "sender": { ... },
      "receiver": { ... },
      "totalWeight": 2.0,
      "colli": [...],
      "addOns": [
        { "type": "signature_required" }
      ]
    }
  }'

# Cash on delivery (Bring only)
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "bring",
    "shipment": {
      "sender": { ... },
      "receiver": { ... },
      "totalWeight": 1.0,
      "colli": [...],
      "addOns": [
        {
          "type": "cash_on_delivery",
          "codAmount": 499.95,
          "codCurrency": "NOK",
          "codAccountNumber": "12345678901"
        }
      ]
    }
  }'

# Insurance / declared value (PostNord only)
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{
    "carrier": "postnord",
    "shipment": {
      "sender": { ... },
      "receiver": { ... },
      "totalWeight": 1.0,
      "colli": [...],
      "addOns": [
        {
          "type": "insurance",
          "insuranceValue": 5000.0,
          "insuranceCurrency": "DKK"
        }
      ]
    }
  }'
```

---

### GET /api/labels/{trackingNumber}

Fetches the shipping label for a booked shipment and returns it as base64-encoded data.

| Query parameter | Description | Default |
|---|---|---|
| `carrier` | Carrier key | `postnord` |
| `format` | Label format: `PDF`, `PNG`, `ZPL`, `EPL`, `ZPLGK` | `PDF` |

#### Label format support by carrier

| Carrier | PDF | ZPL | ZPLGK | PNG | EPL |
|---|---|---|---|---|---|
| PostNord | ✅ | ✅ | ✅ | — | — |
| Bring | ✅ | — | — | — | — |
| GLS | ✅ | ✅ | ✅ | — | — |
| DAO | ✅ | — | — | — | — |
| Posti | ✅ | — | — | — | — |
| InPost | ✅ | — | — | — | — |

```bash
# Fetch a PDF label
curl "http://localhost:8080/api/labels/00073215400599388772?carrier=postnord"

# Fetch a ZPL label for a thermal printer
curl "http://localhost:8080/api/labels/00073215400599388772?carrier=postnord&format=ZPL"

# Decode and save
curl -s "http://localhost:8080/api/labels/00073215400599388772?carrier=postnord" \
  | jq -r '.data' \
  | base64 -d > label.pdf

# Send directly to a ZPL printer
curl -s "http://localhost:8080/api/labels/00073215400599388772?carrier=postnord&format=ZPL" \
  | jq -r '.data' \
  | base64 -d > /dev/usb/lp0
```

#### Response

```json
{
  "trackingNumber": "00073215400599388772",
  "carrier": "postnord",
  "format": "PDF",
  "data": "JVBERi0xLj...",
  "mimeType": "application/pdf"
}
```

---

### GET /api/trackings/{trackingNumber}

```bash
curl "http://localhost:8080/api/trackings/00073215400599388772?carrier=postnord"
```

#### Response

```json
{
  "trackingNumber": "00073215400599388772",
  "carrier": "postnord",
  "status": "INFORMED",
  "estimatedDelivery": "",
  "events": [
    {
      "timestamp": "2026-06-07T18:37:36",
      "status": "INFORMED",
      "location": "PostNord",
      "details": "We have received a notification from your shipper that they are preparing an item for you."
    }
  ]
}
```

Tracking endpoints and response shapes per carrier:

| Carrier | Endpoint | Method | Status field |
|---|---|---|---|
| PostNord | `/rest/shipment/v5/trackandtrace/findByIdentifier.json` | GET | `status` string |
| Bring | `/tracking/api/v2/tracking.json` | GET | `statusDescription` |
| GLS | `/rs/tracking/parceldetails` | POST | Most recent `StatusCode` |
| DAO | `/TrackNTrace_v2.php` | GET | Most recent event description |

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

## Idempotency

Pass `idempotencyKey` in the request body to deduplicate bookings. Keys must be 64 characters or fewer.

```bash
curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: order-98765-attempt-1" \
  -d '{ "carrier": "postnord", "shipment": { ... }, "idempotencyKey": "order-98765-attempt-1" }'
```

| Carrier | Native idempotency | Behaviour |
|---|---|---|
| PostNord | Yes | Key forwarded as `shipmentReference`; server-side deduplication |
| Bring | No | Key logged as `clientReference`; deduplication is your responsibility |
| GLS | No | Key logged; deduplication is your responsibility |
| DAO | No | Key logged; deduplication is your responsibility |

---

## Edge cases and validation

### Postal codes

| Country | Format | Example |
|---|---|---|
| DK | 4 digits | `2300` |
| NO | 4 digits | `0158` |
| SE | 5 digits | `11122` |
| FI | 5 digits | `00100` |
| PL | `NN-NNN` | `00-001` |

### Carrier weight and dimension limits

| Carrier | Max weight | Max dimensions |
|---|---|---|
| PostNord | 30 kg | L+W+H ≤ 300 cm |
| Bring | 30 kg | L ≤ 250 cm, W ≤ 120 cm, H ≤ 100 cm |
| GLS | 40 kg | L ≤ 270 cm, W ≤ 120 cm, H ≤ 120 cm |
| DAO | 35 kg | L ≤ 250 cm, W ≤ 120 cm, H ≤ 120 cm |
| Posti | 30 kg | L ≤ 200 cm, W ≤ 100 cm, H ≤ 100 cm |

---

## Cross-border shipments and customs

The `customs` block is optional for domestic and intra-EU B2C shipments below the de minimis threshold. It is required for everything else.

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

Norway applies a de minimis threshold of 350 NOK for B2C. The EU applies 150 EUR for B2C. B2B shipments within the EU always require a valid `importerVatNumber`.

---

## Payload logging

In development (`LOG_ENV=development`), full request and response bodies are logged at `DEBUG` level. Sensitive fields are scrubbed before any log entry is written (`Authorization` header is SHA-256 hashed; `password`, `token`, `apiKey`, and `secret` JSON fields are replaced with `[redacted]`).

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
│   │   ├── addon.go              # hasAddOn / getAddOn helpers
│   │   ├── postnord.go           # PostNord v3 EDI adapter
│   │   ├── bring.go              # Bring Booking API adapter
│   │   ├── gls.go                # GLS ShipIT Farm v1 adapter
│   │   ├── dao.go                # DAO adapter
│   │   ├── posti.go              # Posti adapter
│   │   ├── inpost.go             # InPost adapter
│   │   ├── mock_*.go             # Mock adapters for testing and MOCK_MODE
│   │   └── *_test.go             # Adapter tests
│   ├── handler/
│   │   ├── handler.go            # Shared config and helpers
│   │   ├── bookings.go           # POST /api/bookings
│   │   ├── labels.go             # GET /api/labels/{trackingNumber}
│   │   ├── trackings.go          # GET /api/trackings/{trackingNumber}
│   │   └── health.go             # GET /api/health
│   ├── middleware/
│   │   ├── request_id.go         # X-Request-ID propagation
│   │   ├── idempotency.go        # Idempotency-Key handling
│   │   └── logging.go            # Debug payload logging with scrubbing
│   ├── router/
│   │   └── router.go             # Route definitions and middleware wiring
│   ├── logger/
│   │   └── logger.go             # Zap logger constructor
│   └── validation/
│       ├── address.go            # Postal codes, house number, state/province rules
│       ├── package.go            # Per-carrier weight, dimensions, girth limits
│       ├── idempotency.go        # Idempotency key rules
│       └── customs.go            # Cross-border and de minimis validation
├── Dockerfile
├── go.mod
├── go.sum
└── README.md
```

### Running tests

```bash
# All tests with race detector
go test -race -count=1 $(go list ./... | grep -v 'cmd/' | grep -v 'internal/router')

# With coverage
go test -cover ./...
```

### Pre-commit checklist

```bash
go build ./...
go test -race -count=1 $(go list ./... | grep -v 'cmd/' | grep -v 'internal/router')
golangci-lint run
```

### Adding a carrier

1. Create `internal/adapter/{carrier}.go` implementing `CarrierAdapter`
2. Create `internal/adapter/mock_{carrier}.go`
3. Create `internal/adapter/{carrier}_test.go`
4. Add the carrier block to `InitAdapters` in `adapter.go`
5. Add a limits entry in `internal/validation/package.go`

The handler, router, and validation layer require no changes.

---

Apache 2.0 — see [LICENSE](LICENSE).
