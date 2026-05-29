# Multi-Carrier Integration Service

A **stateless, modular Go microservice** for integrating with multiple logistics carriers (PostNord, FedEx, DHL). Supports **synchronous operations**, **webhooks**, **colli (multi-package) shipments**, and **environment-based authentication**. Designed for **open-source distribution** under Apache 2.0.

---

## **🚀 Features**
- **Multi-Carrier Support**: PostNord, FedEx, DHL (extensible to others).
- **Colli (Multi-Package) Shipments**: Supports both single-package and multi-package shipments.
- **Idempotency**: Supported for FedEx; ignored for PostNord/DHL (with warnings).
- **Webhooks**: Callback URLs for tracking updates.
- **Stateless Design**: No persistence layer (async job tracking via external storage).
- **First-Class CLI**: Full-featured CLI with the same capabilities as the API.
- **Mock Mode**: Test without API keys using predefined mock responses.
- **Docker & Vercel-Compatible**: Easy deployment as a serverless function or container.

---

## **📌 Table of Contents**
1. [Installation](#installation)
2. [Configuration](#configuration)
3. [API Reference](#api-reference)
4. [CLI Reference](#cli-reference)
5. [Colli (Multi-Package) Shipments](#colli-multi-package-shipments)
6. [Carrier-Specific Notes](#carrier-specific-notes)
7. [Examples](#examples)
8. [Development](#development)
9. [Testing](#testing)
10. [Deployment](#deployment)
11. [Contributing](#contributing)
12. [License](#license)

---

## **💻 Installation**

### **Prerequisites**
- Go 1.21+
- Docker (optional, for local development)
- API keys for carriers (PostNord, FedEx, DHL)

### **Clone the Repository**
```bash
git clone https://github.com/kristiannissen/logistics-gateway.git
cd logistics-gateway
```

### **Install Dependencies**
```bash
go mod download
```

---

## **⚙️ Configuration**

### **Environment Variables**
| Variable               | Description                          | Required | Default       |
|------------------------|--------------------------------------|----------|---------------|
| `PORT`                 | Port for the HTTP server             | No       | `8080`        |
| `API_KEY`              | API key for the service              | Yes      | -             |
| `POSTNORD_API_KEY`     | PostNord API key                     | No       | -             |
| `FED_EX_CLIENT_ID`     | FedEx OAuth 2.0 Client ID             | No       | -             |
| `FED_EX_CLIENT_SECRET`| FedEx OAuth 2.0 Client Secret         | No       | -             |
| `FED_EX_ACCOUNT_NUMBER`| FedEx Account Number                 | No       | -             |
| `DHL_API_KEY`          | DHL API key                          | No       | -             |
| `MOCK_MODE`            | Force mock mode (no API calls)       | No       | `false`       |
| `ENVIRONMENT`          | Environment (e.g., `development`)     | No       | `development` |

### **Mock Mode Rules**
- If **no carrier API keys** are set, the service runs in **mock mode**.
- If `MOCK_MODE=true`, the service runs in **mock mode** regardless of API keys.
- Mock mode logs warnings (e.g., `"Running in mock mode; set POSTNORD_API_KEY for production"`).

### **Example `.env` File**
```env
PORT=8080
API_KEY=your-api-key
POSTNORD_API_KEY=your-postnord-api-key
FED_EX_CLIENT_ID=your-fedex-client-id
FED_EX_CLIENT_SECRET=your-fedex-client-secret
FED_EX_ACCOUNT_NUMBER=your-fedex-account-number
DHL_API_KEY=your-dhl-api-key
MOCK_MODE=false
ENVIRONMENT=development
```

---

## **🌐 API Reference**

### **Base URL**
- Local: `http://localhost:8080/api`
- Vercel: `https://your-app.vercel.app/api`

### **Endpoints**
| Method | Endpoint                          | Description                          |
|--------|-----------------------------------|--------------------------------------|
| POST   | `/api/bookings`                   | Book a shipment (single/multi-colli). |
| GET    | `/api/trackings/{trackingNumber}` | Track a shipment.                     |
| GET    | `/api/service-points`             | Get service points (pickup locations). |
| GET    | `/api/health`                     | Health check.                         |

---

### **1. Book a Shipment**
**Endpoint**: `POST /api/bookings`

#### **Request Body (JSON)**
```json
{
  "carrier": "postnord",
  "shipment": {
    "sender": {
      "name": "Kristian Nissen Logistics",
      "street": "Industrivej 10",
      "city": "Copenhagen",
      "postalCode": "2300",
      "country": "DK",
      "phone": "+4512345678",
      "email": "contact@kristiannissen.dk"
    },
    "receiver": {
      "name": "Receiver AB",
      "street": "Storgatan 1",
      "city": "Stockholm",
      "postalCode": "111 22",
      "country": "SE",
      "phone": "+46123456789"
    },
    "totalWeight": 15.5,
    "colli": [
      {
        "id": "colli_1",
        "reference": "BOX-001",
        "weight": 5.0,
        "dimensions": {
          "length": 30.0,
          "width": 20.0,
          "height": 10.0
        },
        "items": [
          {
            "description": "T-Shirt (Blue, L)",
            "weight": 0.5,
            "quantity": 5,
            "value": 25.0,
            "sku": "TSHIRT-BLUE-L"
          }
        ]
      },
      {
        "id": "colli_2",
        "reference": "BOX-002",
        "weight": 10.5,
        "dimensions": {
          "length": 50.0,
          "width": 40.0,
          "height": 20.0
        },
        "items": [
          {
            "description": "Winter Jacket",
            "weight": 2.0,
            "quantity": 1,
            "value": 150.0,
            "sku": "JACKET-WINTER-01"
          }
        ]
      }
    ]
  },
  "callbackUrl": "https://api.kristiannissen.dk/webhooks/tracking",
  "idempotencyKey": "unique-key-123",
  "incoterms": "DDP",
  "hsCode": "61091000"
}
```

#### **Request Fields**
| Field               | Type          | Description                                                                                     | Required |
|---------------------|---------------|-------------------------------------------------------------------------------------------------|----------|
| `carrier`           | `string`      | Carrier name (`postnord`, `fedex`, `dhl`).                                                     | Yes      |
| `shipment`          | `object`      | Shipment details (see below).                                                                   | Yes      |
| `callbackUrl`       | `string`      | URL for tracking updates (webhook).                                                           | No       |
| `idempotencyKey`    | `string`      | Unique key for idempotency (supported by FedEx; ignored by PostNord/DHL).                      | No       |
| `incoterms`         | `string`      | Incoterms (e.g., `DDP`, `DAP`) for customs/duty handling.                                       | No       |
| `hsCode`            | `string`      | Harmonized System (HS) code for customs classification.                                       | No       |

#### **Shipment Fields**
| Field               | Type          | Description                                                                                     | Required |
|---------------------|---------------|-------------------------------------------------------------------------------------------------|----------|
| `sender`            | `object`      | Sender address (see [Address](#address)).                                                      | Yes      |
| `receiver`          | `object`      | Receiver address (see [Address](#address)).                                                    | Yes      |
| `totalWeight`       | `float`       | Total weight of all colli combined (kg).                                                       | Yes      |
| `colli`            | `array`       | List of individual packages (colli). See [Colli](#colli).                                      | Yes      |

#### **Address Fields**
| Field               | Type     | Description                     | Required |
|---------------------|----------|---------------------------------|----------|
| `name`              | `string` | Name of the contact.            | Yes      |
| `street`            | `string` | Street address.                 | Yes      |
| `city`              | `string` | City.                           | Yes      |
| `postalCode`        | `string` | Postal code.                    | Yes      |
| `country`           | `string` | Country code (ISO 3166-1 alpha-2). | Yes      |
| `phone`             | `string` | Phone number.                   | No       |
| `email`             | `string` | Email address.                  | No       |

#### **Colli Fields**
| Field               | Type          | Description                                                                                     | Required |
|---------------------|---------------|-------------------------------------------------------------------------------------------------|----------|
| `id`                | `string`      | Unique identifier for the colli (e.g., `colli_1`).                                             | Yes      |
| `reference`         | `string`      | Optional reference (e.g., barcode).                                                             | No       |
| `weight`            | `float`       | Weight of this colli (kg).                                                                     | Yes      |
| `dimensions`        | `object`      | Dimensions (`length`, `width`, `height` in cm).                                                 | No       |
| `items`             | `array`       | List of items in this colli. See [Item](#item).                                                | Yes      |

#### **Item Fields**
| Field               | Type     | Description                     | Required |
|---------------------|----------|---------------------------------|----------|
| `description`       | `string` | Description of the item.        | Yes      |
| `weight`            | `float`  | Weight of the item (kg).        | Yes      |
| `quantity`          | `int`    | Quantity of the item.           | Yes      |
| `value`             | `float`  | Value of the item (for customs).| No       |
| `sku`               | `string` | Stock Keeping Unit (SKU).       | No       |

#### **Response (JSON)**
```json
{
  "shipmentId": "550e8400-e29b-41d4-a716-446655440000",
  "trackingNumber": "PN123456789DK",
  "labelUrl": "https://api.postnord.com/labels/550e8400-e29b-41d4-a716-446655440000.pdf",
  "carrier": "postnord",
  "cost": 125.50,
  "currency": "DKK",
  "serviceLevel": "Standard",
  "status": "booked",
  "colli": [
    {
      "id": "colli_1",
      "reference": "BOX-001",
      "trackingNumber": "PN123456789DK-1",
      "labelUrl": "https://api.postnord.com/labels/550e8400-e29b-41d4-a716-446655440000-1.pdf",
      "status": "booked"
    },
    {
      "id": "colli_2",
      "reference": "BOX-002",
      "trackingNumber": "PN123456789DK-2",
      "labelUrl": "https://api.postnord.com/labels/550e8400-e29b-41d4-a716-446655440000-2.pdf",
      "status": "booked"
    }
  ]
}
```

---

### **2. Track a Shipment**
**Endpoint**: `GET /api/trackings/{trackingNumber}?carrier={carrier}`

#### **Query Parameters**
| Parameter   | Type     | Description                     | Required | Default   |
|-------------|----------|---------------------------------|----------|-----------|
| `carrier`   | `string` | Carrier name (`postnord`, `fedex`, `dhl`). | No       | `postnord` |

#### **Response (JSON)**
```json
{
  "shipmentId": "550e8400-e29b-41d4-a716-446655440000",
  "trackingNumber": "PN123456789DK",
  "carrier": "postnord",
  "status": "In Transit",
  "estimatedDelivery": "2026-06-02",
  "events": [
    {
      "timestamp": "2026-05-28T14:30:00Z",
      "status": "Picked Up",
      "location": "Copenhagen, DK",
      "details": "Package picked up at sender location"
    }
  ],
  "colli": [
    {
      "id": "colli_1",
      "reference": "BOX-001",
      "trackingNumber": "PN123456789DK-1",
      "status": "In Transit",
      "events": [
        {
          "timestamp": "2026-05-28T14:30:00Z",
          "status": "Picked Up",
          "location": "Copenhagen, DK",
          "details": "Package picked up at sender location"
        }
      ]
    }
  ]
}
```

---

### **3. Get Service Points**
**Endpoint**: `GET /api/service-points?city={city}&postalCode={postalCode}&country={country}&carrier={carrier}`

#### **Query Parameters**
| Parameter    | Type     | Description                     | Required | Default   |
|--------------|----------|---------------------------------|----------|-----------|
| `city`       | `string` | City name.                      | Yes      | -         |
| `postalCode` | `string` | Postal code.                    | No       | -         |
| `country`    | `string` | Country code (ISO 3166-1 alpha-2). | Yes      | -         |
| `carrier`    | `string` | Carrier name (`postnord`, `fedex`, `dhl`). | No       | `postnord` |

#### **Response (JSON)**
```json
[
  {
    "id": "sp_123",
    "name": "PostNord Copenhagen",
    "address": {
      "name": "PostNord Copenhagen",
      "street": "Main Street 1",
      "city": "Copenhagen",
      "postalCode": "12345",
      "country": "DK"
    },
    "openingHours": "09:00-17:00",
    "services": ["Pickup", "Dropoff"]
  }
]
```

---

## **🖥️ CLI Reference**

### **Installation**
1. **Build the CLI**:
   ```bash
   go build -o logistics-gateway cmd/cli/main.go
   ```

2. **Install Globally**:
   ```bash
   go install cmd/cli/main.go
   ```

3. **Run the CLI**:
   ```bash
   ./logistics-gateway --help
   ```

---

### **Commands**
| Command               | Description                          |
|-----------------------|--------------------------------------|
| `logistics-gateway book` | Book a shipment.                     |
| `logistics-gateway track` | Track a shipment.                    |
| `logistics-gateway service-points` | Get service points.          |
| `logistics-gateway health` | Health check.                       |

---

### **1. Book a Shipment**
**Command**: `logistics-gateway book [flags]`

#### **Flags**
| Flag               | Shorthand | Description                          | Required | Default   |
|--------------------|-----------|--------------------------------------|----------|-----------|
| `--carrier`        | `-c`      | Carrier (e.g., `postnord`, `fedex`, `dhl`). | Yes      | -         |
| `--input`          | `-i`      | Input JSON file (default: stdin).     | No       | -         |
| `--output`         | `-o`      | Output format (`json` or `text`).    | No       | `text`    |
| `--async`          | `-a`      | Enable async booking (if supported). | No       | `false`   |

#### **Examples**
```bash
# From a file (JSON output)
logistics-gateway book --carrier postnord --input shipment.json

# From stdin (text output)
echo '{
  "shipment": {
    "sender": { "name": "Sender", "street": "Street", "city": "Copenhagen", "postalCode": "2300", "country": "DK" },
    "receiver": { "name": "Receiver", "street": "Street", "city": "Stockholm", "postalCode": "111 22", "country": "SE" },
    "totalWeight": 5.0,
    "colli": [{ "id": "colli_1", "weight": 5.0, "items": [{ "description": "Item 1", "weight": 1.0, "quantity": 1 }] }]
  }
}' | logistics-gateway book --carrier postnord --output text
```

#### **Output (Text)**
```
Shipment ID: shipment_123
Tracking Number: PN123456789DK
Label URL: https://mock.postnord.com/labels/PN123456789DK.pdf
Carrier: postnord
Cost: 125.50 DKK
Service Level: Standard
Status: booked

Colli:
  - ID: colli_1, Reference: BOX-001, Tracking: PN123456789DK-1, Status: booked
  - ID: colli_2, Reference: BOX-002, Tracking: PN123456789DK-2, Status: booked
```

---

### **2. Track a Shipment**
**Command**: `logistics-gateway track [flags]`

#### **Flags**
| Flag               | Shorthand | Description                          | Required | Default   |
|--------------------|-----------|--------------------------------------|----------|-----------|
| `--tracking-number`| `-t`      | Tracking number.                     | Yes      | -         |
| `--carrier`        | `-c`      | Carrier (e.g., `postnord`, `fedex`, `dhl`). | No       | `postnord` |
| `--output`         | `-o`      | Output format (`json` or `text`).    | No       | `text`    |

#### **Examples**
```bash
# Track a shipment (JSON output)
logistics-gateway track --tracking-number PN123456789DK

# Track with FedEx (text output)
logistics-gateway track --tracking-number FX123456789US --carrier fedex --output text
```

#### **Output (Text)**
```
Shipment ID: shipment_123
Tracking Number: PN123456789DK
Carrier: postnord
Status: In Transit
Estimated Delivery: 2026-06-02

Events:
  - 2026-05-28T14:30:00Z: Picked Up (Copenhagen, DK)
    Details: Package picked up at sender location

Colli:
  - ID: colli_1, Tracking: PN123456789DK-1, Status: In Transit
    Events:
      - 2026-05-28T14:30:00Z: Picked Up (Copenhagen, DK)
```

---

### **3. Get Service Points**
**Command**: `logistics-gateway service-points [flags]`

#### **Flags**
| Flag               | Shorthand | Description                          | Required | Default   |
|--------------------|-----------|--------------------------------------|----------|-----------|
| `--city`           | -         | City name.                           | Yes      | -         |
| `--postal-code`    | -         | Postal code.                         | No       | -         |
| `--country`        | -         | Country code (e.g., `DK`).           | Yes      | -         |
| `--carrier`        | `-c`      | Carrier (e.g., `postnord`, `fedex`, `dhl`). | No       | `postnord` |
| `--output`         | `-o`      | Output format (`json` or `text`).    | No       | `text`    |

#### **Examples**
```bash
# Get service points in Copenhagen (JSON output)
logistics-gateway service-points --city Copenhagen --country DK

# Get service points in Aarhus (text output)
logistics-gateway service-points --city Aarhus --country DK --output text
```

#### **Output (Text)**
```
Service Points for Copenhagen,  (DK):

1. PostNord Copenhagen (ID: sp_123)
   Address: Main Street 1, Copenhagen, 12345 DK
   Opening Hours: 09:00-17:00
   Services: [Pickup Dropoff]

2. PostNord Aarhus (ID: sp_456)
   Address: Second Street 2, Aarhus, 8000 DK
   Opening Hours: 08:00-16:00
   Services: [Pickup]
```

---

### **4. Health Check**
**Command**: `logistics-gateway health`

#### **Output**
```
CLI is healthy!
Available carriers: postnord, fedex, dhl
Available commands: book, track, service-points, health
```

---

## **📦 Colli (Multi-Package) Shipments**

### **What is a Colli?**
A **colli** (or **multi-package shipment**) is a shipment that consists of **multiple individual packages**. This is useful for:
- Splitting a large order into multiple boxes.
- Shipping bulky or irregularly shaped items separately.
- Mixing different types of items (e.g., pallets + cartons).

### **How It Works**
1. **Request**: Include a list of `colli` in the `Shipment` object. Each colli can have its own:
   - `id` (unique identifier)
   - `weight`
   - `dimensions`
   - `items` (list of items in the colli)
2. **Response**: The API/CLI returns:
   - A **parent tracking number** for the entire shipment.
   - **Individual tracking numbers** for each colli (if supported by the carrier).
   - **Label URLs** for each colli (or a single label for the entire shipment).

### **Example: Single-Package Shipment (1 Colli)**
```json
{
  "carrier": "postnord",
  "shipment": {
    "sender": { "name": "Sender", "street": "Street", "city": "Copenhagen", "postalCode": "2300", "country": "DK" },
    "receiver": { "name": "Receiver", "street": "Street", "city": "Stockholm", "postalCode": "111 22", "country": "SE" },
    "totalWeight": 5.0,
    "colli": [
      {
        "id": "colli_1",
        "weight": 5.0,
        "items": [ { "description": "T-Shirt", "weight": 0.5, "quantity": 5 } ]
      }
    ]
  }
}
```

### **Example: Multi-Package Shipment (2+ Colli)**
```json
{
  "carrier": "postnord",
  "shipment": {
    "sender": { "name": "Sender", "street": "Street", "city": "Copenhagen", "postalCode": "2300", "country": "DK" },
    "receiver": { "name": "Receiver", "street": "Street", "city": "Stockholm", "postalCode": "111 22", "country": "SE" },
    "totalWeight": 15.0,
    "colli": [
      {
        "id": "colli_1",
        "weight": 5.0,
        "items": [ { "description": "T-Shirt", "weight": 0.5, "quantity": 5 } ]
      },
      {
        "id": "colli_2",
        "weight": 10.0,
        "items": [ { "description": "Jacket", "weight": 2.0, "quantity": 1 } ]
      }
    ]
  }
}
```

---

## **🚚 Carrier-Specific Notes**

### **PostNord**
- **Endpoints**:
  - Production: `https://api.postnord.com`
- **Authentication**: API key in `Authorization: Bearer {api_key}` header.
- **Colli Support**: Native support for multi-package shipments.
- **Tracking**: Each colli can have its own tracking number (e.g., `PN123456789DK-1`, `PN123456789DK-2`).
- **Labels**: Individual labels for each colli or a single label for the entire shipment.
- **Idempotency**: Not supported (warnings are logged if `idempotencyKey` is provided).
- **Webhooks**: Supported via `callbackUrl`.

### **FedEx**
- **Endpoints**:
  - Production: `https://apis.fedex.com`
  - Sandbox: `https://apis.fedex.com/sandbox`
- **Authentication**: OAuth 2.0 (`client_credentials` grant type).
- **Colli Support**: Uses `packageLineItems` for multi-package shipments.
- **Tracking**: Each package can have its own tracking number.
- **Labels**: Individual labels for each package.
- **Idempotency**: Supported via `x-customer-transaction-id` header.
- **Webhooks**: Supported via `notificationUrl`.

### **DHL**
- **Endpoints**: TBD (e.g., `https://api.dhl.com`).
- **Authentication**: TBD (e.g., API key or OAuth 2.0).
- **Colli Support**: Uses `pieces` or `packages` for multi-package shipments.
- **Tracking**: Each piece can have its own tracking number.
- **Labels**: Individual labels for each piece.
- **Idempotency**: Not supported (warnings are logged if `idempotencyKey` is provided).

---

## **📝 Examples**

### **API Examples**

#### **1. Book a Single-Package Shipment (PostNord)**
```bash
curl -X POST https://api.example.com/api/bookings \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "carrier": "postnord",
    "shipment": {
      "sender": {
        "name": "Kristian Nissen",
        "street": "Industrivej 10",
        "city": "Copenhagen",
        "postalCode": "2300",
        "country": "DK"
      },
      "receiver": {
        "name": "Receiver AB",
        "street": "Storgatan 1",
        "city": "Stockholm",
        "postalCode": "111 22",
        "country": "SE"
      },
      "totalWeight": 5.0,
      "colli": [
        {
          "id": "colli_1",
          "weight": 5.0,
          "items": [
            {
              "description": "T-Shirt",
              "weight": 0.5,
              "quantity": 5,
              "value": 25.0
            }
          ]
        }
      ]
    }
  }'
```

#### **2. Book a Multi-Package Shipment (FedEx)**
```bash
curl -X POST https://api.example.com/api/bookings \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "carrier": "fedex",
    "shipment": {
      "sender": {
        "name": "Kristian Nissen",
        "street": "123 Main St",
        "city": "New York",
        "postalCode": "10001",
        "country": "US",
        "phone": "+1234567890",
        "email": "sender@example.com"
      },
      "receiver": {
        "name": "Receiver Inc.",
        "street": "456 Oak Ave",
        "city": "Los Angeles",
        "postalCode": "90001",
        "country": "US",
        "phone": "+1987654321",
        "email": "receiver@example.com"
      },
      "totalWeight": 15.0,
      "colli": [
        {
          "id": "colli_1",
          "weight": 5.0,
          "dimensions": { "length": 30.0, "width": 20.0, "height": 10.0 },
          "items": [
            {
              "description": "T-Shirt",
              "weight": 0.5,
              "quantity": 5,
              "value": 25.0,
              "sku": "TSHIRT-001"
            }
          ]
        },
        {
          "id": "colli_2",
          "weight": 10.0,
          "dimensions": { "length": 50.0, "width": 40.0, "height": 20.0 },
          "items": [
            {
              "description": "Jacket",
              "weight": 2.0,
              "quantity": 1,
              "value": 100.0,
              "sku": "JACKET-001"
            }
          ]
        }
      ]
    },
    "idempotencyKey": "unique-key-123",
    "incoterms": "DDP"
  }'
```

#### **3. Track a Shipment (PostNord)**
```bash
curl -X GET https://api.example.com/api/trackings/PN123456789DK \
  -H "Authorization: Bearer your-api-key"
```

#### **4. Get Service Points (PostNord)**
```bash
curl -X GET "https://api.example.com/api/service-points?city=Copenhagen&country=DK" \
  -H "Authorization: Bearer your-api-key"
```

---

### **CLI Examples**

#### **1. Book a Shipment (Mock Mode)**
```bash
# Enable mock mode (no API keys required)
export MOCK_MODE=true

# Book a shipment from a file
logistics-gateway book --carrier postnord --input shipment.json

# Book a shipment from stdin
echo '{
  "shipment": {
    "sender": { "name": "Sender", "street": "Street", "city": "Copenhagen", "postalCode": "2300", "country": "DK" },
    "receiver": { "name": "Receiver", "street": "Street", "city": "Stockholm", "postalCode": "111 22", "country": "SE" },
    "totalWeight": 5.0,
    "colli": [{ "id": "colli_1", "weight": 5.0, "items": [{ "description": "Item 1", "weight": 1.0, "quantity": 1 }] }]
  }
}' | logistics-gateway book --carrier postnord
```

#### **2. Track a Shipment (Production Mode)**
```bash
# Set PostNord API key
export POSTNORD_API_KEY=your-postnord-api-key

# Track a shipment
logistics-gateway track --tracking-number PN123456789DK
```

#### **3. Get Service Points (FedEx)**
```bash
# Set FedEx credentials
export FED_EX_CLIENT_ID=your-client-id
export FED_EX_CLIENT_SECRET=your-client-secret
export FED_EX_ACCOUNT_NUMBER=your-account-number

# Get service points
logistics-gateway service-points --city New York --country US --carrier fedex
```

---

## **🛠️ Development**

### **Project Structure**
```bash
logistics-gateway/
├── cmd/
│   ├── api/
│   │   └── main.go              # API entry point (Vercel Serverless Function)
│   └── cli/
│       ├── main.go              # CLI root command
│       ├── book.go              # `book` subcommand
│       ├── track.go             # `track` subcommand
│       ├── servicepoints.go     # `service-points` subcommand
│       └── health.go            # `health` subcommand
├── internal/
│   ├── adapter/
│   │   ├── adapter.go           # CarrierAdapter interface + shared types
│   │   ├── postnord.go          # PostNord adapter (production)
│   │   ├── mock_postnord.go     # Mock PostNord adapter
│   │   ├── fedex.go             # FedEx adapter (production)
│   │   └── mock_fedex.go        # Mock FedEx adapter
│   ├── handler/
│   │   ├── handler.go           # Shared config and helpers
│   │   ├── bookings.go          # Booking handler
│   │   ├── trackings.go          # Tracking handler
│   │   └── service_points.go    # Service points handler
│   └── middleware/
│       ├── auth.go              # Authentication middleware
│       └── logging.go           # Logging middleware
├── vercel.json                  # Vercel configuration
├── go.mod
├── go.sum
└── README.md
```

### **Local Development**
1. **Run the API locally**:
   ```bash
   go run cmd/api/main.go
   ```
   The API will start on `http://localhost:8080`.

2. **Run the CLI locally**:
   ```bash
   go run cmd/cli/main.go --help
   ```

3. **Test with Mock Mode**:
   ```bash
   MOCK_MODE=true go run cmd/cli/main.go book --carrier postnord --input shipment.json
   ```

4. **Test with Production Mode**:
   ```bash
   export POSTNORD_API_KEY=your-api-key
   go run cmd/cli/main.go book --carrier postnord --input shipment.json
   ```

---

## **🧪 Testing**

### **Run Tests**
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests for a specific package
go test ./internal/adapter
```

### **Test Files**
- `internal/adapter/adapter_test.go`: Tests for shared types and validation.
- `internal/adapter/postnord_test.go`: Tests for PostNord adapter (including retry logic).
- `internal/adapter/fedex_test.go`: Tests for FedEx adapter.
- `internal/handler/bookings_test.go`: Tests for booking handler.

---

## **🚀 Deployment**

### **Vercel**
1. **Install Vercel CLI**:
   ```bash
   npm install -g vercel
   ```

2. **Deploy**:
   ```bash
   vercel
   ```

3. **Configure Environment Variables**:
   Set the required environment variables in the Vercel dashboard:
   - `API_KEY`
   - `POSTNORD_API_KEY`
   - `FED_EX_CLIENT_ID`
   - `FED_EX_CLIENT_SECRET`
   - `FED_EX_ACCOUNT_NUMBER`
   - `DHL_API_KEY`
   - `MOCK_MODE` (optional, defaults to `false`)

4. **Access the API**:
   - Your API will be available at `https://your-app.vercel.app/api`.

### **Docker**
1. **Build the Docker image**:
   ```bash
   docker build -t logistics-gateway .
   ```

2. **Run the container**:
   ```bash
   docker run -p 8080:8080 --env-file .env logistics-gateway
   ```

---

## **🤝 Contributing**

### **How to Contribute**
1. Fork the repository.
2. Create a new branch (`git checkout -b feature/your-feature`).
3. Commit your changes (`git commit -am 'Add some feature'`).
4. Push to the branch (`git push origin feature/your-feature`).
5. Open a Pull Request.

### **Guidelines**
- Follow Go community standards for **formatting** (`gofmt`) and **documentation**.
- Add **unit tests** for all new functionality.
- Use **meaningful commit messages**.
- Keep the code **modular** and **reusable**.

---

## **📜 License**

This project is licensed under the **Apache License 2.0** – see the [LICENSE](LICENSE) file for details.
