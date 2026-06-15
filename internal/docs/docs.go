// Package docs provides built-in API documentation served at /docs.
// This file is located at /internal/docs/docs.go.
package docs

import "sort"

// Endpoint documents a single API route.
type Endpoint struct {
	// Slug is the URL-friendly identifier used in /docs/{slug}.
	Slug string `json:"slug"`
	// Method is the HTTP verb (GET, POST, PATCH, DELETE).
	Method string `json:"method"`
	// Path is the route pattern, e.g. /api/bookings.
	Path string `json:"path"`
	// Summary is a one-line description.
	Summary string `json:"summary"`
	// Description is a longer explanation of the endpoint's behaviour.
	Description string `json:"description"`
	// Payload is the example request body as a formatted JSON string.
	// Intentionally decoupled from the curl command so callers can copy it
	// directly into any HTTP client (curl, Postman, HTTPie, etc.).
	Payload string `json:"payload,omitempty"`
	// Curl is the corresponding curl invocation referencing a payload file.
	// The payload is kept separate so it can be saved as payload.json and
	// passed via -d @payload.json without quoting issues.
	Curl string `json:"curl,omitempty"`
	// Fields documents the key request fields.
	Fields []Field `json:"fields,omitempty"`
}

// Field documents a single request body field.
type Field struct {
	// Name is the JSON field name.
	Name string `json:"name"`
	// Type is the JSON type (string, number, boolean, object, array).
	Type string `json:"type"`
	// Required is true when the field must be present.
	Required bool `json:"required"`
	// Description explains the field's purpose and accepted values.
	Description string `json:"description"`
}

// Term is a freight/logistics glossary entry.
// Format mirrors curl --help: a short one-liner per term followed by an
// optional extended description for callers who want more detail.
type Term struct {
	// Name is the acronym or term, e.g. "COD".
	Name string `json:"name"`
	// Short is the one-line definition shown in the terminology index.
	Short string `json:"short"`
	// Description is the extended explanation.
	Description string `json:"description"`
}

// Registry holds documentation for all endpoints and freight terminology.
// Build once at startup via New and share the pointer across handlers.
type Registry struct {
	endpoints map[string]*Endpoint
	order     []string // insertion order for /docs index
	terms     []*Term
}

// New returns a Registry pre-populated with endpoint docs and freight terms.
func New() *Registry {
	r := &Registry{
		endpoints: make(map[string]*Endpoint),
	}
	r.registerEndpoints()
	r.registerTerms()
	return r
}

// Endpoint returns the documentation for slug. Returns nil when not found.
func (r *Registry) Endpoint(slug string) *Endpoint {
	return r.endpoints[slug]
}

// Endpoints returns all registered endpoints in their original registration order.
func (r *Registry) Endpoints() []*Endpoint {
	out := make([]*Endpoint, 0, len(r.order))
	for _, slug := range r.order {
		if e, ok := r.endpoints[slug]; ok {
			out = append(out, e)
		}
	}
	return out
}

// Terms returns all terminology entries sorted alphabetically by name.
func (r *Registry) Terms() []*Term {
	cp := make([]*Term, len(r.terms))
	copy(cp, r.terms)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Name < cp[j].Name })
	return cp
}

// add registers an endpoint under its slug in insertion order.
func (r *Registry) add(e *Endpoint) {
	r.endpoints[e.Slug] = e
	r.order = append(r.order, e.Slug)
}

