# Multi-Carrier Integration Service

A **stateless, modular Go microservice** for integrating with multiple logistics carriers (PostNord, FedEx, DHL). Supports **synchronous/asynchronous operations**, **webhooks**, and **environment-based authentication**. Designed for **open-source distribution** under Apache 2.0.

---

## **Features**
- **Multi-Carrier Support**: PostNord, FedEx, DHL (extensible to others).
- **Colli (Multi-Package) Shipments**: Supports both single-package and multi-package shipments.
- **Idempotency**: Supported for FedEx; ignored for PostNord/DHL (with warnings).
- **Webhooks**: Callback URLs for tracking updates.
- **Stateless Design**: No persistence layer (async job tracking via external storage).
- **Docker & CLI Tools**: Easy deployment and local development.
- **Vercel-Compatible**: Deploy as a serverless function.

---

## **Table of Contents**
1. [Installation](#installation)
2. [Configuration](#configuration)
3. [API Reference](#api-reference)
4. [Colli (Multi-Package) Shipments](#colli-multi-package-shipments)
5. [Carrier-Specific Notes](#carrier-specific-notes)
6. [Examples](#examples)
7. [Development](#development)
8. [Testing](#testing)
9. [Deployment](#deployment)
10. [Contributing](#contributing)
11. [License](#license)

---

## **Installation**

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

## **Configuration**

### **Environment Variables**
| Variable               | Description                          | Required | Default       |
|------------------------|--------------------------------------|----------|---------------|
| `PORT`                 | Port for the HTTP server             | No       | `8080`        |
| `API_KEY`              | API key for the service              | Yes      | -             |
| `POSTNORD_API_KEY`     | PostNord API key                     | No       | -             |
| `FED_EX_API_KEY`       | FedEx API key                        | No       | -             |
| `DHL_API_KEY`          | DHL API key                          | No       | -             |
| `ENVIRONMENT`          | Environment (e.g., `development`)     | No       | `development` |

### **Example `.env` File**
```env
PORT=8080
API_KEY=your-api-key
POSTNORD_API_KEY=your-postnord-api-key
FED_EX_API_KEY=your-fedex-api-key
DHL_API_KEY=your-dhl-api-key
ENVIRONMENT=development
```

---

## **API Reference**

### **Endpoints**
| Method | Endpoint                          | Description                          |
|--------|-----------------------------------|--------------------------------------|
| POST   | `/api/bookings`                   | Book a shipment (single or multi-colli). |
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
      "name": "Sender Name",
      "street": "Sender Street",
      "city": "Copenhagen",
      "postalCode": "2300",
      "country": "DK",
      "phone": "+4512345678",
      "email": "sender@example.com"
    },
    "receiver": {
      "name": "Receiver Name",
      "street": "Receiver Street",
      "city": "Stockholm",
      "postalCode": "111 22",
      "country": "SE",
      "phone": "+46123456789",
      "email": "receiver@example.com"
    },
    "totalWeight": 15.0,
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
        "weight": 10.0,
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
  "callbackUrl": "https://api.example.com/webhooks/tracking",
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

#### **Response Fields**
| Field               | Type          | Description                                                                                     |
|---------------------|---------------|-------------------------------------------------------------------------------------------------|
| `shipmentId`        | `string`      | Unique identifier for the shipment (UUID).                                                     |
| `trackingNumber`    | `string`      | Parent tracking number for the entire shipment.                                                |
| `labelUrl`          | `string`      | URL to download the shipping label (PDF).                                                       |
| `carrier`           | `string`      | Carrier name (e.g., `postnord`).                                                                |
| `cost`              | `float`       | Total shipping cost.                                                                           |
| `currency`          | `string`      | Currency for the shipping cost (e.g., `DKK`, `USD`).                                            |
| `serviceLevel`      | `string`      | Service level (e.g., `Standard`, `Express`).                                                    |
| `status`            | `string`      | Status of the shipment (e.g., `booked`, `error`).                                               |
| `colli`             | `array`       | List of colli responses. See [ColliResponse](#colliresponse).                                  |

#### **ColliResponse Fields**
| Field               | Type     | Description                     |
|---------------------|----------|---------------------------------|
| `id`                | `string` | Unique identifier for the colli. |
| `reference`         | `string` | Optional reference.              |
| `trackingNumber`    | `string` | Individual tracking number.      |
| `labelUrl`          | `string` | URL to download the label.       |
| `status`            | `string` | Status of this colli.            |

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

#### **Response Fields**
| Field               | Type          | Description                                                                                     |
|---------------------|---------------|-------------------------------------------------------------------------------------------------|
| `shipmentId`        | `string`      | Unique identifier for the shipment.                                                           |
| `trackingNumber`    | `string`      | Parent tracking number for the entire shipment.                                                |
| `carrier`           | `string`      | Carrier name (e.g., `postnord`).                                                                |
| `status`            | `string`      | Overall status of the shipment (e.g., `In Transit`, `Delivered`).                              |
| `estimatedDelivery` | `string`      | Estimated delivery date (ISO 8601 format).                                                     |
| `events`            | `array`       | Parent shipment tracking events. See [TrackingEvent](#trackingevent).                        |
| `colli`             | `array`       | List of colli tracking information. See [ColliTracking](#collitracking).                     |

#### **TrackingEvent Fields**
| Field       | Type     | Description                     |
|-------------|----------|---------------------------------|
| `timestamp` | `string` | Timestamp (ISO 8601 format).     |
| `status`    | `string` | Event status (e.g., `Picked Up`). |
| `location`  | `string` | Location of the event.           |
| `details`   | `string` | Additional details.              |

#### **ColliTracking Fields**
| Field               | Type          | Description                                                                                     |
|---------------------|---------------|-------------------------------------------------------------------------------------------------|
| `id`                | `string`      | Unique identifier for the colli.                                                               |
| `reference`         | `string`      | Optional reference.                                                                              |
| `trackingNumber`    | `string`      | Individual tracking number for this colli.                                                     |
| `status`            | `string`      | Current status of this colli.                                                                   |
| `events`            | `array`       | Tracking events for this colli. See [TrackingEvent](#trackingevent).                          |

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

#### **Response Fields**
| Field          | Type          | Description                                                                                     |
|----------------|---------------|-------------------------------------------------------------------------------------------------|
| `id`           | `string`      | Unique identifier for the service point.                                                       |
| `name`         | `string`      | Name of the service point.                                                                     |
| `address`      | `object`      | Address of the service point. See [Address](#address).                                         |
| `openingHours` | `string`      | Opening hours (e.g., `09:00-17:00`).                                                             |
| `services`     | `array`       | List of services offered (e.g., `Pickup`, `Dropoff`).                                           |

---

## **Colli (Multi-Package) Shipments**

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
2. **Response**: The API returns:
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

## **Carrier-Specific Notes**

### **PostNord**
- **Multi-Colli Support**: Native support for multi-package shipments.
- **Tracking**: Each colli can have its own tracking number (e.g., `PN123456789DK-1`, `PN123456789DK-2`).
- **Labels**: Individual labels for each colli or a single label for the entire shipment.
- **Idempotency**: Not supported (warnings are logged if `idempotencyKey` is provided).

### **FedEx**
- **Multi-Colli Support**: Uses `packageLineItems` for multi-package shipments.
- **Tracking**: Each package can have its own tracking number.
- **Labels**: Individual labels for each package.
- **Idempotency**: Supported via `Idempotency-Key` header.
- **Webhooks**: Supported via `notificationUrl`.

### **DHL**
- **Multi-Colli Support**: Uses `pieces` or `packages` for multi-package shipments.
- **Tracking**: Each piece can have its own tracking number.
- **Labels**: Individual labels for each piece.
- **Idempotency**: Not supported (warnings are logged if `idempotencyKey` is provided).

---

## **Examples**

### **1. Book a Single-Package Shipment (PostNord)**
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

### **2. Book a Multi-Package Shipment (FedEx)**
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

### **3. Track a Shipment (PostNord)**
```bash
curl -X GET https://api.example.com/api/trackings/PN123456789DK \
  -H "Authorization: Bearer your-api-key"
```

### **4. Get Service Points (PostNord)**
```bash
curl -X GET "https://api.example.com/api/service-points?city=Copenhagen&country=DK" \
  -H "Authorization: Bearer your-api-key"
```

---

## **Development**

### **Project Structure**
```bash
logistics-gateway/
├── cmd/
│   └── api/
│       └── main.go              # Vercel Serverless Function entry point
├── internal/
│   ├── adapter/
│   │   ├── adapter.go           # CarrierAdapter interface and shared types
│   │   ├── postnord.go          # PostNord adapter
│   │   ├── fedex.go             # FedEx adapter
│   │   └── dhl.go               # DHL adapter (placeholder)
│   ├── handler/
│   │   ├── handler.go           # Shared handler config and helpers
│   │   ├── bookings.go          # Booking handler
│   │   ├── trackings.go          # Tracking handler
│   │   └── service_points.go    # Service points handler
│   └── middleware/
│       ├── auth.go              # Authentication middleware
│       └── logging.go           # Logging middleware
├── go.mod
├── go.sum
├── Dockerfile
├── docker-compose.yml
└── README.md
```

### **Local Development**
1. **Run the service locally**:
   ```bash
   go run cmd/api/main.go
   ```
   The service will start on `http://localhost:8080`.

2. **Use Docker**:
   ```bash
   docker-compose up --build
   ```

3. **Test with cURL**:
   Use the [examples](#examples) above to test the API.

---

## **Testing**

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
- `internal/adapter/postnord_test.go`: Tests for PostNord adapter.
- `internal/adapter/fedex_test.go`: Tests for FedEx adapter.
- `internal/handler/bookings_test.go`: Tests for booking handler.

---

## **Deployment**

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
   - `FED_EX_API_KEY`
   - `DHL_API_KEY`

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

## **Contributing**

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

## **License**

This project is licensed under the **Apache License 2.0** – see the [LICENSE](LICENSE) file for details.
