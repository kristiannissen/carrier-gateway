// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/sandbox_probe_test.go.

//go:build sandbox

// Sandbox probe tests hit live carrier sandbox/test environments to detect
// breaking API changes early. Each sub-test is independent and skipped
// automatically when its required env vars are absent, so partial credentials
// still exercise whichever carriers are configured.
//
// Run locally:
//
//	POSTNORD_API_KEY=... go test -tags sandbox -v -run TestSandboxProbe ./internal/adapter/...
//
// In CI these tests run weekly via .github/workflows/sandbox-monitor.yml.
package adapter

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// probeTimeout is the per-carrier deadline.
// Carrier sandboxes can be slow; 30 s gives headroom without hanging CI.
const probeTimeout = 30 * time.Second

// skipUnless skips the test when any of the named env vars is empty.
func skipUnless(t *testing.T, vars ...string) {
	t.Helper()
	for _, v := range vars {
		if os.Getenv(v) == "" {
			t.Skipf("sandbox probe skipped: %s not set", v)
		}
	}
}

// sandboxLogger returns a development logger for probe output.
func sandboxLogger(t *testing.T) *zap.Logger {
	t.Helper()
	log, err := zap.NewDevelopment()
	require.NoError(t, err)
	return log
}

// sandboxShipment returns a minimal domestic DK shipment suitable for
// test-mode booking calls that do not create real labels.
func sandboxShipment(carrier string) BookingRequest {
	return BookingRequest{
		Carrier:        carrier,
		IdempotencyKey: "sandbox-probe-" + carrier,
		Shipment: Shipment{
			Sender: Address{
				Name:        "Unisport Sandbox",
				Street:      "Roskildevej",
				HouseNumber: "2A",
				City:        "Glostrup",
				PostalCode:  "2600",
				Country:     "DK",
				Phone:       "+4512345678",
				Email:       "sandbox@unisport.dk",
			},
			Receiver: Address{
				Name:        "Test Receiver",
				Street:      "Nørregade",
				HouseNumber: "1",
				City:        "Copenhagen",
				PostalCode:  "1165",
				Country:     "DK",
				Phone:       "+4587654321",
				Email:       "receiver@example.com",
			},
			TotalWeight: 1.0,
			Colli: []Colli{
				{
					ID:     "probe-colli-1",
					Weight: 1.0,
					Dimensions: Dimensions{Length: 20, Width: 15, Height: 10},
					Items: []Item{
						{Description: "Sandbox probe item", Weight: 1.0, Quantity: 1, Value: 10},
					},
				},
			},
		},
	}
}

// assertNotAuthOrServerError fails the test when err looks like a 401/403/5xx,
// which indicates the carrier changed its auth scheme or the endpoint moved.
// A "not found" response is acceptable — it means we reached the API correctly.
func assertNotAuthOrServerError(t *testing.T, carrier string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	msg := strings.ToLower(err.Error())
	for _, code := range []string{"401", "403", "500", "502", "503"} {
		assert.NotContains(t, msg, code,
			"carrier %s: unexpected %s — possible auth or endpoint change", carrier, code)
	}
}

// TestSandboxProbe exercises each carrier sandbox.
// Sub-tests are independent: a single carrier failure does not block others.
func TestSandboxProbe(t *testing.T) {
	t.Run("PostNord", testPostNordSandbox)
	t.Run("Bring", testBringSandbox)
	t.Run("GLS", testGLSSandbox)
	t.Run("DAO", testDAOSandbox)
	t.Run("DHL", testDHLSandbox)
	t.Run("DHLExpress", testDHLExpressSandbox)
	t.Run("FedEx", testFedExSandbox)
	t.Run("Evri", testEvriSandbox)
}

// testPostNordSandbox probes the PostNord tracking API with a dummy number.
// Tracking is read-only. A "not found" response is fine; a 401/5xx is not.
func testPostNordSandbox(t *testing.T) {
	skipUnless(t, "POSTNORD_API_KEY", "POSTNORD_CUSTOMER_NUMBER")

	a := NewPostNordAdapter(
		os.Getenv("POSTNORD_API_KEY"),
		os.Getenv("POSTNORD_CUSTOMER_NUMBER"),
		0,
		sandboxLogger(t),
	)
	ctx, cancel := context.WithTimeout(t.Context(), probeTimeout)
	defer cancel()

	resp, err := a.TrackShipment(ctx, "SANDBOX000000001")
	if err != nil {
		assertNotAuthOrServerError(t, "postnord", err)
		return
	}
	assert.Equal(t, "postnord", resp.Carrier)
}

// testBringSandbox probes the Bring tracking API to verify auth and endpoint
// routing are intact. A "not found" response for the dummy number is acceptable.
func testBringSandbox(t *testing.T) {
	skipUnless(t, "BRING_API_KEY", "BRING_CUSTOMER_ID")

	a := NewBringAdapter(
		os.Getenv("BRING_API_KEY"),
		os.Getenv("BRING_CUSTOMER_ID"),
		os.Getenv("BRING_CUSTOMER_NUMBER"),
		"Unisport Sandbox",
		sandboxLogger(t),
	)
	ctx, cancel := context.WithTimeout(t.Context(), probeTimeout)
	defer cancel()

	resp, err := a.TrackShipment(ctx, "SANDBOX000000001")
	if err != nil {
		assertNotAuthOrServerError(t, "bring", err)
		return
	}
	assert.Equal(t, "bring", resp.Carrier)
}

