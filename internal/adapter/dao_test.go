// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/dao_test.go.
package adapter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDAOAdapter_BookShipment(t *testing.T) {
	// Mock HTTP server for DAO API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.String(), "/DAODirekte/leveringsordre.php")

		// Mock response for successful booking
		mockResponse := `{
			"status": "OK",
			"fejlkode": "",
			"fejltekst": "",
			"resultat": {
				"stregkode": "DAO123456789DK",
				"labelTekst1": "76201",
				"labelTekst2": "5432345",
				"labelTekst3": "5431",
				"udsorting": "E",
				"ETA": "2026-06-01 05:40"
			}
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize DAO adapter with mock server URL
	adapter := &DAOAdapter{
		CustomerID: "test-customer-id",
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Test booking request
	request := BookingRequest{
		Carrier: "dao",
		Shipment: Shipment{
			Sender: Address{
				Name:       "Sender Name",
				Street:     "Sender Street",
				City:       "Copenhagen",
				PostalCode: "1234",
				Country:    "DK",
				Phone:      "+4512345678",
			},
			Receiver: Address{
				Name:       "Receiver Name",
				Street:     "Receiver Street",
				City:       "Aarhus",
				PostalCode: "5678",
				Country:    "DK",
				Phone:      "+4587654321",
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
	assert.Equal(t, "DAO123456789DK", response.TrackingNumber)
}

func TestDAOAdapter_TrackShipment(t *testing.T) {
	// Mock HTTP server for DAO tracking API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.String(), "/TrackNTrace_v2.php")

		// Mock response for tracking
		mockResponse := `{
			"status": "OK",
			"fejlkode": "",
			"fejltekst": "",
			"resultat": {
				"stregkode": "DAO123456789DK",
				"pakketype": "Home",
				"eta": "2026-06-01",
				"modtager": {
					"navn": "Receiver Name",
					"adresse": "Receiver Street",
					"post": "5678 Aarhus",
					"land": "Danmark"
				},
				"haendelser": [
					{
						"tidspunkt": "2026-05-31T12:00:00Z",
						"haendelse": "10",
						"beskrivelse": "Pakke modtaget på fordelingscenter",
						"pakketype": "Home",
						"sted": "DAO Erritsø"
					}
				]
			}
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize DAO adapter with mock server URL
	adapter := &DAOAdapter{
		CustomerID: "test-customer-id",
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Call the TrackShipment method
	response, err := adapter.TrackShipment("DAO123456789DK")

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "DAO123456789DK", response.TrackingNumber)
	assert.Equal(t, "Home", response.Status)
	assert.Len(t, response.Events, 1)
}

func TestDAOAdapter_GetServicePoints(t *testing.T) {
	// Mock HTTP server for DAO service points API
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.String(), "/DAOPakkeshop/FindPakkeshop.php")

		// Mock response for service points
		mockResponse := `{
			"status": "OK",
			"fejlkode": "",
			"fejltekst": "",
			"resultat": {
				"pakkeshops": [
					{
						"shopId": "DAO001",
						"type": "Pakkeshop",
						"navn": "DAO Pakkeshop 1",
						"adresse": "Mock Street 1",
						"postnr": "1234",
						"bynavn": "Copenhagen",
						"udsorting": "E",
						"latitude": "55.720706",
						"longitude": "9.559928",
						"aabningstider": {
							"man": "08:00 - 22:00",
							"tir": "08:00 - 22:00",
							"ons": "08:00 - 22:00",
							"tor": "08:00 - 22:00",
							"fre": "08:00 - 22:00",
							"lor": "08:00 - 22:00",
							"son": "08:00 - 22:00"
						}
					}
				],
				"udgangspunkt": {
					"latitude": "55.730031160000003",
					"longitude": "9.5619661499999999",
					"position_from_postal": false
				},
				"antal": "1"
			}
		}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer mockServer.Close()

	// Initialize DAO adapter with mock server URL
	adapter := &DAOAdapter{
		CustomerID: "test-customer-id",
		APIKey:     "test-api-key",
		BaseURL:    mockServer.URL,
		HTTPClient: mockServer.Client(),
	}

	// Call the GetServicePoints method
	location := Location{
		City:       "Copenhagen",
		Country:    "DK",
		PostalCode: "1234",
	}
	servicePoints, err := adapter.GetServicePoints(location)

	// Verify the response
	assert.NoError(t, err)
	assert.NotNil(t, servicePoints)
	assert.Len(t, servicePoints, 1)
	assert.Equal(t, "DAO001", servicePoints[0].ID)
	assert.Equal(t, "DAO Pakkeshop 1", servicePoints[0].Name)
}
