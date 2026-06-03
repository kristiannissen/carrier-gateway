package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test Cases for Translation ---

func TestTranslateUnifiedToInPost(t *testing.T) {
	t.Parallel()

	adapter := NewInPostAdapter("test-api-key")

	// Unified payload from logistics-gateway
	unifiedReq := &UnifiedBookingRequest{
		Carrier: "inpost",
		Shipment: UnifiedShipment{
			Sender: UnifiedAddress{
				Name:       "Sender Shop",
				Street:     "Sender Street 1",
				City:       "Warsaw",
				PostalCode: "00-001",
				Country:    "PL",
				Phone:      "+4812345678",
				Email:      "sender@example.com",
			},
			Receiver: UnifiedAddress{
				Name:       "John Kowalski",
				Street:     "Receiver Avenue 2",
				City:       "Krakow",
				PostalCode: "30-001",
				Country:    "PL",
				Phone:      "+48987654321",
				Email:      "john.kowalski@example.com",
			},
			TotalWeight: 2.0,
			Colli: []UnifiedColli{
				{
					ID:     "1",
					Weight: 2.0,
					Dimensions: UnifiedDimensions{
						Length: 30.0,
						Width:  20.0,
						Height: 10.0,
					},
					Reference: "INPOST-ORDER-12345",
				},
			},
		},
		CallbackURL:    "https://api.example.com/webhooks/tracking",
		IdempotencyKey: "unique-key-123",
		Incoterms:      "DDP",
		HsCode:         "61091000",
	}

	// Translate to InPost format
	inpostReq := adapter.TranslateUnifiedToInPost(unifiedReq)

	// Assert sender
	assert.Equal(t, "Sender Shop", inpostReq.Shipment.Sender.Name)
	assert.Equal(t, "Sender Street", inpostReq.Shipment.Sender.Address.StreetName)
	assert.Equal(t, "1", inpostReq.Shipment.Sender.Address.HouseNumber)
	assert.Equal(t, "Warsaw", inpostReq.Shipment.Sender.Address.City)
	assert.Equal(t, "00-001", inpostReq.Shipment.Sender.Address.PostalCode)
	assert.Equal(t, "PL", inpostReq.Shipment.Sender.Address.Country)
	assert.Equal(t, "+4812345678", inpostReq.Shipment.Sender.Contact.Phone)
	assert.Equal(t, "sender@example.com", inpostReq.Shipment.Sender.Contact.Email)

	// Assert recipient
	assert.Equal(t, "John Kowalski", inpostReq.Shipment.Recipient.Name)
	assert.Equal(t, "Receiver Avenue", inpostReq.Shipment.Recipient.Address.StreetName)
	assert.Equal(t, "2", inpostReq.Shipment.Recipient.Address.HouseNumber)
	assert.Equal(t, "Krakow", inpostReq.Shipment.Recipient.Address.City)
	assert.Equal(t, "30-001", inpostReq.Shipment.Recipient.Address.PostalCode)
	assert.Equal(t, "PL", inpostReq.Shipment.Recipient.Address.Country)
	assert.Equal(t, "+48987654321", inpostReq.Shipment.Recipient.Contact.Phone)
	assert.Equal(t, "john.kowalski@example.com", inpostReq.Shipment.Recipient.Contact.Email)

	// Assert parcels
	require.Len(t, inpostReq.Shipment.Parcels, 1)
	assert.Equal(t, "1", inpostReq.Shipment.Parcels[0].ID)
	assert.Equal(t, 2.0, inpostReq.Shipment.Parcels[0].Weight)
	assert.Equal(t, 30, inpostReq.Shipment.Parcels[0].Dimensions.Length)
	assert.Equal(t, 20, inpostReq.Shipment.Parcels[0].Dimensions.Width)
	assert.Equal(t, 10, inpostReq.Shipment.Parcels[0].Dimensions.Height)

	// Assert service
	assert.Equal(t, "INPOST_STANDARD", inpostReq.Shipment.Service.ID)
	assert.Equal(t, time.Now().Format("2006-01-02"), inpostReq.Shipment.Service.PickupDate)

	// Assert reference
	assert.Equal(t, "INPOST-ORDER-12345", inpostReq.Shipment.Reference)
}

