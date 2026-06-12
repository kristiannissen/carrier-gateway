# carrier-gateway

A stateless Go microservice for booking, tracking, and returning shipments across multiple Nordic and European carriers through a single consistent API. Change the `carrier` field in your request and the rest of your integration stays the same.

---

## Supported carriers

| Key | Carrier | Countries | Booking | Tracking | Returns | Labels | Status |
|---|---|---|---|---|---|---|---|
| `postnord` | PostNord | DK, SE, NO, FI | ✅ | ✅ | ✅ | PDF, ZPL, ZPLGK | Production |
| `bring` | Bring | NO, SE, DK, FI | ✅ | ✅ | ✅ | PDF | Production |
| `gls` | GLS | DK, SE, DE, NL, and more | ✅ | ✅ | ✅ | PDF, ZPL, ZPLGK | Production |
| `dao` | DAO | DK | ✅ | ✅ | ✅ | PDF | Beta |
| `dhl` | DHL eCommerce Europe | 28 European countries | ✅ | ✅ | ✅ | PDF | Beta |
| `fedex` | FedEx | US, EU, and more | ✅ | ✅ | ❌ | — | Beta |
| `inpost` | InPost | PL | — | — | — | — | Demo |

Demo carriers return mock data only and are not connected to any live API.

---

## Quick start

```bash
git clone https://github.com/kristiannissen/carrier-gateway.git
cd carrier-gateway
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
| `DHL_CLIENT_ID` | DHL eConnect OAuth2 client ID (for booking) | No | — |
| `DHL_CLIENT_SECRET` | DHL eConnect OAuth2 client secret (for booking) | No | — |
| `DHL_CUSTOMER_ID` | DHL customerIdentification (sent in sender block) | No | — |
| `DHL_TRACKING_API_KEY` | DHL Unified Tracking API subscription key — from [developer.dhl.com](https://developer.dhl.com) | No | — |
| `FEDEX_CLIENT_ID` | FedEx OAuth2 client ID | No | — |
| `FEDEX_CLIENT_SECRET` | FedEx OAuth2 client secret | No | — |
| `FEDEX_ACCOUNT_NUMBER` | FedEx account number (sent in shipment payload) | No | — |

DHL uses `https://api.dhl.com` for both booking and tracking. Booking uses OAuth2 (client_id/client_secret); tracking uses a subscription key in the `DHL-API-Key` header. Use `MOCK_MODE=true` for testing without credentials.

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
DHL_CLIENT_ID=your-dhl-client-id
DHL_CLIENT_SECRET=your-dhl-client-secret
DHL_CUSTOMER_ID=your-dhl-customer-id
DHL_TRACKING_API_KEY=your-dhl-tracking-key
FEDEX_CLIENT_ID=your-fedex-client-id
FEDEX_CLIENT_SECRET=your-fedex-client-secret
FEDEX_ACCOUNT_NUMBER=your-fedex-account-number
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

### Security

**Rate limiting** is stateless and intentionally not enforced inside the container — in-memory counters do not survive across replicas. Apply limits at the infrastructure layer instead.

