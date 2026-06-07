// Package adapter provides interfaces and shared types for carrier integrations.
// This file is located at /internal/adapter/adapter.go.
package adapter

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"
)

// LabelFormat is the requested label output format.
type LabelFormat string

const (
	LabelFormatPDF   LabelFormat = "PDF"
	LabelFormatPNG   LabelFormat = "PNG"
	LabelFormatZPL   LabelFormat = "ZPL"
	LabelFormatEPL   LabelFormat = "EPL"
	LabelFormatZPLGK LabelFormat = "ZPLGK"
)

// LabelRequest specifies which label to fetch and in what format.
type LabelRequest struct {
	// TrackingNumber identifies the shipment whose label to fetch.
	TrackingNumber string
	// Format is the requested output format.
	Format LabelFormat
}

// LabelResponse contains the base64-encoded label data ready for printing.
type LabelResponse struct {
	TrackingNumber string      `json:"trackingNumber"`
	Carrier        string      `json:"carrier"`
	Format         LabelFormat `json:"format"`
	// Data is the base64-encoded label content.
	Data     string `json:"data"`
	// MimeType describes the content (e.g. application/pdf, image/png).
	MimeType string `json:"mimeType"`
}

// mimeTypes maps label formats to their MIME type.
var mimeTypes = map[LabelFormat]string{
	LabelFormatPDF:   "application/pdf",
	LabelFormatPNG:   "image/png",
	LabelFormatZPL:   "application/x-zpl",
	LabelFormatEPL:   "application/x-epl",
	LabelFormatZPLGK: "application/x-zpl",
}

// MimeTypeForFormat returns the MIME type for the given label format.
func MimeTypeForFormat(f LabelFormat) string {
	if mt, ok := mimeTypes[f]; ok {
		return mt
	}
	return "application/octet-stream"
}

// CarrierAdapter defines the interface for all carrier adapters.
type CarrierAdapter interface {
	// BookShipment books a shipment with the carrier and returns a tracking number and label URL.
	BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error)

	// TrackShipment retrieves the tracking status for a shipment.
	TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error)

	// FetchLabel retrieves the label for a shipment in the requested format.
	// The label data is returned as base64-encoded bytes.
	FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error)
}

// Registry holds the available CarrierAdapters and selects one by name.
// It is the single owner of the adapter map; nothing outside this type reads
// or writes the map directly.
type Registry struct {
	adapters map[string]CarrierAdapter
}