func TestTranslateUnifiedToInPost_EmptyHouseNumber(t *testing.T) {
	t.Parallel()

	adapter := NewInPostAdapter("test-api-key")

	// Unified payload with street without house number
	unifiedReq := &UnifiedBookingRequest{
		Carrier: "inpost",
		Shipment: UnifiedShipment{
			Sender: UnifiedAddress{
				Name:       "Sender Shop",
				Street:     "Sender Street", // No house number
				City:       "Warsaw",
				PostalCode: "00-001",
				Country:    "PL",
			},
			Receiver: UnifiedAddress{
				Name:       "John Kowalski",
				Street:     "Receiver Avenue", // No house number
				City:       "Krakow",
				PostalCode: "30-001",
				Country:    "PL",
			},
			TotalWeight: 2.0,
			Colli: []UnifiedColli{
				{
					ID:     "1",
					Weight: 2.0,
				},
			},
		},
	}

	// Translate to InPost format
	inpostReq := adapter.TranslateUnifiedToInPost(unifiedReq)

	// Assert sender address (house number should be empty)
	assert.Equal(t, "Sender Street", inpostReq.Shipment.Sender.Address.StreetName)
	assert.Equal(t, "", inpostReq.Shipment.Sender.Address.HouseNumber)

	// Assert recipient address (house number should be empty)
	assert.Equal(t, "Receiver Avenue", inpostReq.Shipment.Recipient.Address.StreetName)
	assert.Equal(t, "", inpostReq.Shipment.Recipient.Address.HouseNumber)
}

func TestTranslateUnifiedToInPost_GeneratedReference(t *testing.T) {
	t.Parallel()

	adapter := NewInPostAdapter("test-api-key")

	// Unified payload without reference
	unifiedReq := &UnifiedBookingRequest{
		Carrier: "inpost",
		Shipment: UnifiedShipment{
			Sender: UnifiedAddress{
				Name:    "Sender Shop",
				Street:  "Sender Street 1",
				City:    "Warsaw",
				Country: "PL",
			},
			Receiver: UnifiedAddress{
				Name:    "John Kowalski",
				Street:  "Receiver Avenue 2",
				City:    "Krakow",
				Country: "PL",
			},
			Colli: []UnifiedColli{
				{
					ID:     "1",
					Weight: 2.0,
					// No reference
				},
			},
		},
	}

	// Translate to InPost format
	inpostReq := adapter.TranslateUnifiedToInPost(unifiedReq)

	// Assert that a reference was generated
	assert.NotEmpty(t, inpostReq.Shipment.Reference)
	assert.Contains(t, inpostReq.Shipment.Reference, "INPOST-inpost-")
}

// --- Test Cases for BookShipment ---

