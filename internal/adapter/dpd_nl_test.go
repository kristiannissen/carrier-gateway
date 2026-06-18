// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/dpd_nl_test.go.
package adapter

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// =========================================================================
// Mock adapter tests
// =========================================================================

func TestMockDPDNLAdapter_BookShipment(t *testing.T) {
	t.Parallel()

	t.Run("no colli returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewMockDPDNLAdapter().BookShipment(t.Context(), BookingRequest{
			Carrier:  "dpd_nl",
			Shipment: Shipment{Sender: dpdNLTestSender(), Receiver: dpdNLTestReceiver()},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one colli")
	})

	t.Run("valid request returns booked response", func(t *testing.T) {
		t.Parallel()
		resp, err := NewMockDPDNLAdapter().BookShipment(t.Context(), dpdNLMinimalRequest())
		require.NoError(t, err)
		assert.Equal(t, "dpd_nl", resp.Carrier)
		assert.NotEmpty(t, resp.TrackingNumber)
		assert.Equal(t, "booked", resp.Status)
		assert.Len(t, resp.Colli, 1)
	})

	t.Run("custom func overrides default", func(t *testing.T) {
		t.Parallel()
		want := errors.New("upstream timeout")
		a := &MockDPDNLAdapter{
			BookShipmentFunc: func(_ BookingRequest) (*BookingResponse, error) { return nil, want },
		}
		_, err := a.BookShipment(t.Context(), dpdNLMinimalRequest())
		assert.Equal(t, want, err)
	})
}

func TestMockDPDNLAdapter_TrackShipment(t *testing.T) {
	t.Parallel()

	resp, err := NewMockDPDNLAdapter().TrackShipment(t.Context(), "05222835034925")
	require.NoError(t, err)
	assert.Equal(t, "05222835034925", resp.TrackingNumber)
	assert.Equal(t, StatusInTransit, resp.NormalizedStatus)
}

func TestMockDPDNLAdapter_FetchLabel(t *testing.T) {
	t.Parallel()

	resp, err := NewMockDPDNLAdapter().FetchLabel(t.Context(), LabelRequest{
		TrackingNumber: "05222835034925",
		Format:         LabelFormatPDF,
	})
	require.NoError(t, err)
	assert.Equal(t, LabelFormatPDF, resp.Format)
	assert.NotEmpty(t, resp.Data)
}

