package api
// /api/core.go

import (
	"net/http"
	"time"
)

// ==========================================
// STRATEGY & ADAPTER PATTERN: Core Structures
// ==========================================

// Dimensions represents the physical package layout constraints required
// by enterprise WMS and major Scandinavian carrier grids.
type Dimensions struct {
	LengthCM float64 `json:"length_cm"`
	WidthCM  float64 `json:"width_cm"`
	HeightCM float64 `json:"height_cm"`
}

// ColliItem defines an individual physical package within a multi-colli shipment.
type ColliItem struct {
	WeightKG   float64    `json:"weight_kg"`
	Dimensions Dimensions `json:"dimensions"`
}

// CashOnDelivery represents the financial collection instructions triggered
// upon physical delivery of the goods to the recipient (COD).
type CashOnDelivery struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// CustomsItem handles international trade compliance datasets for Non-EU zones.
type CustomsItem struct {
	HSCode           string  `json:"hs_code"`
	Description      string  `json:"description"`
	Value            float64 `json:"value"`
	Currency         string  `json:"currency"`
	CountryOfOrigin  string  `json:"country_of_origin"`
}

// Destination models the delivery target coordinates and location parameters.
type Destination struct {
	CountryCode string `json:"country_code"` // e.g., "DK", "NO", "GB"
	Type        string `json:"type"`         // e.g., "home", "shop"
}

// BookingRequest represents the primary unified ingress data payload received from
// an API client or ERP system trying to secure carrier shipment fulfillment assets.
type BookingRequest struct {
	CarrierCode        string          `json:"carrier_code"`
	IncludeReturnLabel bool            `json:"include_return_label,omitempty"`
	ReturnFormat       string          `json:"return_format,omitempty"` // "pdf" or "qr"
	Incoterm           string          `json:"incoterm,omitempty"`      // "DDP" or "DAP"
	Destination        Destination     `json:"destination"`
	Colli              []ColliItem     `json:"colli"`
	CustomsItems       []CustomsItem   `json:"customs_items,omitempty"`
	CashOnDelivery     *CashOnDelivery `json:"cash_on_delivery,omitempty"`
}

// BookingResult defines the normalized response payload returned to the client.
type BookingResult struct {
	BookingID      string   `json:"booking_id"`
	Status         string   `json:"status"`
	LabelURL       string   `json:"label_url"`
	ReturnLabelURL string   `json:"return_label_url,omitempty"`
	ReturnFormat   string   `json:"return_format,omitempty"`
	Errors         []string `json:"errors,omitempty"`
}

// CarrierStrategy defines the strict unified interface contract for all integrated courier engines.
type CarrierStrategy interface {
	ExecuteBooking(req BookingRequest) (*BookingResult, error)
}

// ==========================================
// OBSERVER PATTERN: Technical Telemetry
// ==========================================

// ExceptionEvent encapsulates critical runtime failure states for background logging.
type ExceptionEvent struct {
	Carrier      string
	Endpoint     string
	ErrorMessage string
	Timestamp    time.Time
}

// EventObserver defines the interface for subscribing to core system telemetry exceptions.
type EventObserver interface {
	OnException(event ExceptionEvent)
}

// TechnicalLogger writes critical telemetry diagnostic info to stdout in English.
type TechnicalLogger struct{}

func (tl TechnicalLogger) OnException(event ExceptionEvent) {
	println("\n🛑 [CRITICAL LOG] [" + event.Timestamp.Format(time.RFC3339) + "] Carrier: " + event.Carrier + " | Endpoint: " + event.Endpoint + " | Error: " + event.ErrorMessage)
}

// EventManager orchestrates live stream dispatching to registered decoupled observers.
type EventManager struct {
	observers []EventObserver
}

// GlobalEM acts as the central messaging registry for the entire execution thread context.
var GlobalEM = &EventManager{
	observers: []EventObserver{TechnicalLogger{}},
}

func (em *EventManager) Notify(event ExceptionEvent) {
	for _, observer := range em.observers {
		observer.OnException(event)
	}
}

// DummyHandler for Vercel deployment compliance.
func DummyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status": "Core engine online"}`))
}