func TestBookShipment_Success(t *testing.T) {
	t.Parallel()

	// Mock InPost API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/shipments", r.URL.Path)

		// Verify headers
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Decode the request body
		var reqBody InPostBookingRequest
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		// Verify the request payload matches the provided example
		assert.Equal(t, "Sender Shop", reqBody.Shipment.Sender.Name)
		assert.Equal(t, "Sender Street", reqBody.Shipment.Sender.Address.StreetName)
		assert.Equal(t, "1", reqBody.Shipment.Sender.Address.HouseNumber)
		assert.Equal(t, "Warsaw", reqBody.Shipment.Sender.Address.City)
		assert.Equal(t, "00-001", reqBody.Shipment.Sender.Address.PostalCode)
		assert.Equal(t, "PL", reqBody.Shipment.Sender.Address.Country)
		assert.Equal(t, "+4812345678", reqBody.Shipment.Sender.Contact.Phone)
		assert.Equal(t, "sender@example.com", reqBody.Shipment.Sender.Contact.Email)

		assert.Equal(t, "John Kowalski", reqBody.Shipment.Recipient.Name)
		assert.Equal(t, "Receiver Avenue", reqBody.Shipment.Recipient.Address.StreetName)
		assert.Equal(t, "2", reqBody.Shipment.Recipient.Address.HouseNumber)
		assert.Equal(t, "Krakow", reqBody.Shipment.Recipient.Address.City)
		assert.Equal(t, "30-001", reqBody.Shipment.Recipient.Address.PostalCode)
		assert.Equal(t, "PL", reqBody.Shipment.Recipient.Address.Country)
		assert.Equal(t, "+48987654321", reqBody.Shipment.Recipient.Contact.Phone)
		assert.Equal(t, "john.kowalski@example.com", reqBody.Shipment.Recipient.Contact.Email)

		require.Len(t, reqBody.Shipment.Parcels, 1)
		assert.Equal(t, "1", reqBody.Shipment.Parcels[0].ID)
		assert.Equal(t, 2.0, reqBody.Shipment.Parcels[0].Weight)
		assert.Equal(t, 30, reqBody.Shipment.Parcels[0].Dimensions.Length)
		assert.Equal(t, 20, reqBody.Shipment.Parcels[0].Dimensions.Width)
		assert.Equal(t, 10, reqBody.Shipment.Parcels[0].Dimensions.Height)

		assert.Equal(t, "INPOST_STANDARD", reqBody.Shipment.Service.ID)
		assert.Equal(t, "INPOST-ORDER-12345", reqBody.Shipment.Reference)

		// Send mock response
		mockResponse := InPostBookingResponse{
			ShipmentID:     "INPOST-550e8400-e29b-41d4-a716-446655440007",
			TrackingNumber: "INPOST123456789PL",
			LabelURL:       "https://api.inpost.pl/labels/550e8400-e29b-41d4-a716-446655440007.pdf",
			Status:         "booked",
			Cost:           8.00,
			Currency:       "PLN",
			LockerID:       "WAR001",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	// Create adapter with mock server URL
	adapter := NewInPostAdapter("test-api-key")
	adapter.baseURL = server.URL

	// Unified payload
	unifiedReq := &UnifiedBookingRequest{
		Carrier: "inpost",
		Shipment: UnifiedShipment{
			Sender: UnifiedAddress{
				Name:       "Sender Shop",
				Street:     "Sender Street 1",
				City:       "Warsaw",
				PostalCode: "00-001",
				Country:    "PL",
				Phone:      "+4812345678",
				Email:      "sender@example.com",
			},
			Receiver: UnifiedAddress{
				Name:       "John Kowalski",
				Street:     "Receiver Avenue 2",
				City:       "Krakow",
				PostalCode: "30-001",
				Country:    "PL",
				Phone:      "+48987654321",
				Email:      "john.kowalski@example.com",
			},
			Colli: []UnifiedColli{
				{
					ID:         "1",
					Weight:     2.0,
					Reference:  "INPOST-ORDER-12345",
					Dimensions: UnifiedDimensions{Length: 30.0, Width: 20.0, Height: 10.0},
				},
			},
		},
	}

	// Call BookShipment
	resp, err := adapter.BookShipment(context.Background(), unifiedReq)
	require.NoError(t, err)

	// Assert response
	assert.Equal(t, "INPOST-550e8400-e29b-41d4-a716-446655440007", resp.ShipmentID)
	assert.Equal(t, "INPOST123456789PL", resp.TrackingNumber)
	assert.Equal(t, "https://api.inpost.pl/labels/550e8400-e29b-41d4-a716-446655440007.pdf", resp.LabelURL)
	assert.Equal(t, "booked", resp.Status)
	assert.Equal(t, 8.00, resp.Cost)
	assert.Equal(t, "PLN", resp.Currency)
	assert.Equal(t, "WAR001", resp.LockerID)
}

func TestBookShipment_APIError(t *testing.T) {
	t.Parallel()

	// Mock InPost API server with error response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Invalid API key"}`))
	}))
	defer server.Close()

	// Create adapter with mock server URL
	adapter := NewInPostAdapter("invalid-api-key")
	adapter.baseURL = server.URL

	// Unified payload
	unifiedReq := &UnifiedBookingRequest{
		Carrier: "inpost",
		Shipment: UnifiedShipment{
			Sender: UnifiedAddress{
				Name:    "Sender Shop",
				Street:  "Sender Street 1",
				City:    "Warsaw",
				Country: "PL",
			},
			Receiver: UnifiedAddress{
				Name:    "John Kowalski",
				Street:  "Receiver Avenue 2",
				City:    "Krakow",
				Country: "PL",
			},
			Colli: []UnifiedColli{
				{ID: "1", Weight: 2.0},
			},
		},
	}

	// Call BookShipment and expect error
	_, err := adapter.BookShipment(context.Background(), unifiedReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API request failed with status 400")
}

func TestBookShipment_MalformedResponse(t *testing.T) {
	t.Parallel()

	// Mock InPost API server with malformed JSON response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{invalid json}`))
	}))
	defer server.Close()

	// Create adapter with mock server URL
	adapter := NewInPostAdapter("test-api-key")
	adapter.baseURL = server.URL

	// Unified payload
	unifiedReq := &UnifiedBookingRequest{
		Carrier: "inpost",
		Shipment: UnifiedShipment{
			Sender: UnifiedAddress{
				Name:    "Sender Shop",
				Street:  "Sender Street 1",
				City:    "Warsaw",
				Country: "PL",
			},
			Receiver: UnifiedAddress{
				Name:    "John Kowalski",
				Street:  "Receiver Avenue 2",
				City:    "Krakow",
				Country: "PL",
			},
			Colli: []UnifiedColli{
				{ID: "1", Weight: 2.0},
			},
		},
	}

	// Call BookShipment and expect error
	_, err := adapter.BookShipment(context.Background(), unifiedReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode response")
}

// --- Test Cases for splitStreetAddress ---

func TestSplitStreetAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Street with house number",
			input:    "Main Street 123",
			expected: []string{"Main Street", "123"},
		},
		{
			name:     "Street without house number",
			input:    "Main Street",
			expected: []string{"Main Street"},
		},
		{
			name:     "Street with multiple spaces",
			input:    "Main Street Apt 123",
			expected: []string{"Main Street Apt", "123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitStreetAddress(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
