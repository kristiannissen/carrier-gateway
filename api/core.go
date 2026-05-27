package api
// /api/core.go

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ==========================================
// DATA MODELS & SCHEMA CONSTRAINTS
// ==========================================

type Dimensions struct {
	LengthCM float64 `json:"length_cm"`
	WidthCM  float64 `json:"width_cm"`
	HeightCM float64 `json:"height_cm"`
}

type ColliItem struct {
	WeightKG   float64    `json:"weight_kg"`
	Dimensions Dimensions `json:"dimensions"`
}

type CashOnDelivery struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type CustomsItem struct {
	HSCode          string  `json:"hs_code"`
	Description     string  `json:"description"`
	Value           float64 `json:"value"`
	Currency        string  `json:"currency"`
	CountryOfOrigin string  `json:"country_of_origin"`
}

type Destination struct {
	CountryCode string `json:"country_code"`
	Type        string `json:"type"`
}

type BookingRequest struct {
	CarrierCode        string          `json:"carrier_code"`
	IncludeReturnLabel bool            `json:"include_return_label,omitempty"`
	ReturnFormat       string          `json:"return_format,omitempty"`
	Incoterm           string          `json:"incoterm,omitempty"`
	Destination        Destination     `json:"destination"`
	Colli              []ColliItem     `json:"colli"`
	CustomsItems       []CustomsItem   `json:"customs_items,omitempty"`
	CashOnDelivery     *CashOnDelivery `json:"cash_on_delivery,omitempty"`
}

type BookingResult struct {
	BookingID      string   `json:"booking_id"`
	Status         string   `json:"status"`
	LabelURL       string   `json:"label_url"`
	ReturnLabelURL string   `json:"return_label_url,omitempty"`
	ReturnFormat   string   `json:"return_format,omitempty"`
	Errors         []string `json:"errors,omitempty"`
}

// ==========================================
// STRATEGY PATTERN CORE IMPLEMENTATION
// ==========================================

type CarrierStrategy interface {
	ExecuteBooking(req BookingRequest) (*BookingResult, error)
}

var (
	strategies      = make(map[string]CarrierStrategy)
	strategiesMutex sync.RWMutex
)

func RegisterStrategy(name string, strategy CarrierStrategy) {
	strategiesMutex.Lock()
	defer strategiesMutex.Unlock()
	strategies[strings.ToLower(name)] = strategy
}

func DispatchBooking(req BookingRequest) (*BookingResult, error) {
	strategiesMutex.RLock()
	strategy, exists := strategies[strings.ToLower(req.CarrierCode)]
	strategiesMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("carrier strategy '%s' is not supported or registered in gateway", req.CarrierCode)
	}
	return strategy.ExecuteBooking(req)
}

// ==========================================
// OBSERVER PATTERN: TELEMETRY LOGGING
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
	println("\n🛑 [CRITICAL LOG] [" + event.Timestamp.Format(time.RFC3339) + "] Carrier: " + event.Carrier + " | Endpoint: " + event.Endpoint + " | Error: " + event.ErrorMessage)
}

var GlobalEM = &EventManager{
	observers: []EventObserver{TechnicalLogger{}},
}

type EventManager struct {
	observers []EventObserver
}

func (em *EventManager) Notify(event ExceptionEvent) {
	for _, observer := range em.observers {
		observer.OnException(event)
	}
}

func DummyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status": "Core engine online"}`))
}