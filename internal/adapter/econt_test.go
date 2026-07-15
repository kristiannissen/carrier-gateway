// Package adapter provides tests for the Econt adapter.
// This file is located at /internal/adapter/econt_test.go.
package adapter

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func econtTestRequest() BookingRequest {
	return BookingRequest{
		Carrier: "econt",
		Shipment: Shipment{
			Sender: Address{
				Name:        "Unisport Group",
				Street:      "Tsarigradsko shose",
				HouseNumber: "115",
				City:        "Sofia",
				PostalCode:  "1784",
				Country:     "BG",
				Phone:       "+35929876543",
				Email:       "warehouse@unisport.bg",
			},
			Receiver: Address{
				Name:        "Ivan Petrov",
				Street:      "Vitosha",
				HouseNumber: "20",
				City:        "Sofia",
				PostalCode:  "1000",
				Country:     "BG",
				Phone:       "+35988123456",
				Email:       "ivan@example.bg",
			},
			TotalWeight: 1.5,
			Colli: []Colli{
				{
					ID:         "colli-bg-01",
					Weight:     1.5,
					Dimensions: Dimensions{Length: 30, Width: 20, Height: 10},
					Items:      []Item{{Description: "Football boots", Weight: 1.5, Quantity: 1, Value: 89.99}},
				},
			},
			DeliveryType: "home",
		},
		IdempotencyKey: "order-bg-001",
	}
}

func econtServeCreate(t *testing.T, shipmentNumber, pdfURL string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		body, _ := io.ReadAll(r.Body)
		var req econtCreateLabelRequest
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "create", req.Mode)
		assert.NotNil(t, req.Label)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(econtCreateLabelResponse{
			Label: &econtShipmentStatus{
				ShipmentNumber:        shipmentNumber,
				ShortDeliveryStatusEn: "Prepared in eEcont",
				PDFURL:                pdfURL,
			},
		})
	}
}

func econtServeStatuses(t *testing.T, shipmentNumber, statusEn, pdfURL string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(econtGetShipmentStatusesResponse{
			ShipmentStatuses: []econtShipmentStatusResultElement{
				{
					Status: &econtShipmentStatus{
						ShipmentNumber:        shipmentNumber,
						ShortDeliveryStatusEn: statusEn,
						PDFURL:                pdfURL,
						TrackingEvents: []econtTrackingEvent{
							{DestinationType: "client", Time: "2026-01-01T10:00:00", CityNameEn: "Sofia"},
						},
					},
				},
			},
		})
	}
}

// ── mock adapter tests ────────────────────────────────────────────────────────

func TestMockEcontAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	resp, err := (&MockEcontAdapter{}).BookShipment(t.Context(), econtTestRequest())
	require.NoError(t, err)
	assert.Equal(t, "econt", resp.Carrier)
	assert.Equal(t, "booked", resp.Status)
	assert.Contains(t, resp.TrackingNumber, "MOCK-ECONT-")
	require.Len(t, resp.Colli, 1)
	assert.Equal(t, "colli-bg-01", resp.Colli[0].ID)
}

func TestMockEcontAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	resp, err := (&MockEcontAdapter{}).TrackShipment(t.Context(), "123456789")
	require.NoError(t, err)
	assert.Equal(t, "econt", resp.Carrier)
	assert.Equal(t, StatusBooked, resp.NormalizedStatus)
	assert.NotEmpty(t, resp.Events)
}

func TestMockEcontAdapter_FetchLabel(t *testing.T) {
	t.Parallel()

	t.Run("PDF format", func(t *testing.T) {
		t.Parallel()
		resp, err := (&MockEcontAdapter{}).FetchLabel(t.Context(), LabelRequest{TrackingNumber: "123456789", Format: LabelFormatPDF})
		require.NoError(t, err)
		assert.Equal(t, LabelFormatPDF, resp.Format)
		assert.NotEmpty(t, resp.Data)
	})

	t.Run("unsupported format", func(t *testing.T) {
		t.Parallel()
		_, err := (&MockEcontAdapter{}).FetchLabel(t.Context(), LabelRequest{TrackingNumber: "123456789", Format: LabelFormatZPL})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ZPL")
	})
}

func TestMockEcontAdapter_CancelShipment(t *testing.T) {
	t.Parallel()

	resp, err := (&MockEcontAdapter{}).CancelShipment(t.Context(), "123456789")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", resp.Status)
	assert.Equal(t, "econt", resp.Carrier)
}

