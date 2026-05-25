package carriers

// internal/carriers/postnord.go

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CarrierProvider defines the contract for executing fulfillment actions across different transport providers.
type CarrierProvider interface {
	CreateBooking(request BookingRequest) (*BookingResponse, error)
}

// PostNordProvider implements the CarrierProvider interface for the PostNord logistics network.
type PostNordProvider struct {
	APIKey      string
	CustomerKey string
}

// postNordBookingPayload represents the JSON structure required by PostNord's Booking API.
type postNordBookingPayload struct {
	ServiceCode   string         `json:"serviceCode"`
	CustomerKey   string         `json:"customerKey"`
	Incoterm      string         `json:"incoterm"`
	DeliveryPoint string         `json:"deliveryPointId,omitempty"`
	Items         []postNordItem `json:"items"`
}

// postNordItem represents a physical package item in a PostNord booking request.
type postNordItem struct {
	Weight      float64 `json:"weight"`
	Description string  `json:"description"`
}

// CreateBooking executes either a mock response or a live API call to PostNord based on the provider configuration.
func (p *PostNordProvider) CreateBooking(request BookingRequest) (*BookingResponse, error) {
	// If the APIKey is empty or set to 'mock', return a hardcoded successful mock response.
	if p.APIKey == "" || p.APIKey == "mock" {
		return &BookingResponse{
			BookingID:  "MOCK-PN-" + fmt.Sprint(time.Now().Unix()),
			TrackingID: "SE-PN-MOCK-9999",
			Metadata:   map[string]string{"mode": "mock"},
		}, nil
	}

	// Determine the PostNord service code: 17 for Home Delivery, 19 for Service Point (MyPack Collect).
	serviceCode := "17"
	if request.DestinationType == "service_point" {
		serviceCode = "19"
	}

	// Map the internal BookingRequest to the PostNord-specific payload structure.
	payload := postNordBookingPayload{
		ServiceCode:   serviceCode,
		CustomerKey:   p.CustomerKey,
		Incoterm:      request.Incoterm,
		DeliveryPoint: request.ServicePointID,
		Items:         make([]postNordItem, len(request.Colli)),
	}

	// Iterate through the normalized colli items to populate the carrier's item list.
	for i, colli := range request.Colli {
		payload.Items[i] = postNordItem{
			Weight:      colli.Weight,
			Description: colli.Description,
		}
	}

	// Marshal the payload into JSON for the HTTP request body.
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PostNord payload: %w", err)
	}

	// Perform the live HTTP POST request to PostNord's endpoint.
	// The endpoint URL and API key header follow PostNord's technical specifications.
	apiURL := "https://api.postnord.com/rest/shipment/v3/bookings"
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("API-Key", p.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("postnord api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("postnord returned unexpected status: %d", resp.StatusCode)
	}

	// Decode the successful response from the carrier.
	var postNordResp struct {
		BookingReference  string `json:"bookingReference"`
		TrackingNumber    string `json:"trackingNumber"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&postNordResp); err != nil {
		return nil, fmt.Errorf("failed to decode postnord response: %w", err)
	}

	return &BookingResponse{
		BookingID:  postNordResp.BookingReference,
		TrackingID: postNordResp.TrackingNumber,
		Metadata:   map[string]string{"source": "postnord_live_api"},
	}, nil
}
