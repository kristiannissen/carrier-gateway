// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/bring_test.go.
package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBringAdapter_BookShipment(t *testing.T) {
	// Mock HTTP server for Bring API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/shipping/shipment", r.URL.Path)

		// Mock response for successful booking
		mockResponse := `{
			"consignmentNumber": "BR123456789NO",
			"labelUrl": "https://example.com/bring-label.png"
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize Bring adapter with mock server URL
	adapter := &BringAdapter{
		apiKey:     "test-api-key",
		customerID: "test-customer-id",
		baseURL:    mockServer.URL,
		httpClient: mockServer.Client(),
	}

	// Test booking request
	request := BookingRequest{
		Carrier: "bring",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Oslo",
				PostalCode: "0123",
				Country:    "NO",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Bergen",
				PostalCode: "5678",
				Country:    "NO",
			},
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 5.0,
					Dimensions: Dimensions{
						Length: 20.0,
						Width:  15.0,
						Height: 10.0,
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
	assert.Equal(t, "BR123456789NO", response.TrackingNumber)
	assert.Equal(t, "https://example.com/bring-label.png", response.LabelURL)
}

func TestBringAdapter_TrackShipment(t *testing.T) {
	// Mock HTTP server for Bring tracking API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/tracking/consignments/BR123456789NO", r.URL.Path)

		// Mock response for tracking
		mockResponse := `{
			"consignmentNumber": "BR123456789NO",
			"status": "In Transit",
			"events": [
				{
					"timestamp": "2026-05-31T12:00:00Z",
					"status": "Shipment Accepted",
					"location": "Oslo"
				}
			]
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize Bring adapter with mock server URL
	adapter := &BringAdapter{
		apiKey:     "test-api-key",
		customerID: "test-customer-id",
		baseURL:    mockServer.URL,
		httpClient: mockServer.Client(),
	}

	// Call the TrackShipment method
	response, err := adapter.TrackShipment("BR123456789NO")

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "BR123456789NO", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
}
