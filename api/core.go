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

// --- ENTERPRISE DOMAIN MODELS ---

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
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	StreetName   string `json:"street_name"`
	StreetNumber string `json:"street_number"`
	PostalCode   string `json:"postal_code"`
	City         string `json:"city"`
	CountryCode  string `json:"country_code"`
	Type         string `json:"type"`           // "residential", "commercial", "locker"
	ParcelShopID string `json:"parcel_shop_id"` // Bruges til specifikke boks UUIDs/IDs
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

// --- NEW UNIFIED MODELS FOR TRACKING & SERVICE POINTS ---

type ServicePoint struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	StreetName   string `json:"street_name"`
	StreetNumber string `json:"street_number"`
	PostalCode   string `json:"postal_code"`
	City         string `json:"city"`
	CountryCode  string `json:"country_code"`
	Type         string `json:"type"` // "locker" eller "shop"
}

type TrackingEvent struct {
	Description string    `json:"description"`
	Status      string    `json:"status"` // "info_received", "in_transit", "out_for_delivery", "delivered", "exception"
	Location    string    `json:"location,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

type TrackingResult struct {
	TrackingID    string          `json:"tracking_id"`
	CarrierCode   string          `json:"carrier_code"`
	CurrentStatus string          `json:"current_status"`
	EstimatedFull time.Time       `json:"estimated_delivery,omitempty"`
	Events        []TrackingEvent `json:"events"`
}

// --- STRATEGY ENGINE INTERFACE ---

type CarrierStrategy interface {
	ExecuteBooking(req BookingRequest) (*BookingResult, error)
	LookupServicePoints(postalCode, countryCode string) ([]ServicePoint, error)
	GetTrackingStatus(trackingID string) (*TrackingResult, error)
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
		return &BookingResult{BookingID: queueID, Status: "queued"}, nil
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

// --- TELEMETRY AND OBSERVABILITY OBSERVERS ---

type ExceptionEvent struct {
	Carrier      string    `json:"carrier"`
	Endpoint     string    `json:"endpoint"`
	ErrorMessage string    `json:"error_message"`
	Timestamp    time.Time `json:"timestamp"`
}

type EventObserver interface{ OnException(event ExceptionEvent) }
type TechnicalLogger struct{}

func (tl TechnicalLogger) OnException(event ExceptionEvent) {
	println("\n🛑 [CRITICAL LOG] [" + event.Timestamp.Format(time.RFC3339) + "] Carrier: " + event.Carrier + " | Endpoint: " + event.Endpoint + " | Error: " + event.ErrorMessage)
}

type InMemoryIncidentRecorder struct {
	mu        sync.Mutex
	Incidents []ExceptionEvent
}

var IncidentTracker = &InMemoryIncidentRecorder{Incidents: make([]ExceptionEvent, 0)}

func (r *InMemoryIncidentRecorder) OnException(event ExceptionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.Incidents) >= 10 {
		r.Incidents = r.Incidents[1:]
	}
	r.Incidents = append(r.Incidents, event)
}

var GlobalEM = &EventManager{observers: []EventObserver{TechnicalLogger{}, IncidentTracker}}

type EventManager struct{ observers []EventObserver }

func (em *EventManager) Notify(event ExceptionEvent) {
	for _, observer := range em.observers {
		observer.OnException(event)
	}
}

type CarrierStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type SystemStatusResponse struct {
	GatewayStatus string           `json:"gateway_status"`
	Carriers      []CarrierStatus  `json:"carriers"`
	RecentErrors  []ExceptionEvent `json:"recent_errors"`
	Timestamp     time.Time        `json:"timestamp"`
}

func Handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	IncidentTracker.mu.Lock()
	errorsCopy := make([]ExceptionEvent, len(IncidentTracker.Incidents))
	copy(errorsCopy, IncidentTracker.Incidents)
	IncidentTracker.mu.Unlock()

	daoStatus := "operational"
	instabeeStatus := "operational"
	dhlStatus := "operational"
	budbeeStatus := "operational"
	packetaStatus := "operational"

	for _, entry := range errorsCopy {
		if time.Since(entry.Timestamp) < 5*time.Minute {
			if entry.Carrier == "dao" && entry.Endpoint != "Strategy-Engine" { daoStatus = "degraded" }
			if entry.Carrier == "instabee" { instabeeStatus = "degraded" }
			if entry.Carrier == "dhl" { dhlStatus = "degraded" }
			if entry.Carrier == "budbee" { budbeeStatus = "degraded" }
			if entry.Carrier == "packeta" { packetaStatus = "degraded" }
		}
	}

	response := SystemStatusResponse{
		GatewayStatus: "operational",
		Timestamp:     time.Now(),
		RecentErrors:  errorsCopy,
		Carriers: []CarrierStatus{
			{Name: "DAO (Dansk Avis Distribution)", Status: daoStatus},
			{Name: "Instabee (Instabox)", Status: instabeeStatus},
			{Name: "DHL Global Freight & Express", Status: dhlStatus},
			{Name: "Budbee Home & Box Delivery", Status: budbeeStatus},
			{Name: "Packeta (Zásilkovna) Central Europe", Status: packetaStatus},
		},
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

// --- NEW UNIFIED ROUTER HANDLERS FOR THE ENTENT WEB MAPPING ---

func UnifiedServicePointsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	carrier := strings.ToLower(r.URL.Query().Get("carrier"))
	postalCode := r.URL.Query().Get("postal_code")
	countryCode := strings.ToUpper(r.URL.Query().Get("country_code"))

	if carrier == "" || postalCode == "" || countryCode == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Missing query parameters: carrier, postal_code, country_code"})
		return
	}

	strategiesMutex.RLock()
	strategy, exists := strategies[carrier]
	strategiesMutex.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Carrier strategy '%s' not registered", carrier)})
		return
	}

	points, err := strategy.LookupServicePoints(postalCode, countryCode)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(points)
}

func UnifiedTrackingHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	carrier := strings.ToLower(r.URL.Query().Get("carrier"))
	trackingID := r.URL.Query().Get("id")

	if carrier == "" || trackingID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Missing query parameters: carrier, id"})
		return
	}

	strategiesMutex.RLock()
	strategy, exists := strategies[carrier]
	strategiesMutex.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Carrier strategy '%s' not registered", carrier)})
		return
	}

	tracking, err := strategy.GetTrackingStatus(trackingID)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(tracking)
}