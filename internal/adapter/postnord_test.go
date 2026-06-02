// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/postnord_test.go.
package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPostNordAdapter_BookShipment(t *testing.T) {
	// Mock HTTP server for PostNord API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/rest/shipment/v1/booking", r.URL.Path)

		// Mock response for successful booking
		mockResponse := `{
			"trackingNumber": "PN123456789DK",
			"labelURL": "https://example.com/label.png"
		}`
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize PostNord adapter with mock server URL
	adapter := &PostNordAdapter{
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Test booking request
	request := BookingRequest{
		Carrier: "postnord",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Sender City",
				PostalCode: "12345",
				Country:    "DK",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Receiver City",
				PostalCode: "67890",
				Country:    "DK",
			},
			Colli: []Colli{
				{
					ID:     "colli-1",
					Weight: 10.0,
					Dimensions: Dimensions{
						Length: 10.0,
						Width:  10.0,
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
	assert.Equal(t, "PN123456789DK", response.TrackingNumber)
	assert.Equal(t, "https://example.com/label.png", response.LabelURL)
}

func TestPostNordAdapter_TrackShipment(t *testing.T) {
	// Mock HTTP server for PostNord tracking API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/rest/shipment/v1/tracking/PN123456789DK", r.URL.Path)

		// Mock response for tracking
		mockResponse := `{
			"trackingNumber": "PN123456789DK",
			"status": "In Transit",
			"events": [
				{
					"timestamp": "2026-05-31T12:00:00Z",
					"status": "Shipment Accepted",
					"location": "Copenhagen"
				}
			]
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize PostNord adapter with mock server URL
	adapter := &PostNordAdapter{
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Call the TrackShipment method
	response, err := adapter.TrackShipment("PN123456789DK")

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "PN123456789DK", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
}
