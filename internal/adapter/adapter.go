// Package adapter provides interfaces and shared types for carrier integrations.
// This file is located at /internal/adapter/adapter.go.
package adapter

// CarrierAdapter defines the interface for all carrier adapters.
// All carrier-specific implementations (e.g., PostNord, FedEx, DHL) must satisfy this interface.
type CarrierAdapter interface {
	// BookShipment books a shipment with the carrier and returns a tracking number and label URL.
	BookShipment(request BookingRequest) (*BookingResponse, error)

	// TrackShipment retrieves the tracking status for a shipment.
	TrackShipment(trackingNumber string) (*TrackingResponse, error)

	// GetServicePoints retrieves available service points (e.g., pickup locations) for a carrier.
	GetServicePoints(location Location) ([]ServicePoint, error)
}

// BookingRequest represents a generic shipment booking request.
// All shipments are treated as a list of colli (single or multi-package).
type BookingRequest struct {
	Carrier       string    `json:"carrier"`
	Shipment      Shipment  `json:"shipment"`
	CallbackURL   string    `json:"callbackUrl,omitempty"`
	IdempotencyKey string    `json:"idempotencyKey,omitempty"`
	Incoterms     string    `json:"incoterms,omitempty"`
	HSCode        string    `json:"hsCode,omitempty"`
}

// Shipment represents the shipment details.
// All shipments are treated as a list of colli (single or multi-package).
type Shipment struct {
	Sender      Address   `json:"sender"`
	Receiver    Address   `json:"receiver"`
	TotalWeight float64   `json:"totalWeight"` // Sum of all colli weights
	Colli       []Colli   `json:"colli"`       // Always a list (1+ colli)
}

// Colli represents an individual package in a shipment.
type Colli struct {
	ID          string   `json:"id"`
	Reference   string   `json:"reference,omitempty"`
	Weight      float64  `json:"weight"`
	Dimensions  struct {
		Length float64 `json:"length"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"dimensions"`
	Items []Item `json:"items"`
}

// Address represents a physical address.
type Address struct {
	Name        string `json:"name"`
	Street      string `json:"street"`
	City        string `json:"city"`
	PostalCode  string `json:"postalCode"`
	Country     string `json:"country"`
	Phone       string `json:"phone,omitempty"`
	Email       string `json:"email,omitempty"`
}

// Item represents an item in a shipment or colli.
type Item struct {
	Description string  `json:"description"`
	Weight      float64 `json:"weight"`
	Quantity    int     `json:"quantity"`
	Value       float64 `json:"value,omitempty"`
	SKU         string  `json:"sku,omitempty"`
}

// BookingResponse represents the response from a carrier after booking a shipment.
type BookingResponse struct {
	ShipmentID     string          `json:"shipmentId,omitempty"`
	TrackingNumber string          `json:"trackingNumber"`
	LabelURL       string          `json:"labelUrl,omitempty"`
	Carrier        string          `json:"carrier"`
	Cost           float64         `json:"cost,omitempty"`
	Currency       string          `json:"currency,omitempty"`
	ServiceLevel   string          `json:"serviceLevel,omitempty"`
	Status         string          `json:"status,omitempty"`
	Colli          []ColliResponse `json:"colli,omitempty"`
	Errors         []string        `json:"errors,omitempty"`
}

// ColliResponse represents the response for an individual colli in a shipment.
type ColliResponse struct {
	ID             string `json:"id"`
	Reference      string `json:"reference,omitempty"`
	TrackingNumber string `json:"trackingNumber,omitempty"`
	LabelURL       string `json:"labelUrl,omitempty"`
	Status         string `json:"status,omitempty"`
}

// TrackingResponse represents the tracking status of a shipment.
type TrackingResponse struct {
	ShipmentID       string           `json:"shipmentId,omitempty"`
	TrackingNumber   string           `json:"trackingNumber"`
	Carrier          string           `json:"carrier"`
	Status           string           `json:"status"`
	Events           []TrackingEvent  `json:"events"`
	EstimatedDelivery string           `json:"estimatedDelivery,omitempty"`
	Colli            []ColliTracking  `json:"colli,omitempty"`
}

// ColliTracking represents the tracking status of an individual colli.
type ColliTracking struct {
	ID             string          `json:"id"`
	Reference      string          `json:"reference,omitempty"`
	TrackingNumber string          `json:"trackingNumber,omitempty"`
	Status         string          `json:"status"`
	Events         []TrackingEvent `json:"events"`
}

// TrackingEvent represents a single tracking event (e.g., "Picked up", "In transit").
type TrackingEvent struct {
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"`
	Location  string `json:"location,omitempty"`
	Details   string `json:"details,omitempty"`
}

// Location represents a geographic location (e.g., for service points).
type Location struct {
	City        string  `json:"city"`
	PostalCode  string  `json:"postalCode"`
	Country     string  `json:"country"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`
}

// ServicePoint represents a carrier service point (e.g., pickup location).
type ServicePoint struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Address     Address `json:"address"`
	OpeningHours string `json:"openingHours,omitempty"`
	Services    []string `json:"services,omitempty"`
}
