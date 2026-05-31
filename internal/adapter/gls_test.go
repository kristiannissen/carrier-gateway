// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/gls_test.go.
package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGLSAdapter_BookShipment(t *testing.T) {
	// Mock HTTP server for GLS API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/shipments", r.URL.Path)

		// Mock response for successful booking
		mockResponse := `{
			"trackingNumber": "GLS123456789DK",
			"labelUrl": "https://example.com/gls-label.png"
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize GLS adapter with mock server URL
	adapter := &GLSAdapter{
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Test booking request
	request := BookingRequest{
		Carrier: "gls",
		Shipment: Shipment{
			Sender: Address{
				Name:    "Sender Name",
				Street:  "Sender Street",
				City:    "Copenhagen",
				PostalCode: "1234",
				Country: "DK",
				Phone:  "+4512345678",
			},
			Receiver: Address{
				Name:    "Receiver Name",
				Street:  "Receiver Street",
				City:    "Aarhus",
				PostalCode: "5678",
				Country: "DK",
				Phone:  "+4587654321",
			},
			Colli: []Colli{
				{
					ID:   "colli-1",
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
	assert.Equal(t, "GLS123456789DK", response.TrackingNumber)
	assert.Equal(t, "https://example.com/gls-label.png", response.LabelURL)
}

func TestGLSAdapter_TrackShipment(t *testing.T) {
	// Mock HTTP server for GLS tracking API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/tracking/GLS123456789DK", r.URL.Path)

		// Mock response for tracking
		mockResponse := `{
			"trackingNumber": "GLS123456789DK",
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

	// Initialize GLS adapter with mock server URL
	adapter := &GLSAdapter{
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Call the TrackShipment method
	response, err := adapter.TrackShipment("GLS123456789DK")

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "GLS123456789DK", response.TrackingNumber)
	assert.Equal(t, "In Transit", response.Status)
}

func TestGLSAdapter_GetServicePoints(t *testing.T) {
	// Mock HTTP server for GLS service points API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.String(), "/parcelshops?city=Copenhagen&country=DK")

		// Mock response for service points
		mockResponse := `[{
			"id": "GLS001",
			"name": "GLS ParcelShop 1",
			"address": {
				"street": "ParcelShop Street 1",
				"postalCode": "1234",
				"city": "Copenhagen",
				"country": "DK"
			}
		}]`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize GLS adapter with mock server URL
	adapter := &GLSAdapter{
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
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
	assert.Len(t, servicePoints, 1)
	assert.Equal(t, "GLS001", servicePoints[0].ID)
	assert.Equal(t, "GLS ParcelShop 1", servicePoints[0].Name)
}
