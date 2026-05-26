package api
// /api/core.go

import (
	"fmt"
	"net/http"
	"time"
)

// ==========================================
// STRATEGY & ADAPTER PATTERN: Core Structures
// ==========================================

type CustomsItem struct {
	HSCode      string  `json:"hs_code"`
	Description string  `json:"description"`
	Value       float64 `json:"value"`
	Currency    string  `json:"currency"`
}

type Destination struct {
	CountryCode string `json:"country_code"` // fx "DK", "GB", "NO"
	Type        string `json:"type"`         // fx "home", "shop"
}

// BookingRequest er udvidet til Trade Compliance og QR-returformater
type BookingRequest struct {
	CarrierCode        string        `json:"carrier_code"`
	IncludeReturnLabel bool          `json:"include_return_label,omitempty"`
	ReturnFormat       string        `json:"return_format,omitempty"` // "pdf" eller "qr"
	Incoterm           string        `json:"incoterm,omitempty"`      // "DDP" eller "DAP"
	Destination        Destination   `json:"destination"`
	Colli              []interface{} `json:"colli"`
	CustomsItems       []CustomsItem `json:"customs_items,omitempty"`
}

type BookingResult struct {
	BookingID      string   `json:"booking_id"`
	Status         string   `json:"status"`
	LabelURL       string   `json:"label_url"`
	ReturnLabelURL string   `json:"return_label_url,omitempty"`
	ReturnFormat   string   `json:"return_format,omitempty"`
	Errors         []string `json:"errors,omitempty"`
}

type CarrierStrategy interface {
	ExecuteBooking(req BookingRequest) (*BookingResult, error)
}

// ==========================================
// OBSERVER PATTERN: Technical Telemetry
// ==========================================

type ExceptionEvent struct {
	Carrier      string
	Endpoint     string
	ErrorMessage string
	Timestamp    time.Time
}

type EventObserver interface {
	OnException(event ExceptionEvent)
}

type TechnicalLogger struct{}

func (tl TechnicalLogger) OnException(event ExceptionEvent) {
	fmt.Printf("\n🛑 [CRITICAL LOG] [%s] Carrier: %s | Endpoint: %s | Error: %s\n",
		event.Timestamp.Format(time.RFC3339), event.Carrier, event.Endpoint, event.ErrorMessage)
}

type EventManager struct {
	observers []EventObserver
}

var GlobalEM = &EventManager{
	observers: []EventObserver{TechnicalLogger{}},
}

func (em *EventManager) Notify(event ExceptionEvent) {
	for _, observer := range em.observers {
		observer.OnException(event)
	}
}

// DummyHandler for Vercel deployment compliance
func DummyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "Core engine online"}`))
}