- **Traefik**: [`rateLimit` middleware](https://doc.traefik.io/traefik/middlewares/http/ratelimit/) on the router
- **nginx**: `limit_req_zone` + `limit_req` in the upstream block
- **API gateway**: per-route or per-API-key limits at the gateway level

Limit by API key rather than IP — clients are typically server-to-server and may share egress IPs.

**TLS** should be terminated at the reverse proxy. The container speaks plain HTTP; the proxy is responsible for HTTPS enforcement and `Strict-Transport-Security`.

**Security headers** (`X-Frame-Options`, `X-Content-Type-Options`, CORS) are not set by the application — they are browser-directed and irrelevant for a server-to-server API. If you expose this service to browser clients, configure those headers at the proxy.

**Request body cap**: all write endpoints (`POST`, `PATCH`) reject bodies larger than 1 MB with `413 Request Entity Too Large`.

**Webhook URLs** must use `https://` — plain HTTP webhook targets are rejected at dispatch time with an error.

---

## API reference

### Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/bookings` | Book a shipment |
| `DELETE` | `/api/bookings/{trackingNumber}` | Cancel a shipment |
| `PATCH` | `/api/bookings/{trackingNumber}` | Update a shipment |
| `GET` | `/api/trackings/{trackingNumber}` | Track a shipment |
| `POST` | `/api/trackings/{trackingNumber}` | Track and dispatch webhook on status change |
| `POST` | `/api/notifications` | Dispatch a webhook notification directly |
| `GET` | `/api/labels/{trackingNumber}` | Fetch a shipping label |
| `GET` | `/api/health` | Health check |

Every request receives an `X-Request-ID` header in the response. Pass it in your request to forward your own correlation ID.

---

### POST /api/bookings

Books a shipment with the specified carrier.

#### Shipment fields

| Field | Type | Description | Required |
|---|---|---|---|
| `carrier` | string | Carrier key (`postnord`, `bring`, `gls`, `dao`, `dhl`) | Yes |
| `shipment.sender` | object | Sender address | Yes |
| `shipment.receiver` | object | Receiver address | Yes |
| `shipment.totalWeight` | float | Total shipment weight in kg — must equal the sum of all colli weights | Yes |
| `shipment.colli` | array | Array of packages | Yes |
| `shipment.deliveryType` | string | `home`, `business`, `servicepoint`, or `return` | No |
| `shipment.returnFunctionality` | string | `standard` or `labelless` (PostNord); `withlabel` or `labelless` (DAO). Defaults vary by carrier. | No |
| `shipment.addOns` | array | Optional service add-ons — see [Add-ons](#add-ons) | No |
| `shipment.customs` | object | Customs declaration — see [Cross-border shipments](#cross-border-shipments-and-customs) | No |
| `idempotencyKey` | string | Deduplication key, max 64 characters | No |
| `notifications` | object | Webhook configuration — see [Webhook notifications](#webhook-notifications) | No |

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
| `items` | array | Line items — description, weight, quantity, value | Yes |

#### Booking response

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

All optional fields are omitted when empty. A cross-border shipment with warnings and notifications would additionally include:

```json
{
  "addOnWarnings": ["sms_notification could not be applied — DAO contact update failed"],
  "customsWarnings": ["PostNord: incoterms not forwarded — wire format pending"],
  "notificationsSent": [{ "event": "booked", "channel": "webhook", "url": "https://...", "status": "sent", "timestamp": "2026-06-11T17:00:00Z" }],
  "notificationsFailed": [],
  "cnFormType": "CN23",
  "cnDocument": "Q1VTVE9NUy..."
}
```

`shipmentId` is the carrier-level shipment identifier where available (PostNord `shipmentId`, Bring consignment number). Distinct from `trackingNumber` on some carriers.

`addOnWarnings` is populated when an add-on was requested but could not be fully applied after a successful booking (e.g. DAO contact update failure). The booking is not rolled back — the tracking number is valid.

`customsWarnings` is populated when customs data was validated but could not be forwarded to the carrier's wire format (carrier documentation pending), or when a VIES VAT lookup was unavailable and format-only validation was used instead.

`notificationsSent` and `notificationsFailed` are populated when a `notifications` block was provided in the request. Failed records should be retried via `POST /api/notifications`.

`cnFormType` is `"CN22"` or `"CN23"` when a customs declaration was auto-generated. `cnDocument` is the base64-encoded plain-text form ready for printing. Both fields are only present for cross-border non-EU shipments with a customs block.

For PostNord and GLS the label is returned inline as base64 in `colli[0].labelUrl`. For Bring a URL is returned. For DAO labels are fetched separately via `GET /api/labels`.

#### Example request

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

---

### Service point delivery

Set `deliveryType: "servicepoint"` and `servicePointId` on the receiver.

| Carrier | Wire field |
|---|---|
| PostNord | `parties.deliveryParty.partyIdentification.partyId` with `partyIdType: "156"`, service code `19` |
| Bring | `recipient.pickupPointId` |
| GLS | `Service[ShopDelivery].ParcelShopID` |
| DAO | `shopid` — routes to `/DAOPakkeshop/leveringsordre.php` |
| DHL | Recipient array `parcelshop` entry with `street1Nr=servicePointID` |

---

### Return bookings

Set `deliveryType: "return"`. Provide `sender` as the customer returning the parcel and `receiver` as the merchant — addresses are not swapped automatically.

| Carrier | Mechanism | Labelless |
|---|---|---|
| PostNord | Separate endpoint `/rest/shipment/v3/returns/edi/labels/pdf` | Yes — `returnFunctionality: "labelless"`, QR code sent via SMS/email add-on |
| Bring | `returnProduct.id: "9350"` added to standard booking | No |
| GLS | GLS Shop Returns Customer Plus API v3 — `/{app-id}/return-orders/label` | No |
| DAO | Separate endpoint `/DAOPakkeshop/returordre.php` | Yes — `returnFunctionality: "labelless"` (default), code returned in `colli[0].labelUrl` |
| DHL | `product: "ParcelEurope.return.network"` | Yes — `returnFunctionality: "labelless"`, QR code as base64 PNG (BE, BG, DE, ES, LU, PL, PT, SE only) |

---

### Add-ons

Optional services in the `addOns` array.

| Type | Description | PostNord | Bring | GLS | DAO | DHL |
|---|---|---|---|---|---|---|
| `sms_notification` | SMS to receiver | Contact field | VAS `1091` | `InfoService SMS` | Post-booking call | Not mapped |
| `email_notification` | Email to receiver | Contact field | VAS `1091` | `InfoService EMAIL` | Post-booking call | Not mapped |
| `flex_delivery` | Deliver without recipient | `A7` | VAS `0041` | `FlexDelivery` | ❌ | Not mapped |
| `signature_required` | Recipient must sign | `A2` | VAS `1131` | `DirectSignature` | ❌ | Not mapped |
| `cash_on_delivery` | Collect payment on delivery | ❌ | VAS `1000` + `cashOnDelivery` object | ❌ | ❌ | ❌ |
| `insurance` | Declared insured value | `A8` | ❌ | ❌ | ❌ | ❌ |

**DAO two-step add-ons:** SMS and email are applied via a separate `OpdaterKontaktOplysning.php` call after booking. If that call fails the shipment is created without notifications and `addOnWarnings` is populated in the response. Retry via `PATCH /api/bookings/{trackingNumber}?carrier=dao` with `phone` and `email`.

---

### DELETE /api/bookings/{trackingNumber}

| Query parameter | Required |
|---|---|
| `carrier` | Yes |

| Carrier | Support | Constraint |
|---|---|---|
| PostNord | ✅ | Before collection |
| Bring | ✅ | Before collection |
| GLS | ❌ | Contact GLS directly |
| DAO | ✅ | Before first terminal scan |
| DHL | ❌ | eConnect portal or customer service |
| FedEx | ✅ | Cancels all packages in the shipment |

```bash
curl -X DELETE "http://localhost:8080/api/bookings/00073215400599388772?carrier=postnord"
```

Response: `{"trackingNumber":"...","carrier":"postnord","status":"cancelled"}`

---

### PATCH /api/bookings/{trackingNumber}

| Query parameter | Required |
|---|---|
| `carrier` | Yes |

| Field | Type | Description |
|---|---|---|
| `phone` | string | Updated receiver phone |
| `email` | string | Updated receiver email |
| `weight` | float | Updated weight in kg |
| `servicePointId` | string | Redirect to different service point |

| Carrier | phone/email | weight | servicePointId | Notes |
|---|---|---|---|---|
| PostNord | ✅ | ❌ | ❌ | SE only per PostNord API |
| Bring | ❌ | ❌ | ❌ | Not supported |
| GLS | ❌ | ❌ | ❌ | Not supported |
| DAO | ✅ | ✅ | ✅ | Before first terminal scan |
| DHL | ❌ | ❌ | ❌ | Not supported |
| FedEx | ❌ | ❌ | ❌ | Cancel and rebook required |

```bash
curl -X PATCH "http://localhost:8080/api/bookings/00057126960000003016?carrier=dao" \
  -H "Content-Type: application/json" \
  -d '{"phone": "+4587654321", "email": "new@example.com", "weight": 2.3}'
```

---

### GET /api/trackings/{trackingNumber}

```bash
curl "http://localhost:8080/api/trackings/00073215400599388772?carrier=postnord"
```

#### Response

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

`status` is the raw carrier-specific string preserved for backward compatibility. `normalizedStatus` is a consistent value across all carriers.

`notificationsSent` and `notificationsFailed` are only present when using `POST /api/trackings/{trackingNumber}` with a `notifications` block and a status change was detected.

#### Normalized status values

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

#### Carrier status mapping notes

| Carrier | Source field | Notes |
|---|---|---|
| PostNord | `status` string | Only `INFORMED` confirmed from production. Full enum pending from PostNord support. |
| Bring | `statusId` | Full enum from Bring Tracking API YAML spec. |
| GLS | `History[0].StatusCode` | All map to `unknown` — enum not publicly documented. Pending GLS support. |
| DAO | `haendelse` numeric code | Codes 10–70 mapped. Full list at `GET /TrackNTraceKoder.php`. |
| DHL | `status.statusCode` | Fully documented: `delivered`, `failure`, `pre-transit`, `transit`, `unknown`. |
| FedEx | `latestStatusDetail.code` | Mapped from FedEx Track API v1. |

---

### POST /api/trackings/{trackingNumber}

Tracks the shipment and dispatches a webhook when the normalised status has changed since the last poll. Returns the full tracking result plus notification outcome.

#### Request body

| Field | Type | Description | Required |
|---|---|---|---|
| `carrier` | string | Carrier key | Yes |
| `previousStatus` | string | The normalised status last observed by the caller. A notification is only sent when the current status differs. | No |
| `notifications` | object | Webhook configuration. When omitted, the endpoint behaves like `GET /api/trackings/{trackingNumber}`. | No |
| `notifications.webhookUrl` | string | HTTPS endpoint that receives the payload | Yes (if notifications set) |
| `notifications.webhookSecret` | string | HMAC-SHA256 signing secret — sent in `X-Signature: sha256=...` | No |
| `notifications.events` | array | Filter which events trigger dispatch. Empty means all events. | No |

`booked` and `unknown` statuses never trigger a webhook dispatch.

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

---

### POST /api/notifications

Stateless webhook dispatch. The caller provides the shipment event and webhook configuration; the gateway dispatches and returns the full outcome. Nothing is stored server-side.

Always returns `200 OK` — check `notificationsFailed` to detect delivery failures.

#### Request body

| Field | Type | Description | Required |
|---|---|---|---|
| `trackingNumber` | string | Shipment tracking number | Yes |
| `carrier` | string | Carrier key | Yes |
| `event` | string | Lifecycle event (`booked`, `picked_up`, `in_transit`, `out_for_delivery`, `delivered`, `failed`, `returned`, `delayed`) | Yes |
| `estimatedDelivery` | string | Carrier ETA forwarded to the integrator | No |
| `delayReason` | string | Set when `event` is `delayed` | No |
| `notifications.webhook.url` | string | HTTPS endpoint | Yes |
| `notifications.webhook.secret` | string | HMAC-SHA256 signing secret | No |
| `notifications.webhook.events` | array | Event filter — empty means all | No |

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

Response:

```json
{
  "notificationsSent": [
    {
      "event": "delivered",
      "channel": "webhook",
      "url": "https://your-service.example.com/hooks/shipments",
      "status": "sent",
      "timestamp": "2026-06-11T17:00:00Z"
    }
  ],
  "notificationsFailed": []
}
```

---

### GET /api/labels/{trackingNumber}

| Query parameter | Default |
|---|---|
| `carrier` | `postnord` |
| `format` | `PDF` |

| Carrier | PDF | ZPL | ZPLGK |
|---|---|---|---|
| PostNord | ✅ | ✅ | ✅ |
| Bring | ✅ | — | — |
| GLS | ✅ | ✅ | ✅ |
| DAO | ✅ | — | — |
| DHL | ✅ | — | — |
| FedEx | ❌ | — | — |

```bash
# Decode and save as PDF
curl -s "http://localhost:8080/api/labels/00073215400599388772?carrier=postnord" \
  | jq -r '.data' | base64 -d > label.pdf

# Send ZPL directly to a thermal printer
curl -s "http://localhost:8080/api/labels/00073215400599388772?carrier=postnord&format=ZPL" \
  | jq -r '.data' | base64 -d > /dev/usb/lp0
```

---

### GET /api/health

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
    "inpost": "production"
  }
}
```

When `MOCK_MODE=true` all carriers report `"mock"` regardless of their beta status.

---

## Webhook notifications

The gateway supports event-driven webhook dispatch at three points:

1. **At booking** — add a `notifications` block to the booking request; a `booked` event is dispatched after a successful booking.
2. **At tracking** — use `POST /api/trackings/{trackingNumber}` with a `notifications` block; a notification is dispatched when `normalizedStatus` differs from `previousStatus`.
3. **On demand** — `POST /api/notifications` dispatches a webhook for any event without requiring a booking or tracking call.

#### Webhook payload

The gateway POSTs JSON to your endpoint:

```json
{
  "event": "delivered",
  "shipmentId": "1234567890",
  "trackingNumber": "00073215400599388772",
  "carrier": "postnord",
  "status": "delivered",
  "previousStatus": "out_for_delivery",
  "timestamp": "2026-06-11T17:00:00Z",
  "estimatedDelivery": "2026-06-11"
}
```

#### Security

- Webhooks are only sent to `https://` endpoints — plain HTTP URLs are rejected.
- When `webhookSecret` is set the payload is signed with HMAC-SHA256 and the signature is sent in the `X-Signature: sha256=<hex>` header.
- The event name is also sent in `X-Event-Type` for quick routing without parsing the body.

#### Event filter

Set `events` to a non-empty array to receive only specific events. Supported values: `booked`, `picked_up`, `in_transit`, `out_for_delivery`, `delivered`, `failed`, `returned`, `delayed`. An empty array means all events are dispatched.

#### Retry strategy

`notificationsFailed` records in booking and tracking responses contain the full error. Retry them via `POST /api/notifications`. The gateway does not retry internally.

---

## Idempotency

Pass `idempotencyKey` in the request body (max 64 characters) to deduplicate bookings.

| Carrier | Native idempotency | Behaviour |
|---|---|---|
| PostNord | Yes | Key forwarded as `shipmentReference`; server-side deduplication |
| Bring | No | Key logged as `clientReference`; deduplication is your responsibility |
| GLS | No | Key logged; deduplication is your responsibility |
| DAO | No | Key logged; deduplication is your responsibility |
| DHL | No | Key logged as `sender.referenceNr`; deduplication is your responsibility |
| FedEx | No | Key logged; deduplication is your responsibility |

---

## Cross-border shipments and customs

The `customs` block is required for all non-EU destinations (NO, GB, CH, US, CA, AU, JP, CN etc.) regardless of shipment type. It is optional for intra-EU B2C shipments below the de minimis threshold.

The gateway rejects a booking with a missing `customs` block when the destination is a known non-EU country — the error message names the missing fields.

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

#### Customs fields

| Field | Type | Description | Required |
|---|---|---|---|
| `incoterms` | string | Incoterms 2020 trade term (e.g. `DDP`, `DAP`, `FCA`) | Non-EU destinations |
| `transportMode` | string | `sea`, `air`, `road`, or `rail` | No |
| `hsCode` | string | 6–10 digit HS commodity code | Non-EU; EU above de minimis |
| `countryOfOrigin` | string | ISO 3166-1 alpha-2 country where goods were manufactured | No |
| `customsValue` | float | Declared value for customs | Non-EU |
| `customsCurrency` | string | ISO 4217 currency for `customsValue` | Non-EU |
| `importerOfRecord` | string | VAT or EORI number of the importer | Non-EU destinations |
| `importerVatNumber` | string | VAT number of the receiver | EU B2B |
| `exporterVatNumber` | string | VAT number of the sender | Non-EU destinations |
| `shipmentType` | string | `B2B` or `B2C` | No |

#### De minimis thresholds

| Destination | Threshold | Notes |
|---|---|---|
| EU (all countries) | 150 EUR | B2C only — above this, HS code required |
| Norway (NO) | 350 NOK | B2C only — above this, full customs required |
| United Kingdom (GB) | 135 GBP | B2C only |
| United States (US) | 800 USD | B2C only |
| Canada (CA) | 150 CAD | B2C only |
| Australia (AU) | 1000 AUD | B2C only |
| Switzerland (CH) | 65 CHF | B2C only |
| Japan (JP) | 10000 JPY | B2C only |

B2B shipments always require full customs data regardless of value.

#### Incoterms validation

Sea-only Incoterms (`FOB`, `FAS`, `CFR`, `CIF`) are rejected when `transportMode` is `air`, `road`, or `rail`. All 11 Incoterms 2020 terms are accepted.

#### VAT number validation

VAT numbers are validated against known format rules for DK, SE, FI, NO, DE, FR, NL, and PL. For EU member state VAT numbers, the gateway additionally calls the [VIES REST API](https://ec.europa.eu/taxation_customs/vies/) to confirm the number is registered and active.

VIES lookups are non-blocking: if VIES is unavailable or times out (2 second hard limit), the booking proceeds with format-only validation and a `customsWarnings` entry is added to the response. Both importer and exporter VAT numbers are checked in parallel so the total overhead is one round trip, not two. Non-EU VAT numbers (NO, GB, CH etc.) are never sent to VIES.

#### Carrier-specific customs rules

Pre-flight validation enforces per-carrier constraints before the booking reaches the carrier API:

| Carrier | Max line items | EORI/VAT required for non-EU |
|---|---|---|
| DHL | 99 | Yes — `exporterVatNumber` required |
| PostNord | 5 | No |
| GLS | No limit (server-side validation) | No |
| Others | No limit enforced pre-flight | No |

Exceeding the item limit returns a `400` with an error message asking you to split the shipment.

#### CN22/CN23 customs declaration forms

For cross-border non-EU shipments with a customs block, the gateway auto-generates a CN22 or CN23 form after a successful booking:

- **CN22** — for shipments with a total declared value ≤ €22 and `customsCurrency: "EUR"`
- **CN23** — for all other cross-border non-EU shipments

The form is returned as `cnDocument` (base64-encoded plain text) and `cnFormType` in the booking response. Print it and attach it to the outside of the parcel alongside the shipping label.

```bash
# Decode and save the CN23 form
curl -s -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d '{ ... }' \
  | jq -r '.cnDocument' | base64 -d > customs-declaration.txt
```

#### Customs wire format support per carrier

DHL forwards `incoterms`, `hsCode`, `countryOfOrigin`, `customsValue`, and `customsCurrency` into the cPAN booking payload. PostNord, Bring, and GLS customs wire format fields are pending carrier documentation — validated customs data is accepted and logged but not forwarded; a `customsWarnings` entry describes which fields were not sent.

#### Norway-specific rules

HS codes starting with chapter `22` (alcohol) or `24` (tobacco) require a special import permit and are rejected at validation time.

---

## Restricted goods

The gateway validates item descriptions against a per-carrier prohibited and restricted goods list before forwarding the booking to the carrier.

**Blocked items** (booking rejected):

| Carrier | Examples |
|---|---|
| All | Explosives, ammunition, weapons, firearms |
| PostNord | Flammable liquids |
| Bring, GLS, DHL | Flammable liquids, dangerous goods |
| DHL | Aerosols |

**Warned items** (booking proceeds, `customsWarnings` populated):

| Carrier | Examples |
|---|---|
| All | Lithium batteries (UN3480/UN3481 labelling required) |
| Bring, DHL | Dry ice (UN1845 labelling required), perishables |
| DHL | Aerosols (blocked for eCommerce Europe), alcohol/wine |
| DAO | Alcohol (age verification compliance required) |

Item matching is case-insensitive substring matching against `colli[].items[].description`.

In addition to carrier rules, the gateway also checks **destination country restrictions**. Certain goods are blocked or warned based on the receiver country regardless of carrier — for example, some countries prohibit specific categories of goods by import law. Destination-level blocks return a `400`; destination-level warnings populate `customsWarnings` in the response.

---

## Payload logging

In development (`LOG_ENV=development`), full request and response bodies are logged at `DEBUG` level. Sensitive fields are scrubbed before writing (`Authorization` header is SHA-256 hashed; `password`, `token`, `apiKey`, and `secret` JSON fields are replaced with `[redacted]`).

---

## Development

### Project structure

```
carrier-gateway/
├── cmd/
│   └── api/
│       └── main.go
├── internal/
│   ├── adapter/
│   │   ├── adapter.go        # CarrierAdapter interface, Registry, shared types,
│   │   │                     # TrackingStatus constants, BookingResponse fields
│   │   ├── addon.go          # hasAddOn / getAddOn helpers
│   │   ├── status.go         # normalizeStatus — carrier raw → TrackingStatus mapping
│   │   ├── customs.go        # Customs wire-format helpers
│   │   ├── postnord.go
│   │   ├── bring.go
│   │   ├── gls.go
│   │   ├── dao.go
│   │   ├── dhl.go
│   │   ├── fedex.go          # FedEx Ship/Track API — book, track, cancel
│   │   ├── inpost.go         # Demo — mock only
│   │   ├── mock_*.go
│   │   └── *_test.go
│   ├── handler/
│   │   ├── handler.go
│   │   ├── bookings.go       # POST /api/bookings — restricted items check,
│   │   │                     # mandatory customs enforcement, VIES parallel calls
│   │   ├── cancellations.go
│   │   ├── updates.go
│   │   ├── labels.go
│   │   ├── trackings.go      # GET + POST /api/trackings — track and notify
│   │   ├── notifications.go  # POST /api/notifications — stateless webhook dispatch
│   │   └── health.go
│   ├── middleware/
│   │   ├── request_id.go
│   │   ├── idempotency.go
│   │   └── logging.go
│   ├── customs/
│   │   └── cn_forms.go       # Stateless CN22/CN23 form generation
│   ├── notification/
│   │   ├── notification.go   # Event types, Payload, Preferences, Record
│   │   ├── service.go        # Dispatch — fan-out to configured channels
│   │   └── webhook.go        # HTTPSender — HTTPS enforcement, HMAC signing, X-Event-Type
│   ├── router/
│   │   └── router.go
│   ├── logger/
│   │   └── logger.go
│   └── validation/
│       ├── address.go        # Postal codes, house number, state/province rules
│       ├── package.go        # Per-carrier weight, dimension, girth limits
│       ├── idempotency.go    # Idempotency key rules
│       ├── customs.go          # Cross-border, de minimis, VAT format, VIES live lookup
│       ├── carrier_customs.go  # Per-carrier item limits and EORI requirements
│       ├── countries.go        # EU / European country sets
│       └── restricted.go       # Per-carrier and per-destination prohibited goods
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
4. Add the carrier block to `InitAdapters` in `adapter.go` (env vars + fallback to mock)
5. Add status mappings in `status.go`
6. Add restricted goods entries in `validation/restricted.go`
7. Add a limits entry in `validation/package.go`
8. Wire customs fields in `adapter/customs.go` if the carrier supports cross-border
9. Add a `carrierCustomsRules` entry in `validation/carrier_customs.go` with item limits and EORI requirements

The handler, router, and validation layer require no other changes.

---

Apache 2.0 — see [LICENSE](LICENSE).