// testGLSSandbox probes the GLS OAuth token endpoint.
// GLS uses client_credentials; a 401 means the auth scheme or credentials changed.
// contactID is the GLS-assigned shipper contact ID (set via GLS_CONTACT_ID secret).
func testGLSSandbox(t *testing.T) {
	skipUnless(t, "GLS_API_KEY", "GLS_CLIENT_SECRET", "GLS_CONTACT_ID")

	a := NewGLSAdapter(
		os.Getenv("GLS_API_KEY"),
		os.Getenv("GLS_CLIENT_SECRET"),
		os.Getenv("GLS_CONTACT_ID"),
		sandboxLogger(t),
	)
	ctx, cancel := context.WithTimeout(t.Context(), probeTimeout)
	defer cancel()

	token, err := a.bearerToken(ctx)
	require.NoError(t, err, "GLS sandbox auth probe failed")
	assert.NotEmpty(t, token, "GLS sandbox returned empty access token")
}

// testDAOSandbox probes the DAO API in test mode (test=1 on all requests).
// test=1 creates an accepted booking without issuing a real label or charge.
func testDAOSandbox(t *testing.T) {
	skipUnless(t, "DAO_CUSTOMER_ID", "DAO_API_KEY")

	a := NewDAOAdapter(
		os.Getenv("DAO_CUSTOMER_ID"),
		os.Getenv("DAO_API_KEY"),
		true, // testMode — adds test=1; no real label issued
		sandboxLogger(t),
	)
	ctx, cancel := context.WithTimeout(t.Context(), probeTimeout)
	defer cancel()

	resp, err := a.BookShipment(ctx, sandboxShipment("dao"))
	require.NoError(t, err, "DAO sandbox booking failed")
	assert.Equal(t, "dao", resp.Carrier)
	assert.NotEmpty(t, resp.TrackingNumber, "DAO sandbox returned no tracking number")
}

// testDHLSandbox probes the DHL eConnect OAuth token endpoint.
// A successful bearer token confirms the auth URL and credential format are intact.
func testDHLSandbox(t *testing.T) {
	skipUnless(t, "DHL_CLIENT_ID", "DHL_CLIENT_SECRET")

	a := NewDHLAdapter(
		os.Getenv("DHL_CLIENT_ID"),
		os.Getenv("DHL_CLIENT_SECRET"),
		os.Getenv("DHL_CUSTOMER_ID"),
		os.Getenv("DHL_TRACKING_API_KEY"),
		sandboxLogger(t),
	)
	ctx, cancel := context.WithTimeout(t.Context(), probeTimeout)
	defer cancel()

	token, err := a.bearerToken(ctx)
	require.NoError(t, err, "DHL sandbox auth probe failed")
	assert.NotEmpty(t, token, "DHL sandbox returned empty access token")
}

// testDHLExpressSandbox probes the MyDHL Express API.
// Set DHL_EXPRESS_BASE_URL=https://express.api.dhl.com/mydhlapi-test to use
// the test environment without creating real shipments.
func testDHLExpressSandbox(t *testing.T) {
	skipUnless(t, "DHL_EXPRESS_USERNAME", "DHL_EXPRESS_PASSWORD")

	a := NewDHLExpressAdapter(
		os.Getenv("DHL_EXPRESS_USERNAME"),
		os.Getenv("DHL_EXPRESS_PASSWORD"),
		os.Getenv("DHL_EXPRESS_ACCOUNT_NUMBER"),
		"P",
		"",
		sandboxLogger(t),
	)
	if sandboxURL := os.Getenv("DHL_EXPRESS_BASE_URL"); sandboxURL != "" {
		a.BaseURL = sandboxURL
	}

	ctx, cancel := context.WithTimeout(t.Context(), probeTimeout)
	defer cancel()

	// Tracking a dummy AWB is safe and read-only.
	resp, err := a.TrackShipment(ctx, "1234567890")
	if err != nil {
		assertNotAuthOrServerError(t, "dhl_express", err)
		return
	}
	assert.Equal(t, "dhl_express", resp.Carrier)
}

// testFedExSandbox probes the FedEx OAuth token endpoint on the sandbox base URL.
// A successful token confirms the auth URL, grant type, and credential format.
func testFedExSandbox(t *testing.T) {
	skipUnless(t, "FEDEX_CLIENT_ID", "FEDEX_CLIENT_SECRET")

	a := NewFedExAdapter(
		os.Getenv("FEDEX_CLIENT_ID"),
		os.Getenv("FEDEX_CLIENT_SECRET"),
		os.Getenv("FEDEX_ACCOUNT_NUMBER"),
		sandboxLogger(t),
	)
	a.BaseURL = "https://apis-sandbox.fedex.com"

	ctx, cancel := context.WithTimeout(t.Context(), probeTimeout)
	defer cancel()

	token, err := a.bearerToken(ctx)
	require.NoError(t, err, "FedEx sandbox auth probe failed")
	assert.NotEmpty(t, token, "FedEx sandbox returned empty access token")
}

// testEvriSandbox probes the Evri Classic API auth endpoint.
// A 401 means the credential format or token endpoint path changed.
func testEvriSandbox(t *testing.T) {
	skipUnless(t, "EVRI_CLIENT_ID", "EVRI_CLIENT_SECRET")

	a := NewEvriAdapter(
		os.Getenv("EVRI_CLIENT_ID"),
		os.Getenv("EVRI_CLIENT_SECRET"),
		sandboxLogger(t),
	)

	ctx, cancel := context.WithTimeout(t.Context(), probeTimeout)
	defer cancel()

	token, err := a.bearerToken(ctx)
	require.NoError(t, err, "Evri sandbox auth probe failed")
	assert.NotEmpty(t, token, "Evri sandbox returned empty access token")
}