func TestMockEcontAdapter_UpdateShipment(t *testing.T) {
	t.Parallel()

	resp, err := (&MockEcontAdapter{}).UpdateShipment(t.Context(), UpdateRequest{
		TrackingNumber: "123456789",
		ReceiverPhone:  "+35988999888",
		ReceiverEmail:  "new@example.bg",
	})
	require.NoError(t, err)
	assert.Equal(t, "updated", resp.Status)
	assert.Contains(t, resp.UpdatedFields, "phone")
	assert.Contains(t, resp.UpdatedFields, "email")
}

// ── live adapter tests ────────────────────────────────────────────────────────

func TestEcontAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(econtServeCreate(t, "987654321", "https://example.com/label.pdf"))
	t.Cleanup(srv.Close)

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	resp, err := a.BookShipment(t.Context(), econtTestRequest())
	require.NoError(t, err)
	assert.Equal(t, "987654321", resp.TrackingNumber)
	assert.Equal(t, "econt", resp.Carrier)
	assert.Equal(t, "booked", resp.Status)
	assert.Equal(t, "https://example.com/label.pdf", resp.LabelURL)
}

func TestEcontAdapter_BookShipment_APIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(econtCreateLabelResponse{
			Error: &econtError{Type: "InvalidAddressException", Message: "Invalid receiver address"},
		})
	}))
	t.Cleanup(srv.Close)

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	_, err := a.BookShipment(t.Context(), econtTestRequest())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid receiver address")
}

func TestEcontAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(econtServeStatuses(t, "987654321", "Delivered", ""))
	t.Cleanup(srv.Close)

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	resp, err := a.TrackShipment(t.Context(), "987654321")
	require.NoError(t, err)
	assert.Equal(t, StatusDelivered, resp.NormalizedStatus)
	assert.Equal(t, "Delivered", resp.OriginalStatus)
	assert.Len(t, resp.Events, 1)
	assert.Equal(t, StatusBooked, resp.Events[0].NormalizedStatus)
}