func (r *Registry) registerEndpoints() {
	r.add(&Endpoint{
		Slug:    "bookings",
		Method:  "POST",
		Path:    "/api/bookings",
		Summary: "Book a shipment",
		Description: `Creates a new shipment with the specified carrier and returns a tracking
number and shipping label. The idempotency key prevents duplicate bookings
if the request is retried — carriers with native idempotency support pass it
directly; others deduplicate via the gateway middleware.

Customs data (incoterms, HS code, customs value) is required for shipments
to non-EU destinations. A CN22 or CN23 form is generated automatically and
returned as a base64-encoded field when applicable.`,
		Payload: `{
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
}`,
		Curl: `curl -X POST http://localhost:8080/api/bookings \
  -H "Content-Type: application/json" \
  -d @payload.json`,
		Fields: []Field{
			{Name: "carrier", Type: "string", Required: true, Description: "Carrier key, e.g. postnord, bring, gls, dao, fedex, dhl_express, omniva."},
			{Name: "shipment.sender", Type: "object", Required: true, Description: "Sender address. name, street, city, country are required."},
			{Name: "shipment.receiver", Type: "object", Required: true, Description: "Receiver address. When servicePointId is set, street/city/postalCode are optional."},
			{Name: "shipment.deliveryType", Type: "string", Required: false, Description: "home, business, servicepoint, or return. Adapter picks a default when omitted."},
			{Name: "shipment.totalWeight", Type: "number", Required: true, Description: "Total shipment weight in kg. Must equal the sum of colli weights."},
			{Name: "shipment.colli", Type: "array", Required: true, Description: "One or more parcels. Each must have id, weight, dimensions, and at least one item."},
			{Name: "shipment.addOns", Type: "array", Required: false, Description: "Optional services: sms_notification, signature_required, cash_on_delivery, insurance, flex_delivery."},
			{Name: "shipment.customs", Type: "object", Required: false, Description: "Required for non-EU destinations. Provide incoterms, hsCode, customsValue, importerOfRecord."},
			{Name: "idempotencyKey", Type: "string", Required: false, Description: "Unique key for deduplication. Use the order ID or a UUID. Max 128 chars."},
			{Name: "labelFormat", Type: "string", Required: false, Description: "PDF (default), PNG, ZPL, EPL, or ZPLGK. Carrier support varies."},
			{Name: "notifications", Type: "object", Required: false, Description: "Webhook configuration for event-driven notifications. Provide webhookUrl and optional webhookSecret."},
		},
	})

	r.add(&Endpoint{
		Slug:    "bookings-cancel",
		Method:  "DELETE",
		Path:    "/api/bookings/{trackingNumber}",
		Summary: "Cancel a shipment",
		Description: `Cancels a booked shipment before it is collected by the carrier.
Not all carriers support cancellation — check the carrier's capabilities
at GET /api/health. Returns 422 when the carrier does not support cancellation
or the shipment has already been scanned.`,
		Curl: `curl -X DELETE "http://localhost:8080/api/bookings/370023456789" \
  -H "Content-Type: application/json"`,
	})

	r.add(&Endpoint{
		Slug:    "bookings-update",
		Method:  "PATCH",
		Path:    "/api/bookings/{trackingNumber}",
		Summary: "Update a booked shipment",
		Description: `Applies partial updates to a booked shipment. Only non-zero fields are
forwarded to the carrier. Supported fields vary by carrier — unsupported
fields are silently ignored and listed in the response's ignoredFields array.`,
		Payload: `{
  "carrier": "dao",
  "phone": "+4587654321",
  "email": "new@example.com",
  "servicePointId": "SP-42"
}`,
		Curl: `curl -X PATCH "http://localhost:8080/api/bookings/370023456789" \
  -H "Content-Type: application/json" \
  -d @payload.json`,
		Fields: []Field{
			{Name: "carrier", Type: "string", Required: true, Description: "Must match the carrier used when the shipment was booked."},
			{Name: "phone", Type: "string", Required: false, Description: "New receiver phone number."},
			{Name: "email", Type: "string", Required: false, Description: "New receiver email address."},
			{Name: "weight", Type: "number", Required: false, Description: "Updated parcel weight in kg. Must be set before first terminal scan."},
			{Name: "servicePointId", Type: "string", Required: false, Description: "Redirect delivery to a different service point. DAO only."},
		},
	})

	r.add(&Endpoint{
		Slug:    "trackings",
		Method:  "GET",
		Path:    "/api/trackings/{trackingNumber}",
		Summary: "Get tracking status",
		Description: `Returns the current tracking status and full event history for a shipment.
The normalizedStatus field uses carrier-agnostic values (booked, picked_up,
in_transit, out_for_delivery, delivered, failed, returned, delayed, unknown)
regardless of which carrier was used.`,
		Curl: `curl "http://localhost:8080/api/trackings/370023456789?carrier=postnord"`,
	})

	r.add(&Endpoint{
		Slug:    "trackings-notify",
		Method:  "POST",
		Path:    "/api/trackings/{trackingNumber}",
		Summary: "Track and dispatch notifications",
		Description: `Fetches the current tracking status and fires webhook notifications
for any configured events. Use this when you need tracking + notification
in a single round-trip, e.g. from a background job.`,
		Payload: `{
  "carrier": "postnord",
  "notifications": {
    "webhookUrl": "https://example.com/hooks/shipment",
    "webhookSecret": "s3cr3t",
    "events": ["delivered", "failed"]
  }
}`,
		Curl: `curl -X POST "http://localhost:8080/api/trackings/370023456789" \
  -H "Content-Type: application/json" \
  -d @payload.json`,
	})

	r.add(&Endpoint{
		Slug:    "labels",
		Method:  "GET",
		Path:    "/api/labels/{trackingNumber}",
		Summary: "Fetch a shipping label",
		Description: `Returns the shipping label for a booked shipment as base64-encoded data.
Use the ?format= query parameter to request a specific output format.
Supported formats depend on the carrier (PDF is always available).`,
		Curl: `curl "http://localhost:8080/api/labels/370023456789?carrier=postnord&format=PDF"`,
	})

	r.add(&Endpoint{
		Slug:    "notifications",
		Method:  "POST",
		Path:    "/api/notifications",
		Summary: "Send a notification",
		Description: `Dispatches a shipment event notification to the configured webhook.
Use this endpoint to retry a failed notification from the notificationsFailed
array in a booking or tracking response.`,
		Payload: `{
  "webhookUrl": "https://example.com/hooks/shipment",
  "webhookSecret": "s3cr3t",
  "event": "delivered",
  "shipmentId": "SHP-001",
  "trackingNumber": "370023456789",
  "carrier": "postnord",
  "status": "delivered"
}`,
		Curl: `curl -X POST http://localhost:8080/api/notifications \
  -H "Content-Type: application/json" \
  -d @payload.json`,
	})

	r.add(&Endpoint{
		Slug:    "pickups-availability",
		Method:  "GET",
		Path:    "/api/pickups/availability",
		Summary: "Get pickup availability",
		Description: `Returns available pickup time slots for a carrier at a given address.
Use this before booking a pickup to present options to the user.`,
		Curl: `curl "http://localhost:8080/api/pickups/availability?carrier=fedex&country=DK&postalCode=2300"`,
	})

	r.add(&Endpoint{
		Slug:    "pickups",
		Method:  "POST",
		Path:    "/api/pickups",
		Summary: "Book a pickup",
		Description: `Schedules a carrier pickup at the sender's address for one or more shipments.
The carrier will collect parcels during the requested time window.`,
		Payload: `{
  "carrier": "fedex",
  "address": {
    "name": "Unisport Group",
    "street": "Industrivej",
    "houseNumber": "10",
    "city": "Copenhagen",
    "postalCode": "2300",
    "country": "DK",
    "phone": "+4512345678"
  },
  "readyTime": "2026-06-16T10:00:00Z",
  "closeTime": "2026-06-16T17:00:00Z",
  "packageCount": 3,
  "totalWeight": 12.5
}`,
		Curl: `curl -X POST http://localhost:8080/api/pickups \
  -H "Content-Type: application/json" \
  -d @payload.json`,
	})

	r.add(&Endpoint{
		Slug:    "pickups-update",
		Method:  "PUT",
		Path:    "/api/pickups/{confirmationNumber}",
		Summary: "Update a pickup booking",
		Description: `Updates the time window or package details for an existing pickup booking.
Use the confirmationNumber returned by POST /api/pickups.`,
		Payload: `{
  "carrier": "fedex",
  "readyTime": "2026-06-16T11:00:00Z",
  "closeTime": "2026-06-16T16:00:00Z"
}`,
		Curl: `curl -X PUT "http://localhost:8080/api/pickups/PU-98765" \
  -H "Content-Type: application/json" \
  -d @payload.json`,
	})

	r.add(&Endpoint{
		Slug:    "pickups-cancel",
		Method:  "DELETE",
		Path:    "/api/pickups/{confirmationNumber}",
		Summary: "Cancel a pickup booking",
		Description: `Cancels a previously booked pickup. The confirmationNumber is returned
by POST /api/pickups and also stored in BookingResponse.dispatchConfirmationNumber
for carriers (e.g. DHL Express) that bundle pickup with shipment booking.`,
		Curl: `curl -X DELETE "http://localhost:8080/api/pickups/PU-98765?carrier=fedex"`,
	})

	r.add(&Endpoint{
		Slug:    "manifests",
		Method:  "POST",
		Path:    "/api/manifests",
		Summary: "Close end-of-day manifest",
		Description: `Closes the carrier's end-of-day manifest, committing all booked shipments
for collection. Required by carriers such as FedEx Ground that need an explicit
close before pickup. Returns 422 for carriers that do not support manifests.`,
		Payload: `{
  "carrier": "fedex"
}`,
		Curl: `curl -X POST http://localhost:8080/api/manifests \
  -H "Content-Type: application/json" \
  -d @payload.json`,
	})

	r.add(&Endpoint{
		Slug:    "returns",
		Method:  "POST",
		Path:    "/api/returns",
		Summary: "Book a return shipment",
		Description: `Books a return shipment against an already-delivered outbound shipment.
Currently supported on Omniva only. The response includes a return tracking
number and a printable return label.`,
		Payload: `{
  "carrier": "omniva",
  "originalTrackingNumber": "EE123456789EE",
  "shipment": {
    "sender": {
      "name": "Anna Svensson",
      "street": "Storgatan",
      "houseNumber": "1",
      "city": "Stockholm",
      "postalCode": "11122",
      "country": "SE"
    },
    "receiver": {
      "name": "Unisport Group",
      "street": "Industrivej",
      "houseNumber": "10",
      "city": "Copenhagen",
      "postalCode": "2300",
      "country": "DK"
    },
    "totalWeight": 2.5,
    "colli": [
      {
        "id": "return-001",
        "weight": 2.5,
        "dimensions": { "length": 30, "width": 20, "height": 10 },
        "items": [
          { "description": "Football boots", "weight": 0.8, "quantity": 1, "value": 129.95 }
        ]
      }
    ]
  },
  "idempotencyKey": "return-98765"
}`,
		Curl: `curl -X POST http://localhost:8080/api/returns \
  -H "Content-Type: application/json" \
  -d @payload.json`,
	})

	r.add(&Endpoint{
		Slug:    "health",
		Method:  "GET",
		Path:    "/api/health",
		Summary: "Service health check",
		Description: `Returns the service status, uptime, mock mode flag, and the operational
mode of every registered carrier adapter (production, mock, or beta).
Use this to verify configuration at startup or in a readiness probe.`,
		Curl: `curl http://localhost:8080/api/health`,
	})
}

