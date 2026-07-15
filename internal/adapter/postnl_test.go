// Package adapter provides tests for the PostNL carrier adapter.
// This file is located at /internal/adapter/postnl_test.go.
package adapter

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// =========================================================================
// Test helpers
// =========================================================================

func postnlTestSender() Address {
	return Address{
		Name:        "Unisport Warehouse",
		Street:      "Industrieweg",
		HouseNumber: "12",
		City:        "Amsterdam",
		PostalCode:  "1000AA",
		Country:     "NL",
		Phone:       "+31612345678",
		Email:       "warehouse@unisport.nl",
	}
}

func postnlTestReceiver() Address {
	return Address{
		Name:        "Jan de Vries",
		Street:      "Waldorpstraat",
		HouseNumber: "3",
		City:        "Den Haag",
		PostalCode:  "2521CA",
		Country:     "NL",
		Phone:       "+31687654321",
		Email:       "jan@example.nl",
	}
}

func postnlTestColli(id string, weight float64) Colli {
	return Colli{
		ID:         id,
		Weight:     weight,
		Dimensions: Dimensions{Length: 30, Width: 20, Height: 10},
		Items:      []Item{{Description: "Sports shoes", Weight: weight, Quantity: 1}},
	}
}

func postnlTestBookingRequest() BookingRequest {
	return BookingRequest{
		Carrier: "postnl",
		Shipment: Shipment{
			Sender:      postnlTestSender(),
			Receiver:    postnlTestReceiver(),
			TotalWeight: 2.5,
			Colli:       []Colli{postnlTestColli("box-1", 2.5)},
		},
	}
}

// postnlOKResponse returns a minimal EmaOkResponse JSON payload with the given barcode.
func postnlOKResponse(barcode string) postnlEmaOkResponse {
	return postnlEmaOkResponse{
		Items: []postnlEmaItem{
			{
				Barcode: barcode,
				Labels: []postnlEmaLabel{
					{Label: "bW9jay1sYWJlbA==", OutputType: "PDF", LabelType: "Label"},
				},
			},
		},
	}
}

// postnlMockServer creates a httptest.Server that matches method+path and
// responds with the provided body and status code.
func postnlMockServer(t *testing.T, method, path string, statusCode int, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, method, r.Method)
		assert.Equal(t, path, r.URL.Path)
		assert.Equal(t, "test-api-key", r.Header.Get("apikey"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
}

// =========================================================================
// Mock adapter tests
// =========================================================================

func TestMockPostNLAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	t.Run("missing TotalWeight", func(t *testing.T) {
		t.Parallel()
		_, err := (&MockPostNLAdapter{}).BookShipment(t.Context(), BookingRequest{
			Carrier: "postnl",
			Shipment: Shipment{
				Sender:   postnlTestSender(),
				Receiver: postnlTestReceiver(),
				Colli:    []Colli{postnlTestColli("box-1", 2.5)},
			},
		})
		assert.Error(t, err)
	})

	t.Run("valid request", func(t *testing.T) {
		t.Parallel()
		resp, err := (&MockPostNLAdapter{}).BookShipment(t.Context(), postnlTestBookingRequest())
		require.NoError(t, err)
		assert.Equal(t, "postnl", resp.Carrier)
		assert.NotEmpty(t, resp.TrackingNumber)
		assert.Equal(t, "booked", resp.Status)
	})

	t.Run("func override", func(t *testing.T) {
		t.Parallel()
		m := &MockPostNLAdapter{
			BookShipmentFunc: func(_ BookingRequest) (*BookingResponse, error) {
				return nil, errors.New("injected error")
			},
		}
		_, err := m.BookShipment(t.Context(), postnlTestBookingRequest())
		assert.EqualError(t, err, "injected error")
	})
}

func TestMockPostNLAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	resp, err := (&MockPostNLAdapter{}).TrackShipment(t.Context(), "3STEST001")
	require.NoError(t, err)
	assert.Equal(t, "3STEST001", resp.TrackingNumber)
	assert.Equal(t, "postnl", resp.Carrier)
	assert.Len(t, resp.Events, 3)
	assert.Equal(t, StatusOutForDelivery, resp.NormalizedStatus)
}

func TestMockPostNLAdapter_FetchLabel(t *testing.T) {
	t.Parallel()

	resp, err := (&MockPostNLAdapter{}).FetchLabel(t.Context(), LabelRequest{
		TrackingNumber: "3STEST001",
		Format:         LabelFormatPDF,
	})
	require.NoError(t, err)
	assert.Equal(t, "3STEST001", resp.TrackingNumber)
	assert.Equal(t, LabelFormatPDF, resp.Format)
	assert.NotEmpty(t, resp.Data)
}

func TestMockPostNLAdapter_CancelShipment(t *testing.T) {
	t.Parallel()

	_, err := (&MockPostNLAdapter{}).CancelShipment(t.Context(), "3STEST001")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestMockPostNLAdapter_UpdateShipment(t *testing.T) {
	t.Parallel()

	_, err := (&MockPostNLAdapter{}).UpdateShipment(t.Context(), UpdateRequest{
		Carrier:        "postnl",
		TrackingNumber: "3STEST001",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestMockPostNLAdapter_BookReturn(t *testing.T) {
	t.Parallel()

	resp, err := (&MockPostNLAdapter{}).BookReturn(t.Context(), ReturnRequest{
		Carrier:  "postnl",
		Sender:   postnlTestReceiver(), // consumer is the sender in a return
		Receiver: postnlTestSender(),
	})
	require.NoError(t, err)
	assert.Equal(t, "postnl", resp.Carrier)
	assert.NotEmpty(t, resp.TrackingNumber)
	assert.Equal(t, "booked", resp.Status)
}

// =========================================================================
// Real adapter tests (httptest server)
// =========================================================================

func newTestPostNLAdapter(t *testing.T, baseURL string) *PostNLAdapter {
	t.Helper()
	a := NewPostNLAdapter("test-api-key", "123456", "ABCD", zap.NewNop())
	a.BaseURL = baseURL
	return a
}

func TestPostNLAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	t.Run("successful booking", func(t *testing.T) {
		t.Parallel()
		srv := postnlMockServer(t, http.MethodPost, "/shipment/delivery/v4/labelconfirm",
			http.StatusOK, postnlOKResponse("3SDEVC123456789"))
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		resp, err := a.BookShipment(t.Context(), postnlTestBookingRequest())
		require.NoError(t, err)
		assert.Equal(t, "3SDEVC123456789", resp.TrackingNumber)
		assert.Equal(t, "postnl", resp.Carrier)
		assert.Equal(t, "booked", resp.Status)
		assert.NotEmpty(t, resp.LabelURL)
	})

	t.Run("letterbox shipment uses single item", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload postnlBookRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			assert.Equal(t, "letterbox", payload.ShipmentType)
			assert.Equal(t, 1, payload.ItemCount)

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(postnlOKResponse("3SLETTERBOX001"))
		}))
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		req := postnlTestBookingRequest()
		req.Shipment.DeliveryType = "letterbox"
		// Supply multiple colli — adapter should trim to one for letterbox.
		req.Shipment.Colli = []Colli{
			postnlTestColli("box-1", 1.0),
			postnlTestColli("box-2", 1.5),
		}
		req.Shipment.TotalWeight = 2.5
		resp, err := a.BookShipment(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "3SLETTERBOX001", resp.TrackingNumber)
	})

	t.Run("EU international shipment includes bundle", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload postnlBookRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			require.NotNil(t, payload.InternationalShipmentData)
			assert.Equal(t, "track_trace", payload.InternationalShipmentData.Bundle)

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(postnlOKResponse("3SINTL001"))
		}))
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		req := postnlTestBookingRequest()
		req.Shipment.Receiver.Country = "DE"
		resp, err := a.BookShipment(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "3SINTL001", resp.TrackingNumber)
	})

	t.Run("upstream error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":["17701: customerNumber missing"]}`))
		}))
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		_, err := a.BookShipment(t.Context(), postnlTestBookingRequest())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "postnl")
	})

	t.Run("empty items in response", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(postnlEmaOkResponse{})
		}))
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		_, err := a.BookShipment(t.Context(), postnlTestBookingRequest())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty items")
	})
}

func TestPostNLAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	t.Run("successful tracking", func(t *testing.T) {
		t.Parallel()
		tracking := postnlTrackingResponse{
			CurrentStatus: &postnlCurrentStatus{
				Shipment: &postnlTrackedShipment{
					Barcode: "3SDEVC001",
					Status: postnlStatus{
						TimeStamp:         "08-11-2022 10:13:20",
						StatusCode:        "7",
						StatusDescription: "Shipment out for delivery",
						PhaseCode:         "3",
						PhaseDescription:  "Distribution",
					},
					Event: []postnlEvent{
						{
							Code:         "J05",
							Description:  "Driver is en route",
							LocationCode: "171966",
							TimeStamp:    "08-11-2022 10:13:20",
						},
					},
				},
			},
		}
		srv := postnlMockServer(t, http.MethodGet, "/shipment/v2/status/barcode/3SDEVC001",
			http.StatusOK, tracking)
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		resp, err := a.TrackShipment(t.Context(), "3SDEVC001")
		require.NoError(t, err)
		assert.Equal(t, "3SDEVC001", resp.TrackingNumber)
		assert.Equal(t, "postnl", resp.Carrier)
		assert.Equal(t, StatusOutForDelivery, resp.NormalizedStatus)
		require.Len(t, resp.Events, 1)
		assert.Equal(t, StatusOutForDelivery, resp.Events[0].NormalizedStatus)
		// Timestamp should be RFC3339-formatted.
		assert.Equal(t, "2022-11-08T10:13:20Z", resp.Events[0].Timestamp)
	})

	t.Run("no current status returns unknown", func(t *testing.T) {
		t.Parallel()
		srv := postnlMockServer(t, http.MethodGet, "/shipment/v2/status/barcode/3SNONE",
			http.StatusOK, postnlTrackingResponse{})
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		resp, err := a.TrackShipment(t.Context(), "3SNONE")
		require.NoError(t, err)
		assert.Equal(t, StatusUnknown, resp.NormalizedStatus)
	})

	t.Run("upstream error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		_, err := a.TrackShipment(t.Context(), "3SNOTFOUND")
		assert.Error(t, err)
	})
}

func TestPostNLAdapter_FetchLabel(t *testing.T) {
	t.Parallel()

	t.Run("successful fetch", func(t *testing.T) {
		t.Parallel()
		srv := postnlMockServer(t, http.MethodPost, "/shipment/delivery/v4/label",
			http.StatusOK, postnlOKResponse("3SLABEL001"))
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		resp, err := a.FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "3SLABEL001",
			Format:         LabelFormatPDF,
		})
		require.NoError(t, err)
		assert.Equal(t, "3SLABEL001", resp.TrackingNumber)
		assert.Equal(t, LabelFormatPDF, resp.Format)
		assert.Equal(t, "application/pdf", resp.MimeType)
		assert.NotEmpty(t, resp.Data)
	})

	t.Run("ZPL format", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload postnlLabelRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			assert.Equal(t, "zpl", payload.LabelSettings.OutputType)

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(postnlOKResponse("3SZPL001"))
		}))
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		resp, err := a.FetchLabel(t.Context(), LabelRequest{
			TrackingNumber: "3SZPL001",
			Format:         LabelFormatZPL,
		})
		require.NoError(t, err)
		assert.Equal(t, LabelFormatZPL, resp.Format)
	})

	t.Run("empty label response", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(postnlEmaOkResponse{})
		}))
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		_, err := a.FetchLabel(t.Context(), LabelRequest{TrackingNumber: "3SEMPTY"})
		assert.Error(t, err)
	})
}

func TestPostNLAdapter_CancelShipment(t *testing.T) {
	t.Parallel()

	a := NewPostNLAdapter("key", "123456", "ABCD", zap.NewNop())
	_, err := a.CancelShipment(t.Context(), "3STEST")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestPostNLAdapter_UpdateShipment(t *testing.T) {
	t.Parallel()

	a := NewPostNLAdapter("key", "123456", "ABCD", zap.NewNop())
	_, err := a.UpdateShipment(t.Context(), UpdateRequest{Carrier: "postnl"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestPostNLAdapter_BookReturn(t *testing.T) {
	t.Parallel()

	t.Run("successful return", func(t *testing.T) {
		t.Parallel()
		srv := postnlMockServer(t, http.MethodPost, "/shipment/delivery/v4/return/generate",
			http.StatusOK, postnlOKResponse("3SRETURN001"))
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		resp, err := a.BookReturn(t.Context(), ReturnRequest{
			Carrier:  "postnl",
			Sender:   postnlTestReceiver(),
			Receiver: postnlTestSender(),
		})
		require.NoError(t, err)
		assert.Equal(t, "3SRETURN001", resp.TrackingNumber)
		assert.Equal(t, "postnl", resp.Carrier)
		assert.Equal(t, "booked", resp.Status)
	})

	t.Run("upstream error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer srv.Close()

		a := newTestPostNLAdapter(t, srv.URL)
		_, err := a.BookReturn(t.Context(), ReturnRequest{
			Carrier: "postnl",
			Sender:  postnlTestReceiver(),
		})
		assert.Error(t, err)
	})
}

// =========================================================================
// Unit tests for helper functions
// =========================================================================

func TestPostNLBuildAddress(t *testing.T) {
	t.Parallel()

	addr := postnlBuildAddress(Address{
		Name:        "Test BV",
		Street:      "Industrieweg",
		HouseNumber: "42",
		Supplement:  "unit B",
		City:        "Rotterdam",
		PostalCode:  "3000AA",
		Country:     "NL",
	})

	assert.Equal(t, "Rotterdam", addr.City)
	assert.Equal(t, "NL", addr.CountryIso)
	assert.Equal(t, "42", addr.HouseNumber)
	assert.Equal(t, "unit B", addr.HouseNumberAddition)
	assert.Equal(t, "Industrieweg 42", addr.AddressLine)
	assert.Equal(t, "Test BV", addr.CompanyName)
}

func TestPostNLBuildServices(t *testing.T) {
	t.Parallel()

	t.Run("no addons returns nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, postnlBuildServices(nil))
	})

	t.Run("insurance", func(t *testing.T) {
		t.Parallel()
		svc := postnlBuildServices([]AddOn{
			{Type: AddOnInsurance, InsuranceValue: 500.0},
		})
		require.NotNil(t, svc)
		require.NotNil(t, svc.InsuredValue)
		assert.Equal(t, 500.0, *svc.InsuredValue)
	})

	t.Run("stated address only", func(t *testing.T) {
		t.Parallel()
		svc := postnlBuildServices([]AddOn{{Type: AddOnStatedAddressOnly}})
		require.NotNil(t, svc)
		assert.True(t, svc.StatedAddressOnly)
	})

	t.Run("age check defaults signature", func(t *testing.T) {
		t.Parallel()
		svc := postnlBuildServices([]AddOn{
			{Type: AddOnAgeCheck, Instructions: "18+"},
		})
		require.NotNil(t, svc)
		assert.Equal(t, "18+", svc.MinimalAgeCheck)
		assert.Equal(t, "signature", svc.DeliveryConfirmation)
	})

	t.Run("evening delivery", func(t *testing.T) {
		t.Parallel()
		svc := postnlBuildServices([]AddOn{{Type: AddOnEveningDelivery}})
		require.NotNil(t, svc)
		require.NotNil(t, svc.DeliveryWindow)
		assert.Equal(t, "evening", svc.DeliveryWindow.Service)
	})

	t.Run("guaranteed before uses instructions", func(t *testing.T) {
		t.Parallel()
		svc := postnlBuildServices([]AddOn{
			{Type: AddOnGuaranteedBefore, Instructions: "10:00"},
		})
		require.NotNil(t, svc)
		require.NotNil(t, svc.DeliveryWindow)
		assert.Equal(t, "10:00", svc.DeliveryWindow.GuaranteedBefore)
	})
}

func TestPostNLBuildItems(t *testing.T) {
	t.Parallel()

	colli := []Colli{
		{ID: "c1", Weight: 2.5, Dimensions: Dimensions{Length: 30, Width: 20, Height: 15}},
		{ID: "c2", Weight: 1.0, Dimensions: Dimensions{Length: 20, Width: 15, Height: 10}},
	}
	items := postnlBuildItems(colli)
	require.Len(t, items, 2)
	require.NotNil(t, items[0].Dimension)
	assert.Equal(t, 2500, items[0].Dimension.Weight) // 2.5 kg → 2500 g
	assert.Equal(t, 300, items[0].Dimension.Length)  // 30 cm → 300 mm
	assert.Equal(t, 200, items[0].Dimension.Width)
	assert.Equal(t, 150, items[0].Dimension.Height)
}

func TestPostNLBuildInternationalData(t *testing.T) {
	t.Parallel()

	t.Run("NL domestic returns nil", func(t *testing.T) {
		t.Parallel()
		s := Shipment{Receiver: Address{Country: "NL"}}
		assert.Nil(t, postnlBuildInternationalData(s))
	})

	t.Run("EU destination uses track_trace bundle", func(t *testing.T) {
		t.Parallel()
		s := Shipment{Receiver: Address{Country: "DE"}}
		data := postnlBuildInternationalData(s)
		require.NotNil(t, data)
		assert.Equal(t, "track_trace", data.Bundle)
	})

	t.Run("non-EU destination uses insured bundle", func(t *testing.T) {
		t.Parallel()
		s := Shipment{Receiver: Address{Country: "US"}}
		data := postnlBuildInternationalData(s)
		require.NotNil(t, data)
		assert.Equal(t, "insured", data.Bundle)
	})

	t.Run("customs content included when present", func(t *testing.T) {
		t.Parallel()
		s := Shipment{
			Receiver: Address{Country: "US"},
			Customs: Customs{
				CustomsValue:    100.0,
				CustomsCurrency: "EUR",
				NatureOfCargo:   "GIFT",
				Items: []CustomsItem{
					{Description: "T-shirt", Quantity: 2, Value: 50.0, NetWeight: 0.5},
				},
			},
		}
		data := postnlBuildInternationalData(s)
		require.NotNil(t, data)
		require.NotNil(t, data.Customs)
		assert.Equal(t, "31", data.Customs.TransactionCode) // gift = 31
		require.Len(t, data.Customs.Content, 1)
		assert.Equal(t, "T-shirt", data.Customs.Content[0].Description)
		assert.Equal(t, 500, data.Customs.Content[0].Weight) // 0.5 kg → 500 g
	})
}

func TestPostNLParseTimestamp(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2022-11-08T10:13:20Z", postnlParseTimestamp("08-11-2022 10:13:20"))
	assert.Equal(t, "bad-ts", postnlParseTimestamp("bad-ts")) // passthrough on parse failure
}

func TestPostNLShipmentType(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "parcel", postnlShipmentType(""))
	assert.Equal(t, "parcel", postnlShipmentType("home"))
	assert.Equal(t, "letterbox", postnlShipmentType("letterbox"))
	assert.Equal(t, "packet", postnlShipmentType("packet"))
	assert.Equal(t, "letter", postnlShipmentType("letter"))
}
