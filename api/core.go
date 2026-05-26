package api
// /api/core.go

import (
	"fmt"
	"time"
)

// DummyHandler er udelukkende til for at stille Vercels compiler tilfreds,
// da denne fil kun fungerer som et delt bibliotek for vores andre endpoints.
func DummyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "Core engine online"}`))
}

// ==========================================
// STRATEGY & ADAPTER PATTERN: Core Interfaces
// ==========================================

// BookingRequest er vores universelle interne format (baseret på bookings-v1.json)
type BookingRequest struct {
	CarrierCode        string        `json:"carrier_code"`
	IncludeReturnLabel bool          `json:"include_return_label,omitempty"`
	Colli              []interface{} `json:"colli"`
}

// BookingResult er det standardiserede svar, som vores CLI/API forventer tilbage
type BookingResult struct {
	BookingID      string `json:"booking_id"`
	Status         string `json:"status"`
	LabelURL       string `json:"label_url"`
	ReturnLabelURL string `json:"return_label_url,omitempty"`
}

// CarrierStrategy er kontrakten. Alle transportør-adaptere skal opfylde denne.
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

// TechnicalLogger skriver kritiske fejl til terminalen/loggen på engelsk
type TechnicalLogger struct{}

func (tl TechnicalLogger) OnException(event ExceptionEvent) {
	fmt.Printf("\n🛑 [CRITICAL LOG] [%s] Carrier: %s | Endpoint: %s | Error: %s\n",
		event.Timestamp.Format(time.RFC3339), event.Carrier, event.Endpoint, event.ErrorMessage)
}

type EventManager struct {
	observers []EventObserver
}

// GlobalEM er vores centrale omdeler-enhed til events
var GlobalEM = &EventManager{
	observers: []EventObserver{TechnicalLogger{}},
}

func (em *EventManager) Notify(event ExceptionEvent) {
	for _, observer := range em.observers {
		observer.OnException(event)
	}
}
