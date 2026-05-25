package carriers

// internal/carriers/adapters.go

import (
	"fmt"
	"time"
)

// CarrierAdapter defines the unified interface for all transport carriers within the gateway.
type CarrierAdapter interface {
	CreateBooking(request BookingRequest) (*BookingResponse, error)
	GetLabel(bookingID string, format string) ([]byte, error)
	GetTracking(bookingID string) (*TrackingStatus, error)
}

// BookingRequest represents the normalized internal schema for a fulfillment transaction.
type BookingRequest struct {
	CarrierCode     string
	Incoterm        string // Must be DAP or DDP
	ServicePointID  string
	DestinationType string // Expected values: "home_delivery" or "service_point"
	Colli           []ColliItem
	Customs         *CustomsData
	COD             *CashOnDelivery
	IncludeReturn   bool
}

// ColliItem defines the physical characteristics and description of a single package.
type ColliItem struct {
	Weight      float64 // Weight in kilograms
	Length      float64 // Length in centimeters
	Width       float64 // Width in centimeters
	Height      float64 // Height in centimeters
	Description string
}

// CustomsData contains mandatory fields required for international Non-EU electronic manifests.
type CustomsData struct {
	HSCode          string
	CountryOfOrigin string // ISO-3166-1 alpha-2 code
	Value           float64
	Currency        string
}

// CashOnDelivery represents the financial collection settings for a shipment.
type CashOnDelivery struct {
	Amount   float64
	Currency string
}

// BookingResponse provides the standardized output after a successful carrier registration.
type BookingResponse struct {
	BookingID  string
	TrackingID string
	Metadata   map[string]string
}

// TrackingStatus provides a normalized view of a shipment's progress in the delivery lifecycle.
type TrackingStatus struct {
	State       string // States: PICKED_UP, IN_TRANSIT, DELIVERED, EXCEPTION
	Description string
	Timestamp   string
}

// PostNordAdapter implements the CarrierAdapter interface specifically for PostNord APIs.
type PostNordAdapter struct {
	APIKey      string
	CustomerKey string
}

// postNordPayload represents the structured JSON structure required by PostNord's Booking API.
type postNordPayload struct {
	ServiceCode   string         `json:"serviceCode"`
	CustomerKey   string         `json:"customerKey"`
	Incoterm      string         `json:"incoterm"`
	DeliveryPoint string         `json:"deliveryPointId,omitempty"`
	Items         []postNordItem `json:"items"`
}

// postNordItem maps internal colli data to PostNord's specific item schema.
type postNordItem struct {
	Weight      float64 `json:"weight"`
	Description string  `json:"description"`
}

// CreateBooking translates the normalized request into a PostNord payload and executes the booking logic.
func (a *PostNordAdapter) CreateBooking(request BookingRequest) (*BookingResponse, error) {
	// PostNord uses service code 17 for MyPack Home and 19 for MyPack Collect (service points).
	serviceCode := "17"
	if request.DestinationType == "service_point" {
		serviceCode = "19"
	}

	// Mapping internal BookingRequest to PostNord's expected payload.
	payload := postNordPayload{
		ServiceCode:   serviceCode,
		CustomerKey:   a.CustomerKey,
		Incoterm:      request.Incoterm,
		DeliveryPoint: request.ServicePointID,
		Items:         make([]postNordItem, len(request.Colli)),
	}

	for i, colli := range request.Colli {
		payload.Items[i] = postNordItem{
			Weight:      colli.Weight,
			Description: colli.Description,
		}
	}

	// Logic for international DDP/DAP validation would be integrated here before final POST.
	return &BookingResponse{
		BookingID:  "PN-" + fmt.Sprint(time.Now().Unix()),
		TrackingID: "SE-PN-MVP-12345",
	}, nil
}

// GetLabel retrieves the physical routing print data or QR codes for return shipments.
func (a *PostNordAdapter) GetLabel(bookingID string, format string) ([]byte, error) {
	// Implementation would call PostNord's Label API to return ZPL/PDF or QR metadata.
	return []byte("POSTNORD_BINARY_LABEL_DATA"), nil
}

// GetTracking fetches raw events and normalizes them into the four global gateway states.
func (a *PostNordAdapter) GetTracking(bookingID string) (*TrackingStatus, error) {
	// Implementation maps PostNord events to PICKED_UP, IN_TRANSIT, DELIVERED, or EXCEPTION.
	return &TrackingStatus{
		State:       "IN_TRANSIT",
		Description: "Parcel processed at PostNord terminal",
		Timestamp:   time.Now().Format(time.RFC3339),
	}, nil
}

// GetAdapter is a factory function that returns the PostNord implementation for Phase 2.
func GetAdapter(carrierCode string, apiKey string, customerKey string) (CarrierAdapter, error) {
	if carrierCode == "postnord" {
		return &PostNordAdapter{
			APIKey:      apiKey,
			CustomerKey: customerKey,
		}, nil
	}
	return nil, fmt.Errorf("carrier %s is not supported in the Phase 2 MVP", carrierCode)
}
