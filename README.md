# Multi-Carrier Integration Service - Technical Specifications

---

## **📌 Overview**
A **stateless, modular Go microservice** for integrating with multiple logistics carriers (PostNord, FedEx, DHL). Supports **synchronous/asynchronous operations**, **webhooks**, and **environment-based authentication**. Designed for **open-source distribution** under Apache 2.0.

**Repository**: [`github.com/kristiannissen/logistics-gateway`](https://github.com/kristiannissen/logistics-gateway)

---

---

## **🎯 Core Principles**
1. **Stateless Design**:
   - 100% stateless. No data is stored by the solution.
   - Async operations are handled via external systems (e.g., queues, webhooks).

2. **Modularity**:
   - All carrier logic is abstracted behind the `CarrierAdapter` interface.
   - Easy to add new carriers (e.g., PostNord, FedEx, DHL).

3. **Open Source**:
   - Licensed under **Apache 2.0**.

4. **Developer Experience (DX) First**:
   - Mocking, testing, and debugging tools are prioritized.
   - Clear documentation and examples for integration.

---

---

## **🔧 Architecture Overview**

---

### **1. Project Structure**
```bash
logistics-gateway/
├── cmd/
│   └── api/
│       └── main.go              # Vercel Serverless Function entry point
├── internal/
│   ├── adapter/                # Carrier adapters
│   │   ├── adapter.go          # CarrierAdapter interface + shared types
│   │   ├── postnord.go         # PostNord adapter
│   │   ├── fedex.go            # FedEx adapter
│   │   └── dhl.go              # DHL adapter (future)
│   ├── handler/                # HTTP handlers
│   │   ├── handler.go          # Shared config and helpers
│   │   ├── bookings.go         # Booking handler
│   │   ├── trackings.go         # Tracking handler
│   │   └── service_points.go   # Service points handler
│   ├── middleware/             # Middleware
│   │   ├── auth.go             # Authentication
│   │   └── logging.go          # Structured logging
│   └── testdata/               # Mock data for testing
│       ├── postnord.json       # PostNord mock responses
│       ├── fedex.json          # FedEx mock responses
│       └── dhl.json            # DHL mock responses
├── test/                       # Additional test utilities
│   └── mocks/                  # Generated mocks (e.g., testify/mock)
├── docs/                       # Documentation
│   ├── openapi.yaml           # OpenAPI/Swagger spec
│   └── postman_collection.json # Postman collection
├── .github/
│   └── workflows/
│       └── test.yml           # GitHub Actions CI
├── Dockerfile                  # Docker support
├── docker-compose.yml          # Local development with mocked APIs
├── Makefile                   # Common commands
├── go.mod
├── go.sum
└── README.md

