// Package adapter provides interfaces and shared types for carrier integrations.
// This file is located at /internal/adapter/adapter.go.
package adapter

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"
)

// CarrierAdapter defines the interface for all carrier adapters.
type CarrierAdapter interface {
	// BookShipment books a shipment with the carrier and returns a tracking number and label URL.
	BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error)

	// TrackShipment retrieves the tracking status for a shipment.
	TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error)
}

// Registry holds the available CarrierAdapters and selects one by name.
// It is the single owner of the adapter map; nothing outside this type reads
// or writes the map directly.
type Registry struct {
	adapters map[string]CarrierAdapter
}

// NewRegistry initialises all carrier adapters and returns a Registry ready
// for use. It is the primary entry point for wiring adapters into the
// application; InitAdapters is retained for use in tests that need the raw
// map.
func NewRegistry(log *zap.Logger) *Registry {
	return &Registry{adapters: InitAdapters(log)}
}

// Select returns the CarrierAdapter registered under carrier.
// Returns an error if the carrier is not registered.
func (r *Registry) Select(carrier string) (CarrierAdapter, error) {
	a, ok := r.adapters[carrier]
	if !ok {
		return nil, fmt.Errorf("carrier %q is not supported", carrier)
	}
	return a, nil
}

// Carriers returns the names of all registered carriers in undefined order.
func (r *Registry) Carriers() []string {
	keys := make([]string, 0, len(r.adapters))
	for k := range r.adapters {
		keys = append(keys, k)
	}
	return keys
}

// InitAdapters initializes all carrier adapters based on environment variables.
func InitAdapters(log *zap.Logger) map[string]CarrierAdapter {
	adapters := make(map[string]CarrierAdapter)
	mockMode := os.Getenv("MOCK_MODE") == "true"

	postNordAPIKey := os.Getenv("POSTNORD_API_KEY")
	if postNordAPIKey != "" && !mockMode {
		adapters["postnord"] = NewPostNordAdapter(postNordAPIKey, log)
		log.Info("PostNord adapter initialized in production mode")
	} else {
		adapters["postnord"] = &MockPostNordAdapter{}
		log.Info("PostNord adapter initialized in mock mode")
	}

	bringAPIKey := os.Getenv("BRING_API_KEY")
	bringCustomerID := os.Getenv("BRING_CUSTOMER_ID")
	if bringAPIKey != "" && bringCustomerID != "" && !mockMode {
		adapters["bring"] = NewBringAdapter(bringAPIKey, bringCustomerID, log)
		log.Info("Bring adapter initialized in production mode")
	} else {
		adapters["bring"] = &MockBringAdapter{}
		log.Info("Bring adapter initialized in mock mode")
	}

	glsAPIKey := os.Getenv("GLS_API_KEY")
	contractID := os.Getenv("GLS_CONTRACT_ID")
	if glsAPIKey != "" && !mockMode {
		adapters["gls"] = NewGLSAdapter(glsAPIKey, contractID, log)
		log.Info("GLS adapter initialized in production mode")
	} else {
		adapters["gls"] = &MockGLSAdapter{}
		log.Info("GLS adapter initialized in mock mode")
	}

	daoCustomerID := os.Getenv("DAO_CUSTOMER_ID")
	daoAPIKey := os.Getenv("DAO_API_KEY")
	if daoCustomerID != "" && daoAPIKey != "" && !mockMode {
		adapters["dao"] = NewDAOAdapter(daoCustomerID, daoAPIKey, log)
		log.Info("DAO adapter initialized in production mode")
	} else {
		adapters["dao"] = &MockDAOAdapter{}
		log.Info("DAO adapter initialized in mock mode")
	}

	postiAPIKey := os.Getenv("POSTI_API_KEY")
	if postiAPIKey != "" && !mockMode {
		adapters["posti"] = NewPostiAdapter(postiAPIKey, log)
		log.Info("Posti adapter initialized in production mode")
	} else {
		adapters["posti"] = &MockPostiAdapter{}
		log.Info("Posti adapter initialized in mock mode")
	}

	inpostAPIKey := os.Getenv("INPOST_API_KEY")
	if inpostAPIKey != "" && !mockMode {
		adapters["inpost"] = NewInPostAdapter(inpostAPIKey, log)
		log.Info("InPost adapter initialized in production mode")
	} else {
		adapters["inpost"] = &MockInPostAdapter{}
		log.Info("InPost adapter initialized in mock mode")
	}

	return adapters
}

// BookingRequest represents a generic shipment booking request.
type BookingRequest struct {
	Carrier        string   `json:"carrier" validate:"required"`
	Shipment       Shipment `json:"shipment" validate:"required"`
	CallbackURL    string   `json:"callbackUrl,omitempty"`
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
	Incoterms      string   `json:"incoterms,omitempty" validate:"omitempty,oneof=EXW FCA CPT CIP DAP DPU DDP FAS FOB CFR CIF"`
	HSCode         string   `json:"hsCode,omitempty"`
}

// Shipment represents the shipment details.
type Shipment struct {
	Sender      Address `json:"sender" validate:"required"`
	Receiver    Address `json:"receiver" validate:"required"`
	TotalWeight float64 `json:"totalWeight" validate:"required,gt=0"`
	Colli       []Colli `json:"colli" validate:"required,min=1"`
	Incoterms   string  `json:"incoterms,omitempty"`
	HSCode      string  `json:"hsCode,omitempty"`
}

// Colli represents an individual package in a shipment.
type Colli struct {
	ID         string     `json:"id" validate:"required"`
	Reference  string     `json:"reference,omitempty"`
	Weight     float64    `json:"weight" validate:"gt=0"`
	Dimensions Dimensions `json:"dimensions"`
	Items      []Item     `json:"items" validate:"required,min=1"`
}

// Dimensions represents the physical dimensions of a package in centimetres.
type Dimensions struct {
	Length float64 `json:"length"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Address represents a physical address.
//
// Street holds the street name only — no house number. HouseNumber is kept
// as a separate field because some carriers (InPost, GLS) require them
// distinct on the wire. Adapters that do not distinguish concatenate
// Street + " " + HouseNumber when building the payload.
//
// Supplement carries anything that does not fit on the street line:
// building name, floor, apartment, attention line, or care-of. It maps to
// what carriers variously call "address line 2", "co", or "addressLine2" —
// the name here is intentionally carrier-agnostic.
type Address struct {
	Name        string `json:"name"        validate:"required"`
	Street      string `json:"street"      validate:"required"`
	HouseNumber string `json:"houseNumber,omitempty"`
	Supplement  string `json:"supplement,omitempty"`
	City        string `json:"city"        validate:"required"`
	PostalCode  string `json:"postalCode"  validate:"required"`
	Country     string `json:"country"     validate:"required"`
	Phone       string `json:"phone,omitempty"`
	Email       string `json:"email,omitempty"`
}

// Item represents an item in a colli.
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
	LockerId       string          `json:"lockerId,omitempty"`
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

// TrackingEvent represents a single tracking event.
type TrackingEvent struct {
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"`
	Location  string `json:"location,omitempty"`
	Details   string `json:"details,omitempty"`
}
