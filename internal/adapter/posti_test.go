// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/posti_test.go.
package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPostiAdapter_BookShipment(t *testing.T) {
	// Mock HTTP server for Posti API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/shipment/v1/shipments", r.URL.Path)

		// Mock response for successful booking
		mockResponse := `{
			"shipmentId": "SHIP123456789",
			"trackingCode": "POSTI123456789FI",
			"labelUrl": "https://example.com/posti-label.png",
			"status": "OK"
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize Posti adapter with mock server URL
	adapter := &PostiAdapter{
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Test booking request
	request := BookingRequest{
		Carrier: "posti",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Helsinki",
				PostalCode: "00100",
				Country:    "FI",
				Phone:      "+35812345678",
				Email:      "sender@example.com",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Tampere",
				PostalCode: "33100",
				Country:    "FI",
				Phone:      "+35887654321",
				Email:      "receiver@example.com",
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
	assert.Equal(t, "POSTI123456789FI", response.TrackingNumber)
	assert.Equal(t, "https://example.com/posti-label.png", response.LabelURL)
}

func TestPostiAdapter_TrackShipment(t *testing.T) {
	// Mock HTTP server for Posti tracking API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/tracking/v1/shipments/POSTI123456789FI", r.URL.Path)

		// Mock response for tracking
		mockResponse := `{
			"shipmentId": "SHIP123456789",
			"trackingCode": "POSTI123456789FI",
			"status": "In Transit",
			"events": [
				{
					"timestamp": "2026-05-31T12:00:00Z",
					"eventCode": "DEP",
					"eventName": "Shipment Accepted",
					"location": "Helsinki",
					"country": "FI"
				}
			]
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize Posti adapter with mock server URL
	adapter := &PostiAdapter{
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Call the TrackShipment method
	response, err := adapter.TrackShipment("POSTI123456789FI")

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "POSTI123456789FI", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
	assert.Len(t, response.Events, 1)
}

func TestPostiAdapter_GetServicePoints(t *testing.T) {
	// Mock HTTP server for Posti service points API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.String(), "/pickup-points/v1/nearest")

		// Mock response for service points
		mockResponse := `{
			"pickupPoints": [
				{
					"id": "POSTI001",
					"name": "Posti Pickup Point 1",
					"address": {
						"street": "Mock Street 1",
						"postalCode": "00100",
						"city": "Helsinki",
						"country": "FI"
					},
					"coordinates": {
						"latitude": 60.1699,
						"longitude": 24.9384
					},
					"openingHours": [
						{
							"day": "Monday",
							"openTime": "08:00",
							"closeTime": "20:00"
						}
					],
					"distance": 1.5
				}
			]
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize Posti adapter with mock server URL
	adapter := &PostiAdapter{
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Call the GetServicePoints method
	location := Location{
		City:       "Helsinki",
		Country:    "FI",
		PostalCode: "00100",
	}
	servicePoints, err := adapter.GetServicePoints(location)

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, servicePoints)
	assert.Len(t, servicePoints, 1)
	assert.Equal(t, "POSTI001", servicePoints[0].ID)
	assert.Equal(t, "Posti Pickup Point 1", servicePoints[0].Name)
}