func TestEcontAdapter_FetchLabel(t *testing.T) {
	t.Parallel()

	pdfContent := []byte("%PDF-1.4 econt label")

	// The test server handles two paths:
	// 1. getShipmentStatuses → returns pdfURL pointing to /label.pdf on the same server
	// 2. /label.pdf → returns the PDF bytes
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/label.pdf" {
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write(pdfContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(econtGetShipmentStatusesResponse{
			ShipmentStatuses: []econtShipmentStatusResultElement{
				{
					Status: &econtShipmentStatus{
						ShipmentNumber:        "987654321",
						ShortDeliveryStatusEn: "Prepared in eEcont",
						PDFURL:                srvURL + "/label.pdf",
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	resp, err := a.FetchLabel(t.Context(), LabelRequest{TrackingNumber: "987654321", Format: LabelFormatPDF})
	require.NoError(t, err)
	assert.Equal(t, LabelFormatPDF, resp.Format)
	assert.NotEmpty(t, resp.Data)
}

func TestEcontAdapter_CancelShipment(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req econtDeleteLabelsRequest
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, []string{"987654321"}, req.ShipmentNumbers)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(econtDeleteLabelsResponse{
			Results: []econtDeleteLabelsResultElement{{ShipmentNum: "987654321"}},
		})
	}))
	t.Cleanup(srv.Close)

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	resp, err := a.CancelShipment(t.Context(), "987654321")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", resp.Status)
}

func TestEcontAdapter_CancelShipment_AlreadyAccepted(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(econtDeleteLabelsResponse{
			Results: []econtDeleteLabelsResultElement{
				{
					ShipmentNum: "987654321",
					Error:       &econtError{Type: "LabelAlreadyAcceptedException", Message: "Shipment has already been accepted"},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	_, err := a.CancelShipment(t.Context(), "987654321")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already been accepted")
}

func TestEcontAdapter_UpdateShipment(t *testing.T) {
	t.Parallel()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			// checkPossibleShipmentEditions
			_ = json.NewEncoder(w).Encode(econtCheckEditionsResponse{
				PossibleShipmentEditions: []econtCheckEditionsResultElement{
					{ShipmentNum: 987654321, PossibleShipmentEditions: []string{"ADDRESS_CHANGE"}},
				},
			})
			return
		}
		// updateLabel
		_ = json.NewEncoder(w).Encode(econtUpdateLabelResponse{
			Label: &econtShipmentStatus{ShipmentNumber: "987654321"},
		})
	}))
	t.Cleanup(srv.Close)

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	resp, err := a.UpdateShipment(t.Context(), UpdateRequest{
		TrackingNumber: "987654321",
		ReceiverPhone:  "+35988999888",
		ReceiverEmail:  "new@example.bg",
	})
	require.NoError(t, err)
	assert.Equal(t, "updated", resp.Status)
	assert.Contains(t, resp.UpdatedFields, "phone")
	assert.Contains(t, resp.UpdatedFields, "email")
	assert.Equal(t, 2, callCount)
}

func TestEcontAdapter_UpdateShipment_NoEditionsAvailable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(econtCheckEditionsResponse{
			PossibleShipmentEditions: []econtCheckEditionsResultElement{
				{ShipmentNum: 987654321, PossibleShipmentEditions: nil},
			},
		})
	}))
	t.Cleanup(srv.Close)

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	_, err := a.UpdateShipment(t.Context(), UpdateRequest{
		TrackingNumber: "987654321",
		ReceiverPhone:  "+35988999888",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no editions available")
}

// ── builder helpers ───────────────────────────────────────────────────────────

func TestEcontLabelFrom_ServicePointID(t *testing.T) {
	t.Parallel()

	r := econtTestRequest()
	r.Shipment.Receiver.ServicePointID = "SF-123"
	label := econtLabelFrom(r)

	assert.Equal(t, "SF-123", label.ReceiverOfficeCode)
	assert.Nil(t, label.ReceiverAddress)
}

func TestEcontLabelFrom_EmailOnDelivery(t *testing.T) {
	t.Parallel()

	r := econtTestRequest()
	label := econtLabelFrom(r)

	assert.Equal(t, "ivan@example.bg", label.EmailOnDelivery)
}

func TestEcontLabelFrom_COD(t *testing.T) {
	t.Parallel()

	r := econtTestRequest()
	r.Shipment.AddOns = []AddOn{{Type: AddOnCashOnDelivery, CODAmount: 89.99, CODCurrency: "EUR"}}
	label := econtLabelFrom(r)

	require.NotNil(t, label.Services)
	assert.Equal(t, 89.99, label.Services.CDAmount)
	assert.Equal(t, "get", label.Services.CDType)
	assert.Equal(t, "EUR", label.Services.CDCurrency)
}

// ── interface assertions ──────────────────────────────────────────────────────

var (
	_ ManifestAdapter = (*EcontAdapter)(nil)
	_ PickupQuerier   = (*EcontAdapter)(nil)
	_ ManifestAdapter = (*MockEcontAdapter)(nil)
	_ PickupQuerier   = (*MockEcontAdapter)(nil)
)

// ── pickup helpers ────────────────────────────────────────────────────────────

func econtTestPickupRequest() PickupRequest {
	return PickupRequest{
		Carrier: "econt",
		Pickup: PickupWindow{
			Date:      "2026-08-03",
			ReadyTime: "10:00",
			CloseTime: "17:00",
		},
		Contact: PickupContact{
			Name:  "Unisport Warehouse",
			Phone: "+35929876543",
			Email: "warehouse@unisport.bg",
		},
		Address: PickupAddress{
			Street:      "Tsarigradsko shose",
			HouseNumber: "115",
			City:        "Sofia",
			PostalCode:  "1784",
			Country:     "BG",
		},
		EstimatedParcels: 3,
		EstimatedWeight:  12.5,
	}
}

// ── mock adapter pickup tests ─────────────────────────────────────────────────

func TestMockEcontAdapter_BookPickup(t *testing.T) {
	t.Parallel()

	resp, err := (&MockEcontAdapter{}).BookPickup(t.Context(), econtTestPickupRequest())
	require.NoError(t, err)
	assert.Equal(t, "econt", resp.Carrier)
	assert.Equal(t, "booked", resp.Status)
	assert.Contains(t, resp.ConfirmationNumber, "MOCK-ECONT-PU-")
}

func TestMockEcontAdapter_GetPickupByID(t *testing.T) {
	t.Parallel()

	info, err := (&MockEcontAdapter{}).GetPickupByID(t.Context(), "123456")
	require.NoError(t, err)
	assert.Equal(t, "econt", info.Carrier)
	assert.Equal(t, "123456", info.ID)
	assert.Equal(t, "CREATED", info.Status)
}

func TestMockEcontAdapter_ManifestNotSupported(t *testing.T) {
	t.Parallel()

	m := &MockEcontAdapter{}

	_, err := m.UpdatePickup(t.Context(), "123", PickupRequest{})
	require.ErrorIs(t, err, ErrNotSupported)

	err = m.CancelPickup(t.Context(), "econt", "123")
	require.ErrorIs(t, err, ErrNotSupported)

	_, err = m.CloseManifest(t.Context(), ManifestRequest{})
	require.ErrorIs(t, err, ErrNotSupported)

	_, err = m.GetPickupAvailability(t.Context(), PickupAvailabilityRequest{})
	require.ErrorIs(t, err, ErrNotSupported)

	_, err = m.ListPickups(t.Context(), ListPickupsRequest{})
	require.ErrorIs(t, err, ErrNotSupported)

	_, err = m.GetCutoffTime(t.Context(), "1784", "BG")
	require.ErrorIs(t, err, ErrNotSupported)
}

// ── live adapter pickup tests ─────────────────────────────────────────────────

func TestEcontAdapter_BookPickup(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req econtRequestCourierRequest
		require.NoError(t, json.Unmarshal(body, &req))
		assert.NotZero(t, req.RequestTimeFrom)
		assert.NotZero(t, req.RequestTimeTo)
		assert.Equal(t, "pack", req.ShipmentType)
		require.NotNil(t, req.SenderAddress)
		assert.Equal(t, "Sofia", req.SenderAddress.City.Name)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(econtRequestCourierResponse{
			CourierRequestID: "CR-987654",
		})
	}))
	t.Cleanup(srv.Close)

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	resp, err := a.BookPickup(t.Context(), econtTestPickupRequest())
	require.NoError(t, err)
	assert.Equal(t, "econt", resp.Carrier)
	assert.Equal(t, "CR-987654", resp.ConfirmationNumber)
	assert.Equal(t, "booked", resp.Status)
	assert.Equal(t, "10:00", resp.ReadyTime)
	assert.Equal(t, "17:00", resp.CloseTime)
}

