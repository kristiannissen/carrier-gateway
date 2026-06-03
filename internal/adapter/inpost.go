package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// InPostAdapter handles communication with the InPost API.
type InPostAdapter struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewInPostAdapter creates a new InPostAdapter instance.
func NewInPostAdapter(apiKey string) *InPostAdapter {
	return &InPostAdapter{
		apiKey:     apiKey,
		baseURL:    "https://api.inpost.pl",
		httpClient: http.DefaultClient,
	}
}

// --- Unified Payload Structs (from logistics-gateway) ---

// UnifiedAddress represents a flat address structure for sender/receiver.
type UnifiedAddress struct {
	Name       string `json:"name"`
	Street     string `json:"street"`
	City       string `json:"city"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
	Phone      string `json:"phone,omitempty"`
	Email      string `json:"email,omitempty"`
}

// UnifiedDimensions represents the dimensions of a colli.
type UnifiedDimensions struct {
	Length float64 `json:"length"` // in cm
	Width  float64 `json:"width"`  // in cm
	Height float64 `json:"height"` // in cm
}

// UnifiedItem represents an item in a colli.
type UnifiedItem struct {
	Description string  `json:"description"`
	Weight      float64 `json:"weight"`
	Quantity    int     `json:"quantity"`
	Value       float64 `json:"value,omitempty"`
	SKU         string  `json:"sku,omitempty"`
}

// UnifiedColli represents a single package in a shipment.
type UnifiedColli struct {
	ID         string            `json:"id"`
	Reference  string            `json:"reference,omitempty"`
	Weight     float64           `json:"weight"`
	Dimensions UnifiedDimensions `json:"dimensions,omitempty"`
	Items      []UnifiedItem     `json:"items,omitempty"`
}

// UnifiedShipment represents the shipment details in the unified payload.
type UnifiedShipment struct {
	Sender      UnifiedAddress `json:"sender"`
	Receiver    UnifiedAddress `json:"receiver"`
	TotalWeight float64        `json:"totalWeight"`
	Colli       []UnifiedColli `json:"colli"`
}

// UnifiedBookingRequest represents the unified payload for booking a shipment.
type UnifiedBookingRequest struct {
	Carrier        string          `json:"carrier"`
	Shipment       UnifiedShipment `json:"shipment"`
	CallbackURL    string          `json:"callbackUrl,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
	Incoterms      string          `json:"incoterms,omitempty"`
	HsCode         string          `json:"hsCode,omitempty"`
}

// --- InPost-Specific Structs ---

// InPostAddress represents a physical address for sender or recipient.
type InPostAddress struct {
	StreetName  string `json:"streetName"`
	HouseNumber string `json:"houseNumber"`
	City        string `json:"city"`
	PostalCode  string `json:"postalCode"`
	Country     string `json:"country"`
}

// InPostContact represents contact information for sender or recipient.
type InPostContact struct {
	Phone string `json:"phone"`
	Email string `json:"email"`
}

// InPostDimensions represents the dimensions of a parcel.
type InPostDimensions struct {
	Length int `json:"length"` // in cm
	Width  int `json:"width"`  // in cm
	Height int `json:"height"` // in cm
}

// InPostParcel represents a single parcel in a shipment.
type InPostParcel struct {
	ID         string           `json:"id"`
	Weight     float64          `json:"weight"` // in kg
	Dimensions InPostDimensions `json:"dimensions"`
}

// InPostService represents the service details for a shipment.
type InPostService struct {
	ID           string `json:"id"`
	PickupDate   string `json:"pickupDate"`
	TargetLocker string `json:"targetLocker,omitempty"` // Optional
}

// InPostSender represents the sender of a shipment.
type InPostSender struct {
	Name    string        `json:"name"`
	Address InPostAddress `json:"address"`
	Contact InPostContact `json:"contact"`
}

// InPostRecipient represents the recipient of a shipment.
type InPostRecipient struct {
	Name    string        `json:"name"`
	Address InPostAddress `json:"address"`
	Contact InPostContact `json:"contact"`
}

// InPostBookingRequest represents the payload for booking a shipment with InPost.
type InPostBookingRequest struct {
	Shipment struct {
		Sender    InPostSender    `json:"sender"`
		Recipient InPostRecipient `json:"recipient"`
		Parcels   []InPostParcel  `json:"parcels"`
		Service   InPostService   `json:"service"`
		Reference string          `json:"reference"`
	} `json:"shipment"`
}

// InPostBookingResponse represents the response from InPost after booking a shipment.
type InPostBookingResponse struct {
	ShipmentID     string  `json:"shipmentId"`
	TrackingNumber string  `json:"trackingNumber"`
	LabelURL       string  `json:"labelUrl"`
	Status         string  `json:"status"`
	Cost           float64 `json:"cost"`
	Currency       string  `json:"currency"`
	LockerID       string  `json:"lockerId,omitempty"` // Optional
}

