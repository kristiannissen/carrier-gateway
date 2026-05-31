// Package adapter provides interfaces and shared types for carrier integrations.
// This file is located at /internal/adapter/adapter.go.
package adapter

import (
	"log/slog"
	"os"
)

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

// InitAdapters initializes all carrier adapters based on environment variables.
func InitAdapters() map[string]CarrierAdapter {
	adapters := make(map[string]CarrierAdapter)
	mockMode := os.Getenv("MOCK_MODE") == "true"

	// PostNord
	postNordAPIKey := os.Getenv("POSTNORD_API_KEY")
	if postNordAPIKey != "" && !mockMode {
		adapters["postnord"] = NewPostNordAdapter(postNordAPIKey)
		slog.Info("PostNord adapter initialized in production mode")
	} else {
		adapters["postnord"] = &MockPostNordAdapter{}
		slog.Info("PostNord adapter initialized in mock mode")
	}

	// Bring
	bringAPIKey := os.Getenv("BRING_API_KEY")
	bringCustomerID := os.Getenv("BRING_CUSTOMER_ID")
	if bringAPIKey != "" && bringCustomerID != "" && !mockMode {
		adapters["bring"] = NewBringAdapter(bringAPIKey, bringCustomerID)
		slog.Info("Bring adapter initialized in production mode")
	} else {
		slog.Info("Bring adapter not initialized (missing API key or customer ID)")
	}

	// GLS
	glsAPIKey := os.Getenv("GLS_API_KEY")
	if glsAPIKey != "" && !mockMode {
		adapters["gls"] = NewGLSAdapter(glsAPIKey)
		slog.Info("GLS adapter initialized in production mode")
	} else {
		adapters["gls"] = &MockGLSAdapter{}
		slog.Info("GLS adapter initialized in mock mode")
	}

	// DAO
	daoCustomerID := os.Getenv("DAO_CUSTOMER_ID")
	daoAPIKey := os.Getenv("DAO_API_KEY")
	if daoCustomerID != "" && daoAPIKey != "" && !mockMode {
		adapters["dao"] = NewDAOAdapter(daoCustomerID, daoAPIKey)
		slog.Info("DAO adapter initialized in production mode")
	} else {
		adapters["dao"] = &MockDAOAdapter{}
		slog.Info("DAO adapter initialized in mock mode")
	}

	// Posti
	postiAPIKey := os.Getenv("POSTI_API_KEY")
	if postiAPIKey != "" && !mockMode {
		adapters["posti"] = NewPostiAdapter(postiAPIKey)
		slog.Info("Posti adapter initialized in production mode")
	} else {
		adapters["posti"] = &MockPostiAdapter{}
		slog.Info("Posti adapter initialized in mock mode")
	}

	return adapters
}

// BookingRequest represents a generic shipment booking request.
// All shipments are treated as a list of colli (single or multi-package).
type BookingRequest struct {
	Carrier        string   `json:"carrier"`
	Shipment       Shipment `json:"shipment"`
	CallbackURL    string   `json:"callbackUrl,omitempty"`
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
	Incoterms      string   `json:"incoterms,omitempty"`
	HSCode         string   `json:"hsCode,omitempty"`
}

// Shipment represents the shipment details.
// All shipments are treated as a list of colli (single or multi-package).
type Shipment struct {
	Sender      Address `json:"sender"`
	Receiver    Address `json:"receiver"`
	TotalWeight float64 `json:"totalWeight"` // Sum of all colli weights
	Colli       []Colli `json:"colli"`       // Always a list (1+ colli)
	Incoterms   string  `json:"incoterms,omitempty"`
	HSCode      string  `json:"hsCode,omitempty"`
}

// Colli represents an individual package in a shipment.
type Colli struct {
	ID         string  `json:"id"`
	Reference  string  `json:"reference,omitempty"`
	Weight     float64 `json:"weight"`
	Dimensions struct {
		Length float64 `json:"length"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"dimensions"`
	Items []Item `json:"items"`
}

// Address represents a physical address.
type Address struct {
	Name       string `json:"name"`
	Street     string `json:"street"`
	City       string `json:"city"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
	Phone      string `json:"phone,omitempty"`
	Email      string `json:"email,omitempty"`
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
	ShipmentID        string          `json:"shipmentId,omitempty"`
	TrackingNumber    string          `json:"trackingNumber"`
	Carrier           string          `json:"carrier"`
	Status            string          `json:"status"`
	Events            []TrackingEvent `json:"events"`
	EstimatedDelivery string          `json:"estimatedDelivery,omitempty"`
	Colli             []ColliTracking `json:"colli,omitempty"`
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

// Location represents a physical location (e.g., sender, receiver, or service point).
type Location struct {
	Name       string `json:"name,omitempty"`
	Street     string `json:"street,omitempty"`
	City       string `json:"city,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Country    string `json:"country,omitempty"`
	Phone      string `json:"phone,omitempty"`
	Email      string `json:"email,omitempty"`
}

// ServicePoint represents a carrier service point (e.g., pickup location).
type ServicePoint struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Address      Address  `json:"address"`
	OpeningHours string   `json:"openingHours,omitempty"`
	Services     []string `json:"services,omitempty"`
}

// Dimensions represents the physical dimensions of a package (length, width, height).
type Dimensions struct {
	Length float64 `json:"length"`   // Length in centimeters
	Width  float64 `json:"width"`    // Width in centimeters
	Height float64 `json:"height"`   // Height in centimeters
}
