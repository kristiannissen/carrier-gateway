package api
// /api/core.go

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// =========================================================================
// DATA MODELS & SCHEMA CONSTRAINTS
// =========================================================================

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
	IsAsync            bool            `json:"is_async,omitempty"`
	Destination        Destination     `json:"destination"`
	Colli              []ColliItem     `json:"colli"`
	CustomsItems       []CustomsItem   `json:"customs_items,omitempty"`
	CashOnDelivery     *CashOnDelivery `json:"cash_on_delivery,omitempty"`
}

type BookingResult struct {
	BookingID      string   `json:"booking_id"`
	Status         string   `json:"status"`
	LabelURL       string   `json:"label_url,omitempty"`
	ReturnLabelURL string   `json:"return_label_url,omitempty"`
	ReturnFormat   string   `json:"return_format,omitempty"`
	Errors         []string `json:"errors,omitempty"`
}

// =========================================================================
// STRATEGY PATTERN & ASYNC CORE DISPATCHER
// =========================================================================

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

	if req.IsAsync {
		queueID := fmt.Sprintf("ASYNC-JOB-%d", time.Now().UnixNano())
		if exists {
			go func(s CarrierStrategy, r BookingRequest) {
				_, _ = s.ExecuteBooking(r)
			}(strategy, req)
		}
		return &BookingResult{
			BookingID: queueID,
			Status:    "queued",
		}, nil
	}

	if !exists {
		mockBookingID := fmt.Sprintf("MOCK-%s-%d", strings.ToUpper(req.CarrierCode), time.Now().Unix())
		return &BookingResult{
			BookingID: mockBookingID,
			Status:    "completed (sandbox-fallback)",
			LabelURL:  fmt.Sprintf("https://mock-carrier-cdn.io/sandbox/labels/%s.pdf", mockBookingID),
		}, nil
	}
	return strategy.ExecuteBooking(req)
}

// =========================================================================
// OBSERVER PATTERN: TELEMETRY & STATUS ENGINE
// =========================================================================

type ExceptionEvent struct {
	Carrier      string    `json:"carrier"`
	Endpoint     string    `json:"endpoint"`
	ErrorMessage string    `json:"error_message"`
	Timestamp    time.Time `json:"timestamp"`
}

type EventObserver interface {
	OnException(event ExceptionEvent)
}

type TechnicalLogger struct{}

func (tl TechnicalLogger) OnException(event ExceptionEvent) {
	println("\n🛑 [CRITICAL LOG] [" + event.Timestamp.Format(time.RFC3339) + "] Carrier: " + event.Carrier + " | Endpoint: " + event.Endpoint + " | Error: " + event.ErrorMessage)
}

// InMemoryIncidentRecorder keeps track of the latest errors for the Status Page dashboard
type InMemoryIncidentRecorder struct {
	mu        sync.Mutex
	Incidents []ExceptionEvent
}

var IncidentTracker = &InMemoryIncidentRecorder{
	Incidents: make([]ExceptionEvent, 0),
}

func (r *InMemoryIncidentRecorder) OnException(event ExceptionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Caps historical incident records queue to the last 10 entries
	if len(r.Incidents) >= 10 {
		r.Incidents = r.Incidents[1:]
	}
	r.Incidents = append(r.Incidents, event)