// --- Translation Function ---

// TranslateUnifiedToInPost converts the unified BookingRequest to InPost's format.
func (a *InPostAdapter) TranslateUnifiedToInPost(unifiedReq *UnifiedBookingRequest) *InPostBookingRequest {
	// Extract house number from street (if available)
	extractHouseNumber := func(street string) (streetName, houseNumber string) {
		// Simple heuristic: assume the last space-separated part is the house number
		// This can be improved based on actual data patterns
		parts := splitStreetAddress(street)
		if len(parts) > 1 {
			return parts[0], parts[1]
		}
		return street, ""
	}

	inpostReq := &InPostBookingRequest{}
	inpostReq.Shipment.Sender = InPostSender{
		Name: unifiedReq.Shipment.Sender.Name,
		Address: InPostAddress{
			StreetName:  extractHouseNumber(unifiedReq.Shipment.Sender.Street),
			HouseNumber: extractHouseNumber(unifiedReq.Shipment.Sender.Street),
			City:        unifiedReq.Shipment.Sender.City,
			PostalCode:  unifiedReq.Shipment.Sender.PostalCode,
			Country:     unifiedReq.Shipment.Sender.Country,
		},
		Contact: InPostContact{
			Phone: unifiedReq.Shipment.Sender.Phone,
			Email: unifiedReq.Shipment.Sender.Email,
		},
	}

	inpostReq.Shipment.Recipient = InPostRecipient{
		Name: unifiedReq.Shipment.Receiver.Name,
		Address: InPostAddress{
			StreetName:  extractHouseNumber(unifiedReq.Shipment.Receiver.Street),
			HouseNumber: extractHouseNumber(unifiedReq.Shipment.Receiver.Street),
			City:        unifiedReq.Shipment.Receiver.City,
			PostalCode:  unifiedReq.Shipment.Receiver.PostalCode,
			Country:     unifiedReq.Shipment.Receiver.Country,
		},
		Contact: InPostContact{
			Phone: unifiedReq.Shipment.Receiver.Phone,
			Email: unifiedReq.Shipment.Receiver.Email,
		},
	}

	// Convert colli to parcels
	for _, colli := range unifiedReq.Shipment.Colli {
		inpostParcel := InPostParcel{
			ID:     colli.ID,
			Weight: colli.Weight,
			Dimensions: InPostDimensions{
				Length: int(colli.Dimensions.Length),
				Width:  int(colli.Dimensions.Width),
				Height: int(colli.Dimensions.Height),
			},
		}
		inpostReq.Shipment.Parcels = append(inpostReq.Shipment.Parcels, inpostParcel)
	}

	// Set service details
	inpostReq.Shipment.Service = InPostService{
		ID:           "INPOST_STANDARD",               // Default service
		PickupDate:   time.Now().Format("2006-01-02"), // Default to today
		TargetLocker: "",                              // Optional: can be set based on additional logic
	}

	// Set reference (use first colli reference or generate one)
	if len(unifiedReq.Shipment.Colli) > 0 && unifiedReq.Shipment.Colli[0].Reference != "" {
		inpostReq.Shipment.Reference = unifiedReq.Shipment.Colli[0].Reference
	} else {
		inpostReq.Shipment.Reference = fmt.Sprintf("INPOST-%s-%d", unifiedReq.Carrier, time.Now().Unix())
	}

	return inpostReq
}

// splitStreetAddress splits a street address into street name and house number.
// This is a simple helper and may need refinement based on actual data.
func splitStreetAddress(street string) []string {
	// Example: "Main Street 123" -> ["Main Street", "123"]
	// This is a placeholder; consider using a more robust method if needed.
	var parts []string
	lastSpace := -1
	for i := len(street) - 1; i >= 0; i-- {
		if street[i] == ' ' {
			lastSpace = i
			break
		}
	}
	if lastSpace != -1 {
		parts = append(parts, street[:lastSpace])
		parts = append(parts, street[lastSpace+1:])
	} else {
		parts = append(parts, street)
	}
	return parts
}

// --- API Methods ---

// BookShipment sends a booking request to the InPost API and returns the response.
func (a *InPostAdapter) BookShipment(ctx context.Context, unifiedReq *UnifiedBookingRequest) (*InPostBookingResponse, error) {
	// Translate the unified request to InPost format
	inpostReq := a.TranslateUnifiedToInPost(unifiedReq)

	// Marshal the request payload
	payload, err := json.Marshal(inpostReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create a new HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/shipments", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

	// Send the request
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check for non-2xx status codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Unmarshal the response
	var bookingResp InPostBookingResponse
	if err := json.NewDecoder(resp.Body).Decode(&bookingResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &bookingResp, nil
}
