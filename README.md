# Multi-Carrier Integration Service

A **stateless, modular Go microservice** for integrating with multiple logistics carriers (PostNord, FedEx, DHL). Supports **synchronous operations**, **colli (multi-package) shipments**, **webhooks**, and **environment-based authentication**. Designed for **open-source distribution** under Apache 2.0.

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
7. [Deployment](#deployment)
8. [Integration Guide](#integration-guide)
9. [Examples](#examples)
10. [Development](#development)
11. [Testing](#testing)
12. [Contributing](#contributing)
13. [License](#license)

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