func TestMockDPDNLAdapter_CancelShipment_NotSupported(t *testing.T) {
	t.Parallel()

	_, err := NewMockDPDNLAdapter().CancelShipment(t.Context(), "05222835034925")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestMockDPDNLAdapter_UpdateShipment_NotSupported(t *testing.T) {
	t.Parallel()

	_, err := NewMockDPDNLAdapter().UpdateShipment(t.Context(), UpdateRequest{TrackingNumber: "05222835034925"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

// =========================================================================
// Real adapter — SOAP payload tests
// =========================================================================

// newDPDNLTestServer creates a DPDNLAdapter pointing at an httptest.Server.
// loginResp is returned for every LoginService call; shipmentResp for ShipmentService.
// The returned request-body string pointer is set on each ShipmentService POST.
func newDPDNLTestServer(t *testing.T, loginResp, shipmentResp, trackingResp string) (*DPDNLAdapter, *string) {
	t.Helper()
	var lastShipmentBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()

		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		switch action {
		case "login":
			w.Header().Set("Content-Type", "text/xml")
			_, _ = w.Write([]byte(loginResp))
		case "storeOrders":
			lastShipmentBody = string(body)
			w.Header().Set("Content-Type", "text/xml")
			_, _ = w.Write([]byte(shipmentResp))
		case "getTrackingData":
			w.Header().Set("Content-Type", "text/xml")
			_, _ = w.Write([]byte(trackingResp))
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)

	a := NewDPDNLAdapter("TESTID00", "testpass", zaptest.NewLogger(t))
	a.LoginURL = srv.URL
	a.ShipmentURL = srv.URL
	a.TrackingURL = srv.URL
	return a, &lastShipmentBody
}

func TestDPDNLAdapter_BookShipment_PayloadShape(t *testing.T) {
	t.Parallel()

	a, captured := newDPDNLTestServer(t, dpdNLLoginResponse("abc123tok", "0522"), dpdNLShipmentResponse("05222835034925"), "")

	resp, err := a.BookShipment(t.Context(), dpdNLMinimalRequest())
	require.NoError(t, err)
	assert.Equal(t, "dpd_nl", resp.Carrier)
	assert.Equal(t, "05222835034925", resp.TrackingNumber)
	assert.Equal(t, "booked", resp.Status)

	body := *captured
	assert.Contains(t, body, "<delisId>TESTID00</delisId>")
	assert.Contains(t, body, "<authToken>abc123tok</authToken>")
	assert.Contains(t, body, "<product>B2B</product>")
	assert.Contains(t, body, "<sendingDepot>0522</sendingDepot>")
	assert.Contains(t, body, "<weight>150</weight>") // 1.5 kg → 150 dg
	assert.Contains(t, body, "<name1>Test Sender</name1>")
	assert.Contains(t, body, "<name1>Test Receiver</name1>")
	assert.Contains(t, body, "storeOrders")
}

func TestDPDNLAdapter_BookShipment_B2CForHomeDelivery(t *testing.T) {
	t.Parallel()

	a, captured := newDPDNLTestServer(t, dpdNLLoginResponse("tok", "0522"), dpdNLShipmentResponse("05222835034926"), "")

	req := dpdNLMinimalRequest()
	req.Shipment.DeliveryType = "home"
	_, err := a.BookShipment(t.Context(), req)
	require.NoError(t, err)

	assert.Contains(t, *captured, "<product>B2C</product>")
}

func TestDPDNLAdapter_BookShipment_PSDForServicePoint(t *testing.T) {
	t.Parallel()

	a, captured := newDPDNLTestServer(t, dpdNLLoginResponse("tok", "0522"), dpdNLShipmentResponse("05222835034927"), "")

	req := dpdNLMinimalRequest()
	req.Shipment.DeliveryType = "servicepoint"
	req.Shipment.Receiver.ServicePointID = "787611436"
	req.Shipment.Receiver.Email = "customer@example.com"
	_, err := a.BookShipment(t.Context(), req)
	require.NoError(t, err)

	assert.Contains(t, *captured, "<product>PSD</product>")
	assert.Contains(t, *captured, "<parcelShopId>787611436</parcelShopId>")
}

func TestDPDNLAdapter_BookShipment_ReturnShipment(t *testing.T) {
	t.Parallel()

	a, captured := newDPDNLTestServer(t, dpdNLLoginResponse("tok", "0522"), dpdNLShipmentResponse("05222835034928"), "")

	req := dpdNLMinimalRequest()
	req.Shipment.DeliveryType = "return"
	_, err := a.BookShipment(t.Context(), req)
	require.NoError(t, err)

	assert.Contains(t, *captured, "<product>B2C</product>")
	assert.Contains(t, *captured, "<returns>true</returns>")
}

func TestDPDNLAdapter_BookShipment_PredictViaEmailAddOn(t *testing.T) {
	t.Parallel()

	a, captured := newDPDNLTestServer(t, dpdNLLoginResponse("tok", "0522"), dpdNLShipmentResponse("05222835034929"), "")

	req := dpdNLMinimalRequest()
	req.Shipment.Receiver.Email = "notify@example.com"
	_, err := a.BookShipment(t.Context(), req)
	require.NoError(t, err)

	assert.Contains(t, *captured, "<predict>")
	assert.Contains(t, *captured, "notify@example.com")
}

func TestDPDNLAdapter_BookShipment_LabelCachedForFetchLabel(t *testing.T) {
	t.Parallel()

	a, _ := newDPDNLTestServer(t, dpdNLLoginResponse("tok", "0522"), dpdNLShipmentResponse("05222835034930"), "")

	bookResp, err := a.BookShipment(t.Context(), dpdNLMinimalRequest())
	require.NoError(t, err)

	// FetchLabel must succeed from cache — no network call should be needed.
	labelResp, err := a.FetchLabel(t.Context(), LabelRequest{
		TrackingNumber: bookResp.TrackingNumber,
		Format:         LabelFormatPDF,
	})
	require.NoError(t, err)
	assert.Equal(t, LabelFormatPDF, labelResp.Format)
	assert.NotEmpty(t, labelResp.Data)
}

func TestDPDNLAdapter_FetchLabel_CacheMiss(t *testing.T) {
	t.Parallel()

	a := NewDPDNLAdapter("ID", "pw", zaptest.NewLogger(t))
	_, err := a.FetchLabel(t.Context(), LabelRequest{
		TrackingNumber: "99999999999999",
		Format:         LabelFormatPDF,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in cache")
}

func TestDPDNLAdapter_FetchLabel_UnsupportedFormat(t *testing.T) {
	t.Parallel()

	a := NewDPDNLAdapter("ID", "pw", zaptest.NewLogger(t))
	_, err := a.FetchLabel(t.Context(), LabelRequest{
		TrackingNumber: "05222835034925",
		Format:         LabelFormatZPL,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DPD NL does not support ZPL")
}

func TestDPDNLAdapter_CancelShipment_NotSupported(t *testing.T) {
	t.Parallel()

	a := NewDPDNLAdapter("ID", "pw", zaptest.NewLogger(t))
	_, err := a.CancelShipment(t.Context(), "05222835034925")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestDPDNLAdapter_UpdateShipment_NotSupported(t *testing.T) {
	t.Parallel()

	a := NewDPDNLAdapter("ID", "pw", zaptest.NewLogger(t))
	_, err := a.UpdateShipment(t.Context(), UpdateRequest{TrackingNumber: "05222835034925"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestDPDNLAdapter_TrackShipment_ParsesResponse(t *testing.T) {
	t.Parallel()

	a, _ := newDPDNLTestServer(t, dpdNLLoginResponse("tok", "0522"), "", dpdNLTrackingResponse("05222835034925", "DELIVERED"))

	resp, err := a.TrackShipment(t.Context(), "05222835034925")
	require.NoError(t, err)
	assert.Equal(t, "05222835034925", resp.TrackingNumber)
	assert.Equal(t, "dpd_nl", resp.Carrier)
	assert.Equal(t, "DELIVERED", resp.Status)
	assert.Equal(t, StatusDelivered, resp.NormalizedStatus)
}

func TestDPDNLAdapter_TokenRefreshOnLOGIN5(t *testing.T) {
	t.Parallel()

	loginCalls := 0
	shipmentCalls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := strings.Trim(r.Header.Get("SOAPAction"), `"`)
		switch action {
		case "login":
			loginCalls++
			w.Header().Set("Content-Type", "text/xml")
			_, _ = w.Write([]byte(dpdNLLoginResponse("freshtoken", "0522")))
		case "storeOrders":
			shipmentCalls++
			w.Header().Set("Content-Type", "text/xml")
			if shipmentCalls == 1 {
				// First call returns a token fault.
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(dpdNLFaultResponse("LOGIN_5")))
				return
			}
			// Second call succeeds.
			_, _ = w.Write([]byte(dpdNLShipmentResponse("05222835034925")))
		}
	}))
	t.Cleanup(srv.Close)

	a := NewDPDNLAdapter("TESTID00", "testpass", zaptest.NewLogger(t))
	a.LoginURL = srv.URL
	a.ShipmentURL = srv.URL
	a.TrackingURL = srv.URL

	resp, err := a.BookShipment(t.Context(), dpdNLMinimalRequest())
	require.NoError(t, err)
	assert.Equal(t, "05222835034925", resp.TrackingNumber)

	assert.Equal(t, 2, loginCalls, "token should be refreshed once after LOGIN_5")
	assert.Equal(t, 2, shipmentCalls, "storeOrders should be retried after token refresh")
}

func TestDPDNLAdapter_BookShipment_MultiColli(t *testing.T) {
	t.Parallel()

	a, captured := newDPDNLTestServer(t, dpdNLLoginResponse("tok", "0522"), dpdNLMultiColliShipmentResponse(), "")

	req := dpdNLMinimalRequest()
	req.Shipment.Colli = []Colli{
		{ID: "box-1", Weight: 2.0, Items: []Item{{Description: "Shoes", Quantity: 1, Weight: 2.0}}},
		{ID: "box-2", Weight: 1.5, Items: []Item{{Description: "Shirt", Quantity: 1, Weight: 1.5}}},
	}
	resp, err := a.BookShipment(t.Context(), req)
	require.NoError(t, err)
	assert.Len(t, resp.Colli, 2)

	body := *captured
	// Two <parcels> blocks expected.
	assert.Equal(t, 2, strings.Count(body, "<parcels>"))
	assert.Contains(t, body, "<weight>200</weight>") // 2.0 kg
	assert.Contains(t, body, "<weight>150</weight>") // 1.5 kg
}

func TestDPDNLAdapter_BookShipment_NoColli(t *testing.T) {
	t.Parallel()

	a := NewDPDNLAdapter("ID", "pw", zaptest.NewLogger(t))
	_, err := a.BookShipment(t.Context(), BookingRequest{
		Carrier:  "dpd_nl",
		Shipment: Shipment{Sender: dpdNLTestSender(), Receiver: dpdNLTestReceiver()},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one colli")
}

// =========================================================================
// Weight conversion
// =========================================================================

func TestKgToDecagrams(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kg   float64
		want int
	}{
		{1.0, 100},
		{1.5, 150},
		{0.075, 8}, // ceil(7.5) = 8
		{0.1, 10},
		{31.5, 3150}, // DPD max weight
	}
	for _, tc := range cases {
		tc := tc
		t.Run("", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, kgToDecagrams(tc.kg))
		})
	}
}

// =========================================================================
// Status normalisation
// =========================================================================

func TestNormalizeDPDNLStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want TrackingStatus
	}{
		{"DELIVERED", StatusDelivered},
		{"OUT_FOR_DELIVERY", StatusOutForDelivery},
		{"IN_TRANSIT", StatusInTransit},
		{"COLLECTED", StatusPickedUp},
		{"NOT_DELIVERED", StatusFailed},
		{"RETURNED", StatusReturned},
		{"CREATED", StatusBooked},
		{"UNKNOWN_CODE_XYZ", StatusUnknown},
		{"", StatusUnknown},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, normalizeDPDNLStatus(tc.raw))
		})
	}
}

// =========================================================================
// Helpers & canned responses
// =========================================================================

func dpdNLTestSender() Address {
	return Address{
		Name:        "Test Sender",
		Street:      "Pakket Onderweg",
		HouseNumber: "1",
		City:        "Oirschot",
		PostalCode:  "5688HB",
		Country:     "NL",
		Phone:       "+31612345678",
		Email:       "sender@test.nl",
	}
}

func dpdNLTestReceiver() Address {
	return Address{
		Name:        "Test Receiver",
		Street:      "High Tech Campus",
		HouseNumber: "1",
		City:        "Eindhoven",
		PostalCode:  "5656AE",
		Country:     "NL",
		Phone:       "+31698765432",
		Email:       "receiver@test.nl",
	}
}

func dpdNLMinimalRequest() BookingRequest {
	return BookingRequest{
		Carrier: "dpd_nl",
		Shipment: Shipment{
			Sender:   dpdNLTestSender(),
			Receiver: dpdNLTestReceiver(),
			Colli: []Colli{
				{
					ID:     "box-1",
					Weight: 1.5,
					Items:  []Item{{Description: "Goods", Quantity: 1, Weight: 1.5}},
				},
			},
		},
	}
}

func dpdNLLoginResponse(token, depot string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <getAuthResponse xmlns="http://dpd.com/common/service/types/LoginService/2.1">
      <return>
        <delisId>TESTID00</delisId>
        <customerUid>TESTID00</customerUid>
        <authToken>` + token + `</authToken>
        <depot>` + depot + `</depot>
        <authTokenExpires>2099-01-01T12:00:00.00</authTokenExpires>
      </return>
    </getAuthResponse>
  </soap:Body>
</soap:Envelope>`
}

func dpdNLShipmentResponse(parcelLabelNumber string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <storeOrdersResponse xmlns="http://dpd.com/common/service/types/ShipmentService/3.5">
      <orderResult>
        <parcellabelsPDF>dGVzdC1sYWJlbC1wZGY=</parcellabelsPDF>
        <shipmentResponses>
          <mpsId>MPS0522283503492520241212</mpsId>
          <parcelInformation>
            <parcelLabelNumber>` + parcelLabelNumber + `</parcelLabelNumber>
          </parcelInformation>
        </shipmentResponses>
      </orderResult>
    </storeOrdersResponse>
  </soap:Body>
</soap:Envelope>`
}

func dpdNLMultiColliShipmentResponse() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <storeOrdersResponse xmlns="http://dpd.com/common/service/types/ShipmentService/3.5">
      <orderResult>
        <parcellabelsPDF>dGVzdC1sYWJlbC1wZGY=</parcellabelsPDF>
        <shipmentResponses>
          <mpsId>MPS0522283503492520241212</mpsId>
          <parcelInformation>
            <parcelLabelNumber>05222835034925</parcelLabelNumber>
          </parcelInformation>
        </shipmentResponses>
        <shipmentResponses>
          <mpsId>MPS0522283503492520241212</mpsId>
          <parcelInformation>
            <parcelLabelNumber>05222835034926</parcelLabelNumber>
          </parcelInformation>
        </shipmentResponses>
      </orderResult>
    </storeOrdersResponse>
  </soap:Body>
</soap:Envelope>`
}

func dpdNLTrackingResponse(parcelLabelNumber, status string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <getTrackingDataResponse xmlns="http://dpd.com/common/service/types/ParcelLifecycleService/2.0">
      <parcelLifeCycleData>
        <parcelLabelNumber>` + parcelLabelNumber + `</parcelLabelNumber>
        <statusInfo>
          <status>` + status + `</status>
          <date>20240115</date>
          <depotCity>AMSTERDAM</depotCity>
        </statusInfo>
        <parcelEvent>
          <description>Parcel delivered to recipient</description>
          <eventDate>20240115</eventDate>
          <eventTime>143000</eventTime>
          <depotCity>AMSTERDAM</depotCity>
          <parcelEventCode>
            <eventCode>` + status + `</eventCode>
            <description>Delivered to recipient</description>
          </parcelEventCode>
        </parcelEvent>
      </parcelLifeCycleData>
    </getTrackingDataResponse>
  </soap:Body>
</soap:Envelope>`
}

func dpdNLFaultResponse(faultString string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <soap:Fault>
      <faultcode>soap:Client</faultcode>
      <faultstring>` + faultString + `</faultstring>
    </soap:Fault>
  </soap:Body>
</soap:Envelope>`
}