func TestEcontAdapter_BookPickup_MissingAddress(t *testing.T) {
	t.Parallel()

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())

	req := econtTestPickupRequest()
	req.Address = PickupAddress{}

	_, err := a.BookPickup(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "address")
}

func TestEcontAdapter_BookPickup_APIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(econtRequestCourierResponse{
			Error: &econtError{Type: "InvalidAddressException", Message: "Address not serviceable"},
		})
	}))
	t.Cleanup(srv.Close)

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	_, err := a.BookPickup(t.Context(), econtTestPickupRequest())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Address not serviceable")
}

func TestEcontAdapter_GetPickupByID(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req econtGetRequestCourierStatusRequest
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, []string{"CR-987654"}, req.RequestCourierIds)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(econtGetRequestCourierStatusResponse{
			RequestCourierStatus: []econtRequestCourierStatusResultElement{
				{Status: &econtRequestCourierStatus{ID: 987654, Status: "taken"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	info, err := a.GetPickupByID(t.Context(), "CR-987654")
	require.NoError(t, err)
	assert.Equal(t, "econt", info.Carrier)
	assert.Equal(t, "COLLECTED", info.Status)
}

func TestEcontAdapter_GetPickupByID_Rejected(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(econtGetRequestCourierStatusResponse{
			RequestCourierStatus: []econtRequestCourierStatusResultElement{
				{Status: &econtRequestCourierStatus{ID: 987654, Status: "reject_client", RejectReason: "client cancelled"}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())
	a.BaseURL = srv.URL

	info, err := a.GetPickupByID(t.Context(), "CR-987654")
	require.NoError(t, err)
	assert.Equal(t, "CANCELLED", info.Status)
}

func TestEcontAdapter_ManifestNotSupported(t *testing.T) {
	t.Parallel()

	a := NewEcontAdapter("test-user", "test-pass", zap.NewNop())

	_, err := a.UpdatePickup(t.Context(), "123", PickupRequest{})
	require.ErrorIs(t, err, ErrNotSupported)

	err = a.CancelPickup(t.Context(), "econt", "123")
	require.ErrorIs(t, err, ErrNotSupported)

	_, err = a.CloseManifest(t.Context(), ManifestRequest{})
	require.ErrorIs(t, err, ErrNotSupported)

	_, err = a.GetPickupAvailability(t.Context(), PickupAvailabilityRequest{})
	require.ErrorIs(t, err, ErrNotSupported)

	_, err = a.ListPickups(t.Context(), ListPickupsRequest{})
	require.ErrorIs(t, err, ErrNotSupported)

	_, err = a.GetCutoffTime(t.Context(), "1784", "BG")
	require.ErrorIs(t, err, ErrNotSupported)
}

func TestEcontPickupTimestamp(t *testing.T) {
	t.Parallel()

	ts, err := econtPickupTimestamp("2026-08-03", "10:00")
	require.NoError(t, err)
	assert.Positive(t, ts)

	_, err = econtPickupTimestamp("not-a-date", "10:00")
	require.Error(t, err)
}

func TestNormalizeEcontEventType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		dtype string
		want  TrackingStatus
	}{
		{"client", StatusBooked},
		{"in_delivery_courier", StatusOutForDelivery},
		{"returned_to_sender", StatusReturned},
		{"failed_delivery", StatusFailed},
		{"unknown_event_xyz", StatusInTransit},
	}

	for _, tc := range cases {
		t.Run(tc.dtype, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, normalizeEcontEventType(tc.dtype))
		})
	}
}
