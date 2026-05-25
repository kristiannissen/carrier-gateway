# Logistics Gateway (v1)

A stateless, schema-first middleware engine designed to unify fulfillment streams across major Scandinavian transport carriers. Logistics Gateway bridges the gap between modern eCommerce platforms, enterprise ERP systems, automated infrastructure pipelines, and logistics providers like PostNord, GLS, DAO, and Bring.

By abstracting carrier-specific REST/XML complexities behind a single unified transaction boundary, developers integrate once and gain access to multi-colli bookings, automated customs compliance logic, and labelless consumer returns.

---

## 🚀 Key Capabilities

* **Unified Booking Ledger:** A single, multi-colli compatible JSON contract (`POST /api/v1/bookings`) that handles single package shop dispatches and multi-package B2B shipments identically.
* **Dual Ingress Architecture:** Fully accessible via standard **REST API Web Endpoints** or a raw, pipe-friendly **Command Line Interface (CLI)** for system administrators and automation cronjobs.
* **Automated Trade Compliance:** Built-in validation loops that intercept Non-EU shipments, strictly enforcing missing HS Codes and customs datasets based on chosen Incoterms (`DDP` / `DAP`).
* **Labelless B2C Returns:** Built-in twin return booking generation including automated "Return of Goods" customs protection and mobile drop-off QR-code asset streaming (`format=qr`).
* **Legacy Translation Node:** Built-in parsing adapters capable of intercepting legacy EDI (EDIFACT IFTMIN / XML) transport streams and converting them to normalized gateway contracts.

---

## 📂 Repository Architecture

The project is structured according to idiomatic Go enterprise layouts, explicitly decoupling network protocol ingress layers from core logistics business logic:

```text
├── go.mod                 # Cloud compilation dependency manifest
├── vercel.json            # Serverless deployment & API routing matrix
├── README.md              # Public project documentation
├── api/                   # REST Ingress (Vercel Serverless Functions)
│   ├── bookings.go        # Handles POST /bookings & GET label asset streams
│   ├── service-points.go  # Cross-carrier geographic parcel shop lookup
│   ├── tracking.go        # Normalized shipment timeline normalization
│   ├── docs.go            # Programmatic self-service validation schemas
│   └── status.go          # Live uptime and upstream carrier connection health
├── cmd/                   # Command Line Interface (CLI)
│   └── lg/
│       └── main.go        # Main executable engine ('lg booking --edi')
├── internal/              # Shared Domain Business Logic (Decoupled Core)
│   ├── engine/
│   │   └── booking.go     # Schema validation, Incoterms, and EDI parsing
│   └── carriers/
│       └── adapters.go    # Structural mapping translations for PostNord, GLS, etc.
└── public/
    └── status.html        # Interactive Developer Sandbox & EDI Playground