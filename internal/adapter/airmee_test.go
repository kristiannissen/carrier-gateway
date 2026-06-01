// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/airmee_test.go.
package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAirmeeAdapter_BookShipment(t *testing.T) {
	// Mock HTTP server for Airmee API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/deliveries", r.URL.Path)

		// Mock response for successful booking
		mockResponse := `{
			"id": "AIRMEE123456789",
			"trackingUrl": "https://tracking.airmee.com/AIRMEE123456789",
			"status": "created"
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize Airmee adapter with mock server URL
	adapter := &AirmeeAdapter{
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Test booking request
	request := BookingRequest{
		Carrier: "airmee",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "1234",
				Country:    "DK",
				Phone:      "+4512345678",
				Email:      "sender@example.com",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Copenhagen",
				PostalCode: "5678",
				Country:    "DK",
				Phone:      "+4587654321",
				Email:      "receiver@example.com",
			},
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 2.0,
					Dimensions: Dimensions{
						Length: 30.0,
						Width:  20.0,
						Height: 15.0,
					},
				},
			},
		},
	}

	// Call the BookShipment method
	response, err := adapter.BookShipment(request)

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "AIRMEE123456789", response.TrackingNumber)
	assert.Equal(t, "https://tracking.airmee.com/AIRMEE123456789", response.LabelURL)
}

func TestAirmeeAdapter_TrackShipment(t *testing.T) {
	// Mock HTTP server for Airmee tracking API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/deliveries/AIRMEE123456789", r.URL.Path)

		// Mock response for tracking
		mockResponse := `{
			"id": "AIRMEE123456789",
			"status": "in_transit",
			"events": [
				{
					"timestamp": "2026-05-31T12:00:00Z",
					"type": "pickup",
					"message": "Courier picked up the parcel",
					"location": {
						"latitude": 55.6761,
						"longitude": 12.5683,
						"address": "Copenhagen"
					}
				},
				{
					"timestamp": "2026-05-31T13:00:00Z",
					"type": "in_transit",
					"message": "Courier is on the way to delivery",
					"location": {
						"latitude": 55.6761,
						"longitude": 12.5683,
						"address": "Copenhagen"
					}
				}
			]
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize Airmee adapter with mock server URL
	adapter := &AirmeeAdapter{
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Call the TrackShipment method
	response, err := adapter.TrackShipment("AIRMEE123456789")

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "AIRMEE123456789", response.TrackingNumber)
	assert.Equal(t, "in_transit", response.Status)
	assert.Len(t, response.Events, 2)
}

func TestAirmeeAdapter_GetServicePoints(t *testing.T) {
	// Initialize Airmee adapter
	adapter := &AirmeeAdapter{
		APIKey:     "test-api-key",
		BaseURL:    "https://api.airmee.com/v1",
		HTTPClient: http.DefaultClient,
	}

	// Call the GetServicePoints method
	location := Location{
		City:    "Copenhagen",
		Country: "DK",
	}
	servicePoints, err := adapter.GetServicePoints(location)

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, servicePoints)
	assert.Len(t, servicePoints, 0) // Airmee does not have traditional service points
}