func (r *Registry) registerTerms() {
	r.terms = []*Term{
		{
			Name:  "AWB",
			Short: "Air Waybill — the tracking document for air freight shipments.",
			Description: `An Air Waybill (AWB) is the shipping document that accompanies air freight
and serves as a contract of carriage between the shipper and the carrier.
For express parcel carriers (DHL Express, FedEx) the AWB number is used as
the tracking number.`,
		},
		{
			Name:  "B2B",
			Short: "Business-to-Business — shipment between two companies.",
			Description: `A B2B shipment is sent from one business to another. It affects customs
treatment: B2B shipments may require full customs declarations and VAT
registration numbers (EORI, importer/exporter VAT) even within the EU
above the de minimis threshold (currently EUR 150).`,
		},
		{
			Name:  "B2C",
			Short: "Business-to-Consumer — shipment from a business to an end customer.",
			Description: `A B2C shipment is sent from a business to a private individual. The de
minimis threshold (EUR 150 within the EU) often exempts low-value B2C
parcels from full customs declarations. IOSS registration can pre-collect
VAT at point of sale for eligible B2C shipments to the EU.`,
		},
		{
			Name:  "COD",
			Short: "Cash on Delivery — payment collected from the receiver on delivery.",
			Description: `With COD (Cash on Delivery), the carrier collects payment from the
recipient at the time of delivery and transfers it to the shipper's bank
account. Set addOns[].type = "cash_on_delivery" and provide codAmount,
codCurrency, and codAccountNumber. Supported on Bring (VAS 1000) and
Omniva (IBAN required for Estonian accounts).`,
		},
		{
			Name:  "Colli",
			Short: "An individual parcel or package within a shipment.",
			Description: `Colli (singular: collis) refers to the individual physical packages that
make up a shipment. A single booking can contain multiple colli, each with
its own weight, dimensions, and item list. The sum of colli weights must
equal shipment.totalWeight.`,
		},
		{
			Name:  "DAP",
			Short: "Delivered at Place — seller delivers to destination; buyer pays import duties.",
			Description: `Under DAP Incoterms, the seller (shipper) is responsible for delivering
the goods to the named destination. The buyer (receiver) is responsible
for import customs clearance and any duties/taxes. DAP is common for
international B2C e-commerce. Set shipment.customs.incoterms = "DAP".`,
		},
		{
			Name:  "DDP",
			Short: "Delivered Duty Paid — seller pays all duties and taxes to the destination.",
			Description: `Under DDP Incoterms, the seller takes maximum responsibility and pays
all costs including import customs duties and VAT at destination. This
provides the best customer experience (no surprise charges on delivery)
but requires the seller to be VAT-registered in the destination country or
use an IOSS number. Set shipment.customs.incoterms = "DDP".`,
		},
		{
			Name:  "De minimis",
			Short: "The customs threshold below which duties and taxes are not collected.",
			Description: `The de minimis threshold is the value below which a shipment can enter
a country without formal customs clearance or duty payment. Within the EU
the threshold is EUR 150 for customs duties (0 for VAT unless IOSS is used).
Different countries have different thresholds — the US threshold is USD 800.
Shipments above de minimis require a full customs declaration block.`,
		},
		{
			Name:  "EORI",
			Short: "Economic Operators Registration and Identification — EU customs ID for businesses.",
			Description: `An EORI number is a unique identifier assigned by EU customs authorities to
economic operators (importers, exporters, carriers) involved in customs activities.
It is required for businesses importing or exporting goods to/from the EU.
Format: {country-code}{number}, e.g. DK12345678.`,
		},
		{
			Name:  "HS Code",
			Short: "Harmonized System code — international commodity classification number.",
			Description: `An HS (Harmonized System) code is a 6–10 digit number that classifies goods
for customs purposes under the World Customs Organization's international
nomenclature. It determines applicable duties, taxes, and import/export
restrictions. The first 6 digits are universal; digits 7–10 are country-specific.
Required for all non-EU shipments. Set shipment.customs.hsCode.`,
		},
		{
			Name:  "Idempotency key",
			Short: "A unique key that prevents duplicate bookings on retry.",
			Description: `An idempotency key ensures that retrying a failed request does not create
a duplicate shipment. Use your order ID or a UUID. Carriers with native
idempotency support (PostNord, Evri) pass the key to their API; others
deduplicate via the gateway's middleware store. Max 128 characters.
Set idempotencyKey on the booking request.`,
		},
		{
			Name:  "Incoterms",
			Short: "International Commercial Terms — trade rules defining delivery responsibility.",
			Description: `Incoterms (International Commercial Terms) are a set of 11 standardised
trade terms published by the International Chamber of Commerce (ICC). They
define who is responsible for transportation, insurance, and customs at each
stage of an international shipment. Common values: EXW, FCA, DAP, DDP, CIP.
Required for non-EU shipments. Set shipment.customs.incoterms.`,
		},
		{
			Name:  "IOSS",
			Short: "Import One Stop Shop — EU VAT scheme for low-value B2C imports.",
			Description: `IOSS (Import One Stop Shop) is an EU VAT registration scheme that allows
sellers outside the EU to collect and remit VAT on B2C sales below EUR 150
at point of sale. Shipments with a valid IOSS number clear customs faster
because VAT has already been paid. Set shipment.customs.iossNumber.`,
		},
		{
			Name:  "Manifest",
			Short: "End-of-day carrier document committing all booked shipments for collection.",
			Description: `A manifest (or end-of-day close) is a document submitted to the carrier
at the end of the business day that lists all shipments ready for pickup.
Some carriers (FedEx Ground) require an explicit manifest close via
POST /api/manifests before they will collect parcels. Others close
automatically on collection.`,
		},
		{
			Name:  "POD",
			Short: "Proof of Delivery — confirmation that a shipment was delivered.",
			Description: `Proof of Delivery (POD) is the electronic or physical record confirming that
a shipment reached its destination. It typically includes a timestamp, the
name of the person who signed, and sometimes a signature image or document
URL. Available in TrackingResponse.proofOfDelivery for carriers that support
ePOD (currently DHL Express and DHL Freight).`,
		},
		{
			Name:  "Service point",
			Short: "A pickup point, parcel shop, or locker where the receiver collects their parcel.",
			Description: `A service point is a staffed or automated location (parcel shop, post office,
locker) where the receiver can collect their parcel instead of receiving it
at home. Set receiver.servicePointId to route a shipment to a specific
service point. Each carrier uses a different ID format:
PostNord → servicePointId, Bring → pickupPointId, GLS → parcelShopId,
DAO → lockerId, InPost → targetLocker, DHL Express → 6-char code (e.g. BRU001).`,
		},
		{
			Name:  "VAS",
			Short: "Value-Added Service — an optional add-on service attached to a shipment.",
			Description: `Value-Added Services (VAS) are optional enhancements to a standard shipment,
such as SMS notification, signature on delivery, cash on delivery, insurance,
or flex delivery. In the carrier-gateway API these are modelled as addOns[]
on the shipment. Each adapter maps the carrier-agnostic add-on type to its
own VAS code (e.g. Bring VAS 1131 = signature_required).`,
		},
	}
}