// NewRegistryFromMap creates a Registry from an already-initialised adapter
// map. Intended for tests that need precise control over which adapters are
// present without going through InitAdapters or touching env vars.
func NewRegistryFromMap(adapters map[string]CarrierAdapter) *Registry {
	return &Registry{adapters: adapters}
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

// carrierCapabilities describes optional features a carrier's API supports natively.
type carrierCapabilities struct {
	// NativeIdempotency is true when the carrier's booking API accepts an
	// idempotency key directly and uses it for server-side deduplication.
	// When false, deduplication must be handled by the integrating system.
	NativeIdempotency bool
	// Beta is true when the carrier integration is not yet fully validated
	// for production use. Callers receive a warning in the booking response.
	Beta bool
}

// capabilities maps carrier keys to their known API capabilities.
var capabilities = map[string]carrierCapabilities{
	"postnord": {NativeIdempotency: true},
	"bring":    {NativeIdempotency: false},
	"gls":      {NativeIdempotency: false},
	"dao":      {NativeIdempotency: false, Beta: true},
	"posti":    {NativeIdempotency: false},
	"inpost":   {NativeIdempotency: false},
}

// SupportsNativeIdempotency reports whether the given carrier accepts an
// idempotency key directly in its booking API. When false, the integrating
// system is responsible for deduplication using the key.
func SupportsNativeIdempotency(carrier string) bool {
	return capabilities[carrier].NativeIdempotency
}

// IsBeta reports whether the given carrier integration is in beta.
// Beta carriers are functional but not fully validated for production use.
func IsBeta(carrier string) bool {
	return capabilities[carrier].Beta
}

// InitAdapters initializes all carrier adapters based on environment variables.
func InitAdapters(log *zap.Logger) map[string]CarrierAdapter {
	adapters := make(map[string]CarrierAdapter)
	mockMode := os.Getenv("MOCK_MODE") == "true"

	postNordAPIKey := os.Getenv("POSTNORD_API_KEY")
	switch {
	case mockMode:
		adapters["postnord"] = &MockPostNordAdapter{}
		log.Info("PostNord adapter initialized in mock mode (MOCK_MODE=true)")
	case postNordAPIKey == "":
		adapters["postnord"] = &MockPostNordAdapter{}
		log.Warn("PostNord adapter falling back to mock mode (POSTNORD_API_KEY not set)")
	default:
		adapters["postnord"] = NewPostNordAdapter(postNordAPIKey, log)
		log.Info("PostNord adapter initialized in production mode")
	}

	bringAPIKey := os.Getenv("BRING_API_KEY")
	bringCustomerID := os.Getenv("BRING_CUSTOMER_ID")
	switch {
	case mockMode:
		adapters["bring"] = &MockBringAdapter{}
		log.Info("Bring adapter initialized in mock mode (MOCK_MODE=true)")
	case bringAPIKey == "" || bringCustomerID == "":
		adapters["bring"] = &MockBringAdapter{}
		log.Warn("Bring adapter falling back to mock mode (BRING_API_KEY or BRING_CUSTOMER_ID not set)")
	default:
		adapters["bring"] = NewBringAdapter(bringAPIKey, bringCustomerID, log)
		log.Info("Bring adapter initialized in production mode")
	}

	glsAPIKey := os.Getenv("GLS_API_KEY")
	glsClientSecret := os.Getenv("GLS_CLIENT_SECRET")
	contractID := os.Getenv("GLS_CONTRACT_ID")
	switch {
	case mockMode:
		adapters["gls"] = &MockGLSAdapter{}
		log.Info("GLS adapter initialized in mock mode (MOCK_MODE=true)")
	case glsAPIKey == "" || glsClientSecret == "":
		adapters["gls"] = &MockGLSAdapter{}
		log.Warn("GLS adapter falling back to mock mode (GLS_API_KEY or GLS_CLIENT_SECRET not set)")
	default:
		adapters["gls"] = NewGLSAdapter(glsAPIKey, glsClientSecret, contractID, log)
		log.Info("GLS adapter initialized in production mode")
	}

	daoCustomerID := os.Getenv("DAO_CUSTOMER_ID")
	daoAPIKey := os.Getenv("DAO_API_KEY")
	switch {
	case mockMode:
		adapters["dao"] = &MockDAOAdapter{}
		log.Info("DAO adapter initialized in mock mode (MOCK_MODE=true)")
	case daoCustomerID == "" || daoAPIKey == "":
		adapters["dao"] = &MockDAOAdapter{}
		log.Warn("DAO adapter falling back to mock mode (DAO_CUSTOMER_ID or DAO_API_KEY not set)")
	default:
		adapters["dao"] = NewDAOAdapter(daoCustomerID, daoAPIKey, log)
		log.Info("DAO adapter initialized in production mode (beta)")
	}

	postiAPIKey := os.Getenv("POSTI_API_KEY")
	switch {
	case mockMode:
		adapters["posti"] = &MockPostiAdapter{}
		log.Info("Posti adapter initialized in mock mode (MOCK_MODE=true)")
	case postiAPIKey == "":
		adapters["posti"] = &MockPostiAdapter{}
		log.Warn("Posti adapter falling back to mock mode (POSTI_API_KEY not set)")
	default:
		adapters["posti"] = NewPostiAdapter(postiAPIKey, log)
		log.Info("Posti adapter initialized in production mode")
	}

	inpostAPIKey := os.Getenv("INPOST_API_KEY")
	switch {
	case mockMode:
		adapters["inpost"] = &MockInPostAdapter{}
		log.Info("InPost adapter initialized in mock mode (MOCK_MODE=true)")
	case inpostAPIKey == "":
		adapters["inpost"] = &MockInPostAdapter{}
		log.Warn("InPost adapter falling back to mock mode (INPOST_API_KEY not set)")
	default:
		adapters["inpost"] = NewInPostAdapter(inpostAPIKey, log)
		log.Info("InPost adapter initialized in production mode")
	}

	return adapters
}

// Customs holds cross-border declaration data required for non-EU
// destinations and EU B2B shipments above the de minimis threshold.
// It is optional for domestic and intra-EU B2C shipments below 150 EUR.
type Customs struct {
	// Incoterms is the trade term (e.g. DDP, DAP). Required for non-EU destinations.
	Incoterms string `json:"incoterms,omitempty"`

	// TransportMode is the mode of transport for the shipment.
	// Accepted values: "sea", "air", "road", "rail".
	// Some Incoterms are only valid for sea transport (FOB, FAS, CFR, CIF).
	TransportMode string `json:"transportMode,omitempty"`

	// HSCode is the 6-10 digit Harmonized System commodity code.
	// Required for non-EU destinations and EU shipments above de minimis.
	HSCode string `json:"hsCode,omitempty"`

	// CountryOfOrigin is the ISO 3166-1 alpha-2 country code where the goods
	// were manufactured or substantially transformed. Distinct from the
	// sender's address country and used for rules of origin checks.
	CountryOfOrigin string `json:"countryOfOrigin,omitempty"`

	// CustomsValue is the declared value of the shipment for customs purposes.
	CustomsValue float64 `json:"customsValue,omitempty"`

	// CustomsCurrency is the ISO 4217 currency code for CustomsValue (e.g. DKK, EUR, NOK).
	CustomsCurrency string `json:"customsCurrency,omitempty"`

	// ImporterOfRecord is the VAT or EORI number of the importer.
	// Required for non-EU destinations (e.g. Norway).
	ImporterOfRecord string `json:"importerOfRecord,omitempty"`

	// ImporterVATNumber is the VAT registration number of the receiver.
	// Required for EU B2B shipments.
	ImporterVATNumber string `json:"importerVatNumber,omitempty"`

	// ExporterVATNumber is the VAT registration number of the sender.
	// Required for non-EU destinations.
	ExporterVATNumber string `json:"exporterVatNumber,omitempty"`

	// ShipmentType is either "B2B" or "B2C". Affects VAT and de minimis rules.
	ShipmentType string `json:"shipmentType,omitempty"`
}

// BookingRequest represents a generic shipment booking request.
type BookingRequest struct {
	Carrier        string   `json:"carrier"        validate:"required"`
	Shipment       Shipment `json:"shipment"       validate:"required"`
	CallbackURL    string   `json:"callbackUrl,omitempty"`
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
}

// Shipment represents the shipment details.
type Shipment struct {
	Sender      Address  `json:"sender"      validate:"required"`
	Receiver    Address  `json:"receiver"    validate:"required"`
	TotalWeight float64  `json:"totalWeight" validate:"required,gt=0"`
	Colli       []Colli  `json:"colli"       validate:"required,min=1"`
	// DeliveryType controls the shipping product. When empty the adapter
	// selects a sensible default based on whether ServicePointID is set.
	// Accepted values: "home", "business", "servicepoint", "return".
	DeliveryType string `json:"deliveryType,omitempty"`
	// Customs holds cross-border declaration data. Required for non-EU
	// destinations and EU B2B shipments above the de minimis threshold.
	Customs Customs `json:"customs,omitempty"`
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
// building name, floor, apartment, attention line, or care-of.
//
// State holds the state, province, or region code where required by the
// destination country (e.g. "CA" for California, "ON" for Ontario).
//
// ServicePointID identifies a carrier service point (parcel shop, pickup
// point, locker) for receiver addresses. When set, Street, City, and
// PostalCode are optional for the receiver — the carrier routes to the
// service point directly. Each carrier maps this to its own wire field name:
//
//	- PostNord: servicePointId
//	- Bring/Posti: pickupPointId
//	- GLS: parcelShopId
//	- DAO: lockerId
//	- InPost: targetLocker (service block)
type Address struct {
	Name           string `json:"name"           validate:"required"`
	Street         string `json:"street"         validate:"required"`
	HouseNumber    string `json:"houseNumber,omitempty"`
	Supplement     string `json:"supplement,omitempty"`
	City           string `json:"city"           validate:"required"`
	PostalCode     string `json:"postalCode"     validate:"required"`
	Country        string `json:"country"        validate:"required"`
	State          string `json:"state,omitempty"`
	ServicePointID string `json:"servicePointId,omitempty"`
	Phone          string `json:"phone,omitempty"`
	Email          string `json:"email,omitempty"`
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
	ShipmentID       string          `json:"shipmentId,omitempty"`
	TrackingNumber   string          `json:"trackingNumber"`
	LabelURL         string          `json:"labelUrl,omitempty"`
	Carrier          string          `json:"carrier"`
	Cost             float64         `json:"cost,omitempty"`
	Currency         string          `json:"currency,omitempty"`
	ServiceLevel     string          `json:"serviceLevel,omitempty"`
	Status           string          `json:"status,omitempty"`
	Colli            []ColliResponse `json:"colli,omitempty"`
	Errors           []string        `json:"errors,omitempty"`
	LockerId         string          `json:"lockerId,omitempty"`
	ServicePointID   string          `json:"servicePointId,omitempty"`
	// FlaggedForReview is true when the address passed a ReviewRequired
	// validation — the booking was accepted but should be checked manually.
	FlaggedForReview bool `json:"flaggedForReview,omitempty"`
	// BetaWarning is set when the carrier integration is in beta.
	BetaWarning string `json:"betaWarning,omitempty"`
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
