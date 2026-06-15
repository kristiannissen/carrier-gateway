// Package adapter provides interfaces and shared types for carrier integrations.
// This file is located at /internal/adapter/adapter.go.
package adapter

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// LabelFormat is the requested label output format.
type LabelFormat string

const (
	LabelFormatPDF   LabelFormat = "PDF"
	LabelFormatPNG   LabelFormat = "PNG"
	LabelFormatZPL   LabelFormat = "ZPL"
	LabelFormatEPL   LabelFormat = "EPL"
	LabelFormatZPLGK LabelFormat = "ZPLGK"
)

// LabelRequest specifies which label to fetch and in what format.
type LabelRequest struct {
	// TrackingNumber identifies the shipment whose label to fetch.
	TrackingNumber string
	// Format is the requested output format.
	Format LabelFormat
}

// LabelResponse contains the base64-encoded label data ready for printing.
type LabelResponse struct {
	TrackingNumber string      `json:"trackingNumber"`
	Carrier        string      `json:"carrier"`
	Format         LabelFormat `json:"format"`
	// Data is the base64-encoded label content.
	Data string `json:"data"`
	// MimeType describes the content (e.g. application/pdf, image/png).
	MimeType string `json:"mimeType"`
}

// mimeTypes maps label formats to their MIME type.
var mimeTypes = map[LabelFormat]string{
	LabelFormatPDF:   "application/pdf",
	LabelFormatPNG:   "image/png",
	LabelFormatZPL:   "application/x-zpl",
	LabelFormatEPL:   "application/x-epl",
	LabelFormatZPLGK: "application/x-zpl",
}

// MimeTypeForFormat returns the MIME type for the given label format.
func MimeTypeForFormat(f LabelFormat) string {
	if mt, ok := mimeTypes[f]; ok {
		return mt
	}
	return "application/octet-stream"
}

// CarrierAdapter defines the interface for all carrier adapters.
//
// Five methods are intentional: book, track, label, cancel, and update map to
// five distinct shipment lifecycle operations every carrier must express.
// Splitting into smaller role interfaces (e.g. Booker, Tracker) would scatter
// the constraint without reducing its footprint — every adapter implements all
// five and every handler selects a single adapter by name.
type CarrierAdapter interface {
	// BookShipment books a shipment with the carrier and returns a tracking number and label URL.
	BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error)

	// TrackShipment retrieves the tracking status for a shipment.
	TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error)

	// FetchLabel retrieves the label for a shipment in the requested format.
	// The label data is returned as base64-encoded bytes.
	FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error)

	// CancelShipment cancels a booked shipment before it is collected.
	// Returns an error if the carrier does not support cancellation or the
	// shipment has already been collected by the carrier.
	CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error)

	// UpdateShipment applies partial updates to a booked shipment.
	// Only non-zero fields in UpdateRequest are forwarded to the carrier.
	// Returns an error if the carrier does not support the requested update
	// or the shipment has already been scanned.
	UpdateShipment(ctx context.Context, req UpdateRequest) (*UpdateResponse, error)
}

// Registry holds the available CarrierAdapters and selects one by name.
// It is the single owner of the adapter map; nothing outside this type reads
// or writes the map directly.
type Registry struct {
	adapters map[string]CarrierAdapter
}

// NewRegistryFromMap creates a Registry from an already-initialised adapter
// map. Intended for tests that need precise control over which adapters are
// present without going through InitAdapters or touching env vars.
func NewRegistryFromMap(adapters map[string]CarrierAdapter) *Registry {
	return &Registry{adapters: adapters}
}

// NewRegistry initialises all carrier adapters and returns a Registry ready
// for use. It is the primary entry point for wiring adapters into the
// application; InitAdapters is retained for use in tests that need the raw
// map.
func NewRegistry(log *zap.Logger) *Registry {
	return &Registry{adapters: InitAdapters(log)}
}

// Select returns the CarrierAdapter registered under carrier.
// Returns an error if the carrier is not registered.
func (r *Registry) Select(carrier string) (CarrierAdapter, error) {
	a, ok := r.adapters[carrier]
	if !ok {
		return nil, fmt.Errorf("carrier %q is not supported", carrier)
	}
	return a, nil
}

// Carriers returns the names of all registered carriers in undefined order.
func (r *Registry) Carriers() []string {
	keys := make([]string, 0, len(r.adapters))
	for k := range r.adapters {
		keys = append(keys, k)
	}
	return keys
}

// CancelResponse is returned after a successful shipment cancellation.
type CancelResponse struct {
	TrackingNumber string `json:"trackingNumber"`
	Carrier        string `json:"carrier"`
	Status         string `json:"status"` // always "cancelled"
}

// UpdateRequest specifies which fields to update on a booked shipment.
// Only non-zero fields are forwarded to the carrier.
type UpdateRequest struct {
	Carrier        string `json:"carrier"`
	TrackingNumber string `json:"trackingNumber"`
	// ReceiverPhone updates the receiver's contact phone number.
	ReceiverPhone string `json:"phone,omitempty"`
	// ReceiverEmail updates the receiver's contact email address.
	ReceiverEmail string `json:"email,omitempty"`
	// Weight updates the parcel weight in kg.
	// DAO converts to grams internally. Must be set before first terminal scan.
	Weight float64 `json:"weight,omitempty"`
	// ServicePointID redirects delivery to a different service point.
	// DAO only — uses OpdaterShopid.php.
	ServicePointID string `json:"servicePointId,omitempty"`
}

// UpdateResponse is returned after a successful shipment update.
type UpdateResponse struct {
	TrackingNumber string `json:"trackingNumber"`
	Carrier        string `json:"carrier"`
	Status         string `json:"status"` // always "updated"
	// UpdatedFields lists the field names that were successfully updated.
	UpdatedFields []string `json:"updatedFields"`
}

// carrierCapabilities describes optional features a carrier's API supports natively.
type carrierCapabilities struct {
	// NativeIdempotency is true when the carrier's booking API accepts an
	// idempotency key directly and uses it for server-side deduplication.
	// When false, deduplication must be handled by the integrating system.
	NativeIdempotency bool
	// Beta is true when the carrier integration is not yet fully validated
	// for production use. Callers receive a warning in the booking response.
	Beta bool
	// Demo is true when the carrier integration is a placeholder only.
	// Demo adapters return mock data and are not connected to any live API.
	// They exist to satisfy the CarrierAdapter interface for future implementation.
	Demo bool
	// SupportsCancellation is true when the carrier's API supports cancelling
	// a booked shipment before collection.
	SupportsCancellation bool
	// SupportsUpdate is true when the carrier's API supports partial updates
	// to a booked shipment (contact, weight, service point).
	SupportsUpdate bool
	// SupportsReturnBooking is true when the carrier supports registering a
	// return against an already-delivered shipment via POST /api/returns.
	// The adapter must expose a BookReturn method callable directly from the handler.
	SupportsReturnBooking bool
	// SupportsEventPolling is true when the carrier exposes a cursor-based
	// tracking event stream. The adapter runs an internal background poller and
	// pushes events to the notification service; no public API change is needed.
	SupportsEventPolling bool
}

// capabilities maps carrier keys to their known API capabilities.
var capabilities = map[string]carrierCapabilities{
	"postnord": {NativeIdempotency: true, SupportsCancellation: true, SupportsUpdate: true},
	"bring":    {NativeIdempotency: false, SupportsCancellation: true, SupportsUpdate: false},
	"gls":      {NativeIdempotency: false, SupportsCancellation: true, SupportsUpdate: false},
	"dao":      {NativeIdempotency: false, Beta: false, SupportsCancellation: true, SupportsUpdate: true},
	"dhl":      {NativeIdempotency: false, Beta: true, SupportsCancellation: false, SupportsUpdate: false},
	"hermes":   {NativeIdempotency: false, Beta: true, SupportsCancellation: false, SupportsUpdate: false},
	"inpost":   {NativeIdempotency: false, Demo: true, SupportsCancellation: false, SupportsUpdate: false},
	"omniva": {
		NativeIdempotency:     false,
		SupportsCancellation:  true,
		SupportsUpdate:        true,
		SupportsReturnBooking: true,
		SupportsEventPolling:  true,
	},
	// FedEx: cancellation via PUT /ship/v1/shipments/cancel.
	// Pickup scheduling via POST /pickup/v1/pickups (Express/Ground).
	// Pickup availability via POST /pickup/v1/pickups/availabilities.
	// Pickup cancellation via PUT /pickup/v1/pickups/cancel. Update not supported — cancel-and-rebook.
	// Ground end-of-day manifest close via PUT /ship/v1/endofday/. Express does not require a close.
	// Customs declaration wired for international shipments. Service point delivery via HOLD_AT_LOCATION.
	"fedex": {NativeIdempotency: false, Beta: false, SupportsCancellation: true, SupportsUpdate: false},
	// DHL Express: cancel AWB is not available via API; pickup cancellation requires
	// the dispatchConfirmationNumber from BookingResponse, not the AWB.
	"dhl_express": {NativeIdempotency: false, Beta: true, SupportsCancellation: false, SupportsUpdate: false},
	// DHL eCommerce Americas: manifest close is supported via the async Manifest API v4.
	// Booking, tracking, and label retrieval are not yet implemented.
	"dhl_ecommerce": {NativeIdempotency: false, Beta: true, SupportsCancellation: false, SupportsUpdate: false},
	// DHL eCommerce UK: booking, label, tracking, and cancellation are supported.
	// No manifest API exists on the UK platform — CloseManifest returns ErrNotSupported.
	// Cancellation requires the consignee postal code, cached at BookShipment time.
	// Shipments booked outside this process cannot be cancelled via the API.
	"dhl_ecommerce_uk": {
		NativeIdempotency:    false,
		Beta:                 true,
		SupportsCancellation: true,
		SupportsUpdate:       false,
	},
	// DPD: capabilities are registered dynamically per country key ("dpd_lt", "dpd_at", …)
	// by registerDPD in InitAdapters. There is no static "dpd" entry — the key depends
	// on which DPD_{COUNTRY}_API_TOKEN env vars are present at startup.
	"dpd_uk": {
		NativeIdempotency:    false,
		Beta:                 true,
		SupportsCancellation: false, // cancellation endpoint not yet confirmed
		SupportsUpdate:       false,
	},
	// Evri (formerly Hermes UK): booking and label retrieval only.
	// The Evri Classic API exposes no tracking, cancellation, or update endpoint.
	// UK domestic only — all delivery addresses must be valid UK postcodes.
	"evri": {
		NativeIdempotency:    true, // clientUID used for server-side deduplication
		Beta:                 true,
		SupportsCancellation: false,
		SupportsUpdate:       false,
	},
}

// SupportsNativeIdempotency reports whether the given carrier accepts an
// idempotency key directly in its booking API. When false, the integrating
// system is responsible for deduplication using the key.
func SupportsNativeIdempotency(carrier string) bool {
	return capabilities[carrier].NativeIdempotency
}

// IsBeta reports whether the given carrier integration is in beta.
// Beta carriers are functional but not fully validated for production use.
func IsBeta(carrier string) bool {
	return capabilities[carrier].Beta
}

// IsDemo reports whether the given carrier is a demo placeholder.
// Demo carriers return mock data and are not connected to any live API.
func IsDemo(carrier string) bool {
	return capabilities[carrier].Demo
}

// SupportsCancellation reports whether the given carrier supports post-booking cancellation.
func SupportsCancellation(carrier string) bool {
	return capabilities[carrier].SupportsCancellation
}

// SupportsUpdate reports whether the given carrier supports partial post-booking updates.
func SupportsUpdate(carrier string) bool {
	return capabilities[carrier].SupportsUpdate
}

// InitAdapters initializes all carrier adapters based on environment variables.
func InitAdapters(log *zap.Logger) map[string]CarrierAdapter {
	adapters := make(map[string]CarrierAdapter)
	mockMode := os.Getenv("MOCK_MODE") == "true"

	postNordAPIKey := os.Getenv("POSTNORD_API_KEY")
	postNordCustomerNumber := os.Getenv("POSTNORD_CUSTOMER_NUMBER")
	postNordAppID := 0
	if v := os.Getenv("POSTNORD_APPLICATION_ID"); v != "" {
		if id, parseErr := strconv.Atoi(v); parseErr == nil {
			postNordAppID = id
		}
	}
	switch {
	case mockMode:
		adapters["postnord"] = &MockPostNordAdapter{}
		log.Info("PostNord adapter initialized in mock mode (MOCK_MODE=true)")
	case postNordAPIKey == "":
		adapters["postnord"] = &MockPostNordAdapter{}
		log.Warn("PostNord adapter falling back to mock mode (POSTNORD_API_KEY not set)")
	default:
		adapters["postnord"] = NewPostNordAdapter(postNordAPIKey, postNordCustomerNumber, postNordAppID, log)
		log.Info("PostNord adapter initialized in production mode")
	}

	bringAPIKey := os.Getenv("BRING_API_KEY")
	bringCustomerID := os.Getenv("BRING_CUSTOMER_ID")
	bringCustomerNumber := os.Getenv("BRING_CUSTOMER_NUMBER")
	bringCompanyName := os.Getenv("BRING_COMPANY_NAME")
	switch {
	case mockMode:
		adapters["bring"] = &MockBringAdapter{}
		log.Info("Bring adapter initialized in mock mode (MOCK_MODE=true)")
	case bringAPIKey == "" || bringCustomerID == "":
		adapters["bring"] = &MockBringAdapter{}
		log.Warn("Bring adapter falling back to mock mode (BRING_API_KEY or BRING_CUSTOMER_ID not set)")
	default:
		adapters["bring"] = NewBringAdapter(bringAPIKey, bringCustomerID, bringCustomerNumber, bringCompanyName, log)
		log.Info("Bring adapter initialized in production mode")
	}

	glsAPIKey := os.Getenv("GLS_API_KEY")
	glsClientSecret := os.Getenv("GLS_CLIENT_SECRET")
	contractID := os.Getenv("GLS_CONTRACT_ID")
	switch {
	case mockMode:
		adapters["gls"] = &MockGLSAdapter{}
		log.Info("GLS adapter initialized in mock mode (MOCK_MODE=true)")
	case glsAPIKey == "" || glsClientSecret == "":
		adapters["gls"] = &MockGLSAdapter{}
		log.Warn("GLS adapter falling back to mock mode (GLS_API_KEY or GLS_CLIENT_SECRET not set)")
	default:
		adapters["gls"] = NewGLSAdapter(glsAPIKey, glsClientSecret, contractID, log)
		log.Info("GLS adapter initialized in production mode")
	}

	daoCustomerID := os.Getenv("DAO_CUSTOMER_ID")
	daoAPIKey := os.Getenv("DAO_API_KEY")
	daoTestMode := os.Getenv("DAO_TEST_MODE") == "true"
	switch {
	case mockMode:
		adapters["dao"] = &MockDAOAdapter{}
		log.Info("DAO adapter initialized in mock mode (MOCK_MODE=true)")
	case daoCustomerID == "" || daoAPIKey == "":
		adapters["dao"] = &MockDAOAdapter{}
		log.Warn("DAO adapter falling back to mock mode (DAO_CUSTOMER_ID or DAO_API_KEY not set)")
	default:
		adapters["dao"] = NewDAOAdapter(daoCustomerID, daoAPIKey, daoTestMode, log)
		if daoTestMode {
			log.Info("DAO adapter initialized in test mode — test=1 on all requests")
		} else {
			log.Info("DAO adapter initialized in production mode")
		}
	}

	dhlClientID := os.Getenv("DHL_CLIENT_ID")
	dhlClientSecret := os.Getenv("DHL_CLIENT_SECRET")
	dhlCustomerID := os.Getenv("DHL_CUSTOMER_ID")
	dhlTrackingAPIKey := os.Getenv("DHL_TRACKING_API_KEY")
	switch {
	case mockMode:
		adapters["dhl"] = &MockDHLAdapter{}
		log.Info("DHL adapter initialized in mock mode (MOCK_MODE=true)")
	case dhlClientID == "" || dhlClientSecret == "":
		adapters["dhl"] = &MockDHLAdapter{}
		log.Warn("DHL adapter falling back to mock mode (DHL_CLIENT_ID or DHL_CLIENT_SECRET not set)")
	default:
		a := NewDHLAdapter(dhlClientID, dhlClientSecret, dhlCustomerID, dhlTrackingAPIKey, log)
		adapters["dhl"] = a
		log.Info("DHL adapter initialized in production mode (beta)",
			zap.String("bookingURL", a.BookingBaseURL),
			zap.String("trackingURL", a.TrackingBaseURL),
		)
	}

	inpostAPIKey := os.Getenv("INPOST_API_KEY")
	switch {
	case mockMode:
		adapters["inpost"] = &MockInPostAdapter{}
		log.Info("InPost adapter initialized in mock mode (MOCK_MODE=true)")
	case inpostAPIKey == "":
		adapters["inpost"] = &MockInPostAdapter{}
		log.Warn("InPost adapter falling back to mock mode (INPOST_API_KEY not set)")
	default:
		adapters["inpost"] = NewInPostAdapter(inpostAPIKey, log)
		log.Info("InPost adapter initialized in production mode")
	}

	hermesClientID := os.Getenv("HERMES_CLIENT_ID")
	hermesClientSecret := os.Getenv("HERMES_CLIENT_SECRET")
	switch {
	case mockMode:
		adapters["hermes"] = &MockHermesAdapter{}
		log.Info("Hermes adapter initialized in mock mode (MOCK_MODE=true)")
	case hermesClientID == "" || hermesClientSecret == "":
		adapters["hermes"] = &MockHermesAdapter{}
		log.Warn("Hermes adapter falling back to mock mode (HERMES_CLIENT_ID or HERMES_CLIENT_SECRET not set)")
	default:
		adapters["hermes"] = NewHermesAdapter(hermesClientID, hermesClientSecret, log)
		log.Info("Hermes adapter initialized in production mode (beta)")
	}

	fedexClientID := os.Getenv("FEDEX_CLIENT_ID")
	fedexClientSecret := os.Getenv("FEDEX_CLIENT_SECRET")
	fedexAccountNumber := os.Getenv("FEDEX_ACCOUNT_NUMBER")
	switch {
	case mockMode:
		adapters["fedex"] = &MockFedExAdapter{}
		log.Info("FedEx adapter initialized in mock mode (MOCK_MODE=true)")
	case fedexClientID == "" || fedexClientSecret == "":
		adapters["fedex"] = &MockFedExAdapter{}
		log.Warn("FedEx adapter falling back to mock mode (FEDEX_CLIENT_ID or FEDEX_CLIENT_SECRET not set)")
	default:
		a := NewFedExAdapter(fedexClientID, fedexClientSecret, fedexAccountNumber, log)
		adapters["fedex"] = a
		log.Info("FedEx adapter initialized",
			zap.String("baseURL", a.BaseURL),
		)
	}

	dhlExpressUsername := os.Getenv("DHL_EXPRESS_USERNAME")
	dhlExpressPassword := os.Getenv("DHL_EXPRESS_PASSWORD")
	dhlExpressAccountNumber := os.Getenv("DHL_EXPRESS_ACCOUNT_NUMBER")
	dhlExpressProductCode := os.Getenv("DHL_EXPRESS_PRODUCT_CODE")
	if dhlExpressProductCode == "" {
		dhlExpressProductCode = "P" // EXPRESS WORLDWIDE — override via DHL_EXPRESS_PRODUCT_CODE
	}
	dhlExpressReturnProductCode := os.Getenv("DHL_EXPRESS_RETURN_PRODUCT_CODE")
	switch {
	case mockMode:
		adapters["dhl_express"] = &MockDHLExpressAdapter{}
		log.Info("DHL Express adapter initialized in mock mode (MOCK_MODE=true)")
	case dhlExpressUsername == "" || dhlExpressPassword == "":
		adapters["dhl_express"] = &MockDHLExpressAdapter{}
		log.Warn("DHL Express adapter falling back to mock mode (DHL_EXPRESS_USERNAME or DHL_EXPRESS_PASSWORD not set)")
	default:
		a := NewDHLExpressAdapter(dhlExpressUsername, dhlExpressPassword, dhlExpressAccountNumber, dhlExpressProductCode, dhlExpressReturnProductCode, log)
		adapters["dhl_express"] = a
		log.Info("DHL Express adapter initialized in production mode (beta)",
			zap.String("baseURL", a.BaseURL),
			zap.String("defaultProductCode", a.DefaultProductCode),
		)
	}

	registerDPD(adapters, mockMode, log)
	registerGLSNL(adapters, mockMode, log)

	dpdUKUsername := os.Getenv("DPD_UK_USERNAME")
	dpdUKPassword := os.Getenv("DPD_UK_PASSWORD")
	dpdUKUserID := os.Getenv("DPD_UK_USER_ID")
	dpdUKNetworkCode := os.Getenv("DPD_UK_NETWORK_CODE")
	switch {
	case mockMode:
		adapters["dpd_uk"] = &MockDPDUKAdapter{}
		log.Info("DPD UK adapter initialized in mock mode (MOCK_MODE=true)")
	case dpdUKUsername == "" || dpdUKPassword == "" || dpdUKUserID == "":
		adapters["dpd_uk"] = &MockDPDUKAdapter{}
		log.Warn("DPD UK adapter falling back to mock mode (DPD_UK_USERNAME, DPD_UK_PASSWORD or DPD_UK_USER_ID not set)")
	default:
		adapters["dpd_uk"] = NewDPDUKAdapter(dpdUKUsername, dpdUKPassword, dpdUKUserID, dpdUKNetworkCode, log)
		log.Info("DPD UK adapter initialized in production mode (beta)",
			zap.String("networkCode", func() string {
				if dpdUKNetworkCode == "" {
					return "1^12 (default)"
				}
				return dpdUKNetworkCode
			}()),
		)
	}

	omnivaUsername := os.Getenv("OMNIVA_USERNAME")
	omnivaPassword := os.Getenv("OMNIVA_PASSWORD")
	omnivaCustomerCode := os.Getenv("OMNIVA_CUSTOMER_CODE")
	omnivaAgentID := os.Getenv("OMNIVA_AGENT_ID")
	switch {
	case mockMode:
		adapters["omniva"] = &MockOmnivaAdapter{}
		log.Info("Omniva adapter initialized in mock mode (MOCK_MODE=true)")
	case omnivaUsername == "" || omnivaPassword == "":
		adapters["omniva"] = &MockOmnivaAdapter{}
		log.Warn("Omniva adapter falling back to mock mode (OMNIVA_USERNAME or OMNIVA_PASSWORD not set)")
	default:
		adapters["omniva"] = NewOmnivaAdapter(omnivaUsername, omnivaPassword, omnivaCustomerCode, omnivaAgentID, log)
		log.Info("Omniva adapter initialized in production mode")
	}

	evriClientID := os.Getenv("EVRI_CLIENT_ID")
	evriClientSecret := os.Getenv("EVRI_CLIENT_SECRET")
	switch {
	case mockMode:
		adapters["evri"] = NewMockEvriAdapter()
		log.Info("Evri adapter initialized in mock mode (MOCK_MODE=true)")
	case evriClientID == "" || evriClientSecret == "":
		adapters["evri"] = NewMockEvriAdapter()
		log.Warn("Evri adapter falling back to mock mode (EVRI_CLIENT_ID or EVRI_CLIENT_SECRET not set)")
	default:
		adapters["evri"] = NewEvriAdapter(evriClientID, evriClientSecret, log)
		log.Info("Evri adapter initialized in production mode (beta — tracking/cancel/update not supported)")
	}

	dhlECSPickup := os.Getenv("DHLECS_PICKUP_ACCOUNT")
	dhlECSClientID := os.Getenv("DHLECS_CLIENT_ID")
	dhlECSClientSecret := os.Getenv("DHLECS_CLIENT_SECRET")
	switch {
	case mockMode:
		adapters["dhl_ecommerce"] = &MockDHLECSAdapter{}
		log.Info("DHL eCommerce Americas adapter initialized in mock mode (MOCK_MODE=true)")
	case dhlECSClientID == "" || dhlECSClientSecret == "" || dhlECSPickup == "":
		adapters["dhl_ecommerce"] = &MockDHLECSAdapter{}
		log.Warn("DHL eCommerce Americas adapter falling back to mock mode (DHLECS_CLIENT_ID, DHLECS_CLIENT_SECRET or DHLECS_PICKUP_ACCOUNT not set)")
	default:
		a := NewDHLECSAdapter(dhlECSPickup, dhlECSClientID, dhlECSClientSecret, log)
		adapters["dhl_ecommerce"] = a
		log.Info("DHL eCommerce Americas adapter initialized in production mode (beta)",
			zap.String("baseURL", a.BaseURL),
			zap.String("pickup", a.PickupAccountNumber),
		)
	}

	dhlUKPickup := os.Getenv("DHLECS_UK_PICKUP_ACCOUNT")
	dhlUKClientID := os.Getenv("DHLECS_UK_CLIENT_ID")
	dhlUKClientSecret := os.Getenv("DHLECS_UK_CLIENT_SECRET")
	dhlUKProductCode := os.Getenv("DHLECS_UK_ORDERED_PRODUCT")
	if dhlUKProductCode == "" {
		dhlUKProductCode = "220" // Signature At Address Next Day — override via DHLECS_UK_ORDERED_PRODUCT
	}
	dhlUKTradingLocationID := os.Getenv("DHLECS_UK_TRADING_LOCATION_ID")
	switch {
	case mockMode:
		adapters["dhl_ecommerce_uk"] = &MockDHLEcomUKAdapter{}
		log.Info("DHL eCommerce UK adapter initialized in mock mode (MOCK_MODE=true)")
	case dhlUKClientID == "" || dhlUKClientSecret == "" || dhlUKPickup == "":
		adapters["dhl_ecommerce_uk"] = &MockDHLEcomUKAdapter{}
		log.Warn("DHL eCommerce UK adapter falling back to mock mode (DHLECS_UK_CLIENT_ID, DHLECS_UK_CLIENT_SECRET or DHLECS_UK_PICKUP_ACCOUNT not set)")
	default:
		a := NewDHLEcomUKAdapter(dhlUKPickup, dhlUKTradingLocationID, dhlUKClientID, dhlUKClientSecret, dhlUKProductCode, log)
		a.ReturnAccount = os.Getenv("DHLECS_UK_RETURN_ACCOUNT")
		a.ReturnProductCode = os.Getenv("DHLECS_UK_RETURN_PRODUCT")
		log.Info("DHL eCommerce UK adapter initialized in production mode (beta)",
			zap.String("baseURL", a.BaseURL),
			zap.String("pickupAccount", a.PickupAccount),
			zap.String("defaultProductCode", a.DefaultProductCode),
		)
		adapters["dhl_ecommerce_uk"] = a
	}

	return adapters
}

// registerDPD scans environment variables for DPD_{COUNTRY}_API_TOKEN and
// DPD_{COUNTRY}_BASE_URL pairs and registers one adapter per configured country
// under the key "dpd_{country}" (e.g. "dpd_at", "dpd_lt", "dpd_be").
//
// This factory approach means adding a new DPD country requires only new env
// vars — no code change. It follows the same simplicity principle as the other
// single-entity carrier blocks above, but handles the multi-region case without
// hardcoding country keys.
//
// Example env vars:
//
//	DPD_LT_API_TOKEN=<token>  DPD_LT_BASE_URL=https://esiunta.dpd.lt/api/v1
//	DPD_AT_API_TOKEN=<token>  DPD_AT_BASE_URL=https://...dpd.at/api/v1
func registerDPD(adapters map[string]CarrierAdapter, mockMode bool, log *zap.Logger) {
	seen := make(map[string]bool)
	for _, env := range os.Environ() {
		// Match DPD_{COUNTRY}_API_TOKEN.
		if !strings.HasPrefix(env, "DPD_") || !strings.Contains(env, "_API_TOKEN=") {
			continue
		}
		// env is e.g. "DPD_LT_API_TOKEN=abc123"
		eqIdx := strings.Index(env, "=")
		key := env[:eqIdx]   // "DPD_LT_API_TOKEN"
		token := env[eqIdx+1:] // "abc123"

		// Extract country from "DPD_{COUNTRY}_API_TOKEN".
		parts := strings.SplitN(key, "_", 3) // ["DPD", "LT", "API_TOKEN"]
		if len(parts) != 3 || parts[2] != "API_TOKEN" {
			continue
		}
		country := strings.ToLower(parts[1]) // "lt"
		carrierKey := "dpd_" + country       // "dpd_lt"

		if seen[carrierKey] {
			continue
		}
		seen[carrierKey] = true

		if mockMode {
			adapters[carrierKey] = NewMockDPDAdapter()
			log.Info("DPD adapter initialized in mock mode",
				zap.String("carrier", carrierKey),
			)
			continue
		}

		if token == "" {
			adapters[carrierKey] = NewMockDPDAdapter()
			log.Warn("DPD adapter falling back to mock mode (token empty)",
				zap.String("carrier", carrierKey),
			)
			continue
		}

		baseURL := os.Getenv("DPD_" + strings.ToUpper(country) + "_BASE_URL")
		if baseURL == "" {
			adapters[carrierKey] = NewMockDPDAdapter()
			log.Warn("DPD adapter falling back to mock mode (BASE_URL not set)",
				zap.String("carrier", carrierKey),
				zap.String("missing", "DPD_"+strings.ToUpper(country)+"_BASE_URL"),
			)
			continue
		}

		adapters[carrierKey] = NewDPDAdapter(token, baseURL, log)
		// Register capabilities for this country key at runtime.
		capabilities[carrierKey] = carrierCapabilities{
			NativeIdempotency:    false,
			Beta:                 true,
			SupportsCancellation: true,
			SupportsUpdate:       false,
		}
		log.Info("DPD adapter initialized in production mode (beta)",
			zap.String("carrier", carrierKey),
			zap.String("baseURL", baseURL),
		)
	}
}

// registerGLSNL scans environment variables for GLS_{CC}_USERNAME where CC is
// exactly a 2-letter ISO country code and registers one GLSNLAdapter per country
// under the key "gls_{cc}" (e.g. "gls_nl", "gls_be").
//
// This is the factory for GLS national subsidiaries that use username/password
// authentication (api-portal.gls.nl and compatible portals). It is distinct from
// the unified GLS Group OAuth2 adapter registered under the key "gls".
//
// Adding a new GLS country requires only new env vars — no code change:
//
//	GLS_NL_USERNAME=...  GLS_NL_PASSWORD=...  GLS_NL_BASE_URL=https://api.mygls.nl
//	GLS_BE_USERNAME=...  GLS_BE_PASSWORD=...  GLS_BE_BASE_URL=https://api.mygls.be
func registerGLSNL(adapters map[string]CarrierAdapter, mockMode bool, log *zap.Logger) {
	seen := make(map[string]bool)
	for _, env := range os.Environ() {
		// Match GLS_{CC}_USERNAME= where CC is exactly 2 uppercase letters.
		// The 2-char constraint prevents collision with GLS_API_KEY, GLS_CLIENT_SECRET, etc.
		if !strings.HasPrefix(env, "GLS_") || !strings.Contains(env, "_USERNAME=") {
			continue
		}
		eqIdx := strings.Index(env, "=")
		key := env[:eqIdx]      // e.g. "GLS_NL_USERNAME"
		username := env[eqIdx+1:] // e.g. "myuser"

		// key must be "GLS_{CC}_USERNAME" with exactly a 2-letter country code.
		parts := strings.SplitN(key, "_", 3) // ["GLS", "NL", "USERNAME"]
		if len(parts) != 3 || parts[2] != "USERNAME" || len(parts[1]) != 2 {
			continue
		}
		country := parts[1]                  // "NL"
		carrierKey := "gls_" + strings.ToLower(country) // "gls_nl"

		if seen[carrierKey] {
			continue
		}
		seen[carrierKey] = true

		if mockMode {
			adapters[carrierKey] = NewMockGLSNLAdapter()
			log.Info("GLS NL-style adapter initialized in mock mode",
				zap.String("carrier", carrierKey),
			)
			continue
		}

		if username == "" {
			adapters[carrierKey] = NewMockGLSNLAdapter()
			log.Warn("GLS NL-style adapter falling back to mock (username empty)",
				zap.String("carrier", carrierKey),
			)
			continue
		}

		password := os.Getenv("GLS_" + country + "_PASSWORD")
		if password == "" {
			adapters[carrierKey] = NewMockGLSNLAdapter()
			log.Warn("GLS NL-style adapter falling back to mock (password not set)",
				zap.String("carrier", carrierKey),
				zap.String("missing", "GLS_"+country+"_PASSWORD"),
			)
			continue
		}

		baseURL := os.Getenv("GLS_" + country + "_BASE_URL")
		if baseURL == "" {
			adapters[carrierKey] = NewMockGLSNLAdapter()
			log.Warn("GLS NL-style adapter falling back to mock (base URL not set)",
				zap.String("carrier", carrierKey),
				zap.String("missing", "GLS_"+country+"_BASE_URL"),
			)
			continue
		}

		adapters[carrierKey] = NewGLSNLAdapter(username, password, baseURL, strings.ToLower(country), log)
		capabilities[carrierKey] = carrierCapabilities{
			NativeIdempotency:    false,
			Beta:                 true,
			SupportsCancellation: true,
			SupportsUpdate:       false,
		}
		log.Info("GLS NL-style adapter initialized in production mode (beta)",
			zap.String("carrier", carrierKey),
			zap.String("baseURL", baseURL),
		)
	}
}

// Customs holds cross-border declaration data required for non-EU
// destinations and EU B2B shipments above the de minimis threshold.
// It is optional for domestic and intra-EU B2C shipments below 150 EUR.
type Customs struct {
	// Incoterms is the trade term (e.g. DDP, DAP). Required for non-EU destinations.
	Incoterms string `json:"incoterms,omitempty"`

	// TransportMode is the mode of transport for the shipment.
	// Accepted values: "sea", "air", "road", "rail".
	// Some Incoterms are only valid for sea transport (FOB, FAS, CFR, CIF).
	TransportMode string `json:"transportMode,omitempty"`

	// HSCode is the 6-10 digit Harmonized System commodity code.
	// Required for non-EU destinations and EU shipments above de minimis.
	HSCode string `json:"hsCode,omitempty"`

	// CountryOfOrigin is the ISO 3166-1 alpha-2 country code where the goods
	// were manufactured or substantially transformed. Distinct from the
	// sender's address country and used for rules of origin checks.
	CountryOfOrigin string `json:"countryOfOrigin,omitempty"`

	// CustomsValue is the declared value of the shipment for customs purposes.
	CustomsValue float64 `json:"customsValue,omitempty"`

	// CustomsCurrency is the ISO 4217 currency code for CustomsValue (e.g. DKK, EUR, NOK).
	CustomsCurrency string `json:"customsCurrency,omitempty"`

	// ImporterOfRecord is the VAT or EORI number of the importer.
	// Required for non-EU destinations (e.g. Norway).
	ImporterOfRecord string `json:"importerOfRecord,omitempty"`

	// ImporterVATNumber is the VAT registration number of the receiver.
	// Required for EU B2B shipments.
	ImporterVATNumber string `json:"importerVatNumber,omitempty"`

	// ExporterVATNumber is the VAT registration number of the sender.
	// Required for non-EU destinations.
	ExporterVATNumber string `json:"exporterVatNumber,omitempty"`

	// ShipmentType is either "B2B" or "B2C". Affects VAT and de minimis rules.
	ShipmentType string `json:"shipmentType,omitempty"`

	// NatureOfCargo describes the commercial nature of the goods.
	// Required by Bring for international shipments.
	// Accepted values: "SALE_OF_GOODS", "GIFT", "RETURNED_GOODS", "COMMERCIAL_SAMPLE", "DOCUMENTS", "OTHER".
	// When empty and ShipmentType is "B2B" or "B2C", Bring defaults to "SALE_OF_GOODS".
	NatureOfCargo string `json:"natureOfCargo,omitempty"`

	// InvoiceNumber is the commercial invoice number. Required by DHL Express
	// for all customs-declarable shipments.
	InvoiceNumber string `json:"invoiceNumber,omitempty"`

	// InvoiceDate is the commercial invoice date in YYYY-MM-DD format. Required
	// by DHL Express — drives exchange rate calculation. Defaults to today when empty.
	InvoiceDate string `json:"invoiceDate,omitempty"`

	// IossNumber is the EU Import One Stop Shop (IOSS) VAT registration number.
	// Required for EU B2C shipments where VAT is collected at point of sale.
	// DHL Express maps this to registrationNumbers typeCode "SDT" on the importer.
	IossNumber string `json:"iossNumber,omitempty"`

	// GoodsCategoryCode classifies the shipment contents for customs.
	// Accepted values: "SALE_OF_GOODS", "GIFT", "COMMERCIAL_SAMPLE", "DOCUMENTS",
	// "RETURNED_GOODS", "OTHER". Required by Omniva for non-EU destinations.
	// NB: Omniva does not permit GIFT for US destinations via OMX.
	// When empty and NatureOfCargo is set, adapters may derive a compatible value.
	GoodsCategoryCode string `json:"goodsCategoryCode,omitempty"`
	// CategoryExplanation is required when GoodsCategoryCode is "OTHER" (max 40 chars).
	// Omniva maps this to customs.categoryExplanation.
	CategoryExplanation string `json:"categoryExplanation,omitempty"`
	// LicenceNumber is the export licence reference (max 21 chars).
	// Omniva maps this to customs.licenceNumber.
	LicenceNumber string `json:"licenceNumber,omitempty"`
	// CertificateNumber is a phytosanitary or other certificate reference (max 21 chars).
	// Omniva maps this to customs.certificateNumber.
	CertificateNumber string `json:"certificateNumber,omitempty"`
	// SenderCustomsReference is the sender's own customs reference number (max 21 chars).
	// Omniva maps this to customs.senderCustomsReference.
	SenderCustomsReference string `json:"senderCustomsReference,omitempty"`
	// ImportersReference is the importer's customs reference number (max 21 chars).
	// Omniva maps this to customs.importersReference.
	ImportersReference string `json:"importersReference,omitempty"`
	// Items holds the line-item breakdown required for full customs declarations.
	// Required for non-EU destinations; each item maps to one commodity in the
	// carrier's customs API (DHL cCustoms item, GLS lineItem, PostNord item, etc.).
	Items []CustomsItem `json:"items,omitempty"`
}

// CustomsItem describes a single line item within a customs declaration.
// It maps to the commodity/item level of carrier customs APIs.
type CustomsItem struct {
	// Description is a plain-language description of the goods (required).
	Description string `json:"description"`
	// HSCode is the 6-10 digit Harmonized System commodity code (required for
	// non-EU destinations).
	HSCode string `json:"hsCode,omitempty"`
	// CountryOfOrigin is the ISO 3166-1 alpha-2 code where the goods were
	// manufactured or substantially transformed.
	CountryOfOrigin string `json:"countryOfOrigin,omitempty"`
	// Quantity is the number of units of this line item.
	Quantity int `json:"quantity"`
	// NetWeight is the net weight in kg for this line item (excluding packaging).
	NetWeight float64 `json:"netWeight,omitempty"`
	// Value is the declared customs value for this line item.
	Value float64 `json:"value"`
	// Currency is the ISO 4217 currency code for Value (e.g. "EUR", "DKK").
	Currency string `json:"currency,omitempty"`
}

// NotificationPreferences holds the integrator-supplied webhook configuration
// for a booking. Kept in the adapter package to avoid an import cycle between
// adapter and notification.
type NotificationPreferences struct {
	// WebhookURL is the endpoint that receives shipment event payloads.
	WebhookURL string `json:"webhookUrl"`
	// WebhookSecret is used to sign the payload with HMAC-SHA256.
	// Leave empty to skip signing.
	WebhookSecret string `json:"webhookSecret,omitempty"`
	// Events filters which events trigger a dispatch.
	// An empty slice means all events are dispatched.
	Events []string `json:"events,omitempty"`
}

// BookingRequest represents a generic shipment booking request.
type BookingRequest struct {
	Carrier        string   `json:"carrier"        validate:"required"`
	Shipment       Shipment `json:"shipment"       validate:"required"`
	CallbackURL    string   `json:"callbackUrl,omitempty"`
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
	// LabelFormat specifies the desired label output format for the booking response.
	// When empty the carrier adapter picks its default (typically PDF).
	// Supported values depend on the carrier — see each adapter's feature mapping.
	LabelFormat LabelFormat `json:"labelFormat,omitempty"`
	// Notifications configures optional event-driven webhook dispatch.
	// When set, the gateway fires a "booked" notification after a successful booking.
	Notifications *NotificationPreferences `json:"notifications,omitempty"`
}

// AddOnType identifies a carrier-agnostic optional service.
type AddOnType string

const (
	// AddOnSMSNotification sends an SMS to the receiver when the shipment is ready.
	AddOnSMSNotification AddOnType = "sms_notification"
	// AddOnEmailNotification sends an email to the receiver when the shipment is ready.
	AddOnEmailNotification AddOnType = "email_notification"
	// AddOnFlexDelivery allows delivery without the receiver being present.
	// Use Instructions to specify where to leave the parcel.
	AddOnFlexDelivery AddOnType = "flex_delivery"
	// AddOnSignatureRequired requires a recipient signature on delivery.
	// Bring maps this to VAS 1131 (direct signature).
	// PostNord maps this to additionalServiceCode "A2".
	// GLS maps this to the DirectSignature service.
	AddOnSignatureRequired AddOnType = "signature_required"
	// AddOnCashOnDelivery collects payment from the receiver on delivery.
	// Set CODAmount, CODCurrency, and CODAccountNumber on the AddOn.
	// Currently supported on Bring only (VAS 1000).
	AddOnCashOnDelivery AddOnType = "cash_on_delivery"
	// AddOnInsurance declares an insured value for the shipment.
	// Set InsuranceValue and InsuranceCurrency on the AddOn.
	// Currently supported on PostNord only (additionalServiceCode "A8").
	AddOnInsurance AddOnType = "insurance"
	// AddOnDeliveryToSpecificPerson requires the carrier to deliver only to a named
	// individual. Set PersonalCode on the AddOn when the channel is PARCEL_MACHINE.
	// Omniva maps this to addService DELIVERY_TO_A_SPECIFIC_PERSON.
	AddOnDeliveryToSpecificPerson AddOnType = "delivery_to_specific_person"
	// AddOnDeliveryToPrivatePerson signals that the receiver is a private individual.
	// Omniva requires contact mobile or email when this add-on is active.
	// Omniva maps this to addService DELIVERY_TO_PRIVATE_PERSON.
	AddOnDeliveryToPrivatePerson AddOnType = "delivery_to_private_person"
	// AddOnFragile marks the parcel as fragile. Allowed inside Omniva consolidated
	// shipments alongside COD, DOCUMENT_RETURN, or MULTIPLE_PARCELS_DELIVERY_TOGETHER.
	// Omniva maps this to addService FRAGILE.
	AddOnFragile AddOnType = "fragile"
	// AddOnDocumentReturn instructs the carrier to return a signed document
	// to the sender after delivery.
	// Omniva maps this to addService DOCUMENT_RETURN.
	AddOnDocumentReturn AddOnType = "document_return"
	// AddOnMultiParcelTogether instructs the carrier to keep multiple parcels
	// in a consolidated shipment together during delivery.
	// Omniva maps this to addService MULTIPLE_PARCELS_DELIVERY_TOGETHER.
	AddOnMultiParcelTogether AddOnType = "multi_parcel_together"
)

// AddOn represents an optional service attached to a shipment.
// Phone and email for notifications are read from Receiver.Phone and Receiver.Email.
type AddOn struct {
	Type AddOnType `json:"type"`
	// Instructions is used for flex delivery — e.g. "Leave behind the green shed".
	Instructions string `json:"instructions,omitempty"`
	// CODAmount is the amount to collect on delivery. Required for cash_on_delivery.
	CODAmount float64 `json:"codAmount,omitempty"`
	// CODCurrency is the ISO 4217 currency code for COD (e.g. "DKK", "NOK"). Required for cash_on_delivery.
	CODCurrency string `json:"codCurrency,omitempty"`
	// CODAccountNumber is the bank account number to transfer the collected amount to.
	// Required for Bring COD (VAS 1000). Must be IBAN for Omniva COD.
	CODAccountNumber string `json:"codAccountNumber,omitempty"`
	// CODReceiver is the name of the person who receives the COD payment.
	// Omniva-specific: maps to addService param COD_RECEIVER.
	CODReceiver string `json:"codReceiver,omitempty"`
	// CODReferenceNo is the payment reference number appended to the COD transfer.
	// Omniva-specific: validated against EE bank rules when CODAccountNumber is an
	// Estonian IBAN. Maps to addService param COD_REFERENCE_NO.
	CODReferenceNo string `json:"codReferenceNo,omitempty"`
	// CODBic is the BIC/SWIFT code for the COD bank account.
	// Required for DHL eConnect SEPA COD.
	CODBic string `json:"codBic,omitempty"`
	// InsuranceValue is the declared value of the shipment for insurance purposes.
	// Required for insurance add-on.
	InsuranceValue float64 `json:"insuranceValue,omitempty"`
	// InsuranceCurrency is the ISO 4217 currency code for the insured value (e.g. "DKK").
	// Required for insurance add-on.
	InsuranceCurrency string `json:"insuranceCurrency,omitempty"`
	// PersonalCode is the national identification code of the intended recipient.
	// Omniva: required for delivery_to_specific_person when deliveryChannel is PARCEL_MACHINE.
	PersonalCode string `json:"personalCode,omitempty"`
}

// Shipment represents the shipment details.
type Shipment struct {
	Sender      Address `json:"sender"      validate:"required"`
	Receiver    Address `json:"receiver"    validate:"required"`
	TotalWeight float64 `json:"totalWeight" validate:"required,gt=0"`
	Colli       []Colli `json:"colli"       validate:"required,min=1"`
	// DeliveryType controls the shipping product. When empty the adapter
	// selects a sensible default based on whether ServicePointID is set.
	// Accepted values: "home", "business", "servicepoint", "return".
	DeliveryType string `json:"deliveryType,omitempty"`
	// ReturnFunctionality controls the return label type for carriers that
	// support it. Only used when DeliveryType is "return".
	// Accepted values: "standard" (customer prints label at service point),
	// "labelless" (customer writes code on package).
	// Defaults to "standard" when empty.
	ReturnFunctionality string `json:"returnFunctionality,omitempty"`
	// ServiceTier selects the product/speed tier within the delivery type.
	// Omniva parcels: "economy", "standard", "premium".
	// Omniva letters: "procedural_document", "registered_letter", "registered_maxiletter".
	// Other carriers may map this to their own tier codes.
	ServiceTier string `json:"serviceTier,omitempty"`
	// PaidByReceiver instructs the carrier to bill the receiver rather than the sender.
	// Omniva: valid for PARCEL and PALLET main services.
	PaidByReceiver bool `json:"paidByReceiver,omitempty"`
	// ShipmentComment is a free-text delivery instruction passed to the carrier (max 128 chars).
	// Omniva maps this to shipmentComment.
	ShipmentComment string `json:"shipmentComment,omitempty"`
	// AddOns lists optional services to attach to the shipment.
	// Each adapter maps these to its own wire format.
	AddOns []AddOn `json:"addOns,omitempty"`
	// Customs holds cross-border declaration data. Required for non-EU
	// destinations and EU B2B shipments above the de minimis threshold.
	Customs Customs `json:"customs,omitempty"`
}

// Colli represents an individual package in a shipment.
type Colli struct {
	ID         string     `json:"id" validate:"required"`
	Reference  string     `json:"reference,omitempty"`
	Weight     float64    `json:"weight" validate:"gt=0"`
	Dimensions Dimensions `json:"dimensions"`
	Items      []Item     `json:"items" validate:"required,min=1"`
}

// Dimensions represents the physical dimensions of a package in centimetres.
type Dimensions struct {
	Length float64 `json:"length"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Address represents a physical address.
//
// Street holds the street name only — no house number. HouseNumber is kept
// as a separate field because some carriers (InPost, GLS) require them
// distinct on the wire. Adapters that do not distinguish concatenate
// Street + " " + HouseNumber when building the payload.
//
// Supplement carries anything that does not fit on the street line:
// building name, floor, apartment, attention line, or care-of.
//
// State holds the state, province, or region code where required by the
// destination country (e.g. "CA" for California, "ON" for Ontario).
//
// ServicePointID identifies a carrier service point (parcel shop, pickup
// point, locker) for receiver addresses. When set, Street, City, and
// PostalCode are optional for the receiver — the carrier routes to the
// service point directly. Each carrier maps this to its own wire field name:
//
//   - PostNord: servicePointId
//   - Bring: pickupPointId
//   - GLS: parcelShopId
//   - DAO: lockerId
//   - InPost: targetLocker (service block)
//   - DHL Express: onDemandDelivery.servicePointId (6-char code, e.g. "BRU001")
type Address struct {
	Name           string `json:"name"           validate:"required"`
	Street         string `json:"street"         validate:"required"`
	HouseNumber    string `json:"houseNumber,omitempty"`
	Supplement     string `json:"supplement,omitempty"`
	City           string `json:"city"           validate:"required"`
	PostalCode     string `json:"postalCode"     validate:"required"`
	Country        string `json:"country"        validate:"required"`
	State          string `json:"state,omitempty"`
	ServicePointID string `json:"servicePointId,omitempty"`
	Phone          string `json:"phone,omitempty"`
	Email          string `json:"email,omitempty"`
	// AltName overrides the carrier account name on the printed label and in
	// notification messages. Omniva maps this to senderAddressee.altName.
	// Only meaningful on sender addresses.
	AltName string `json:"altName,omitempty"`
	// UseAddressForReturn instructs the carrier to return undeliverable parcels
	// to this address rather than the pre-configured handover location.
	// Omniva maps this to senderAddressee.useSenderAddressForReturn.
	UseAddressForReturn bool `json:"useAddressForReturn,omitempty"`
}

// Item represents an item in a colli.
type Item struct {
	Description string  `json:"description"`
	Weight      float64 `json:"weight"`
	Quantity    int     `json:"quantity"`
	Value       float64 `json:"value,omitempty"`
	SKU         string  `json:"sku,omitempty"`
}

// BookingResponse represents the response from a carrier after booking a shipment.
type BookingResponse struct {
	ShipmentID     string          `json:"shipmentId,omitempty"`
	TrackingNumber string          `json:"trackingNumber"`
	LabelURL       string          `json:"labelUrl,omitempty"`
	Carrier        string          `json:"carrier"`
	Cost           float64         `json:"cost,omitempty"`
	Currency       string          `json:"currency,omitempty"`
	ServiceLevel   string          `json:"serviceLevel,omitempty"`
	Status         string          `json:"status,omitempty"`
	Colli          []ColliResponse `json:"colli,omitempty"`
	Errors         []string        `json:"errors,omitempty"`
	LockerId       string          `json:"lockerId,omitempty"`
	ServicePointID string          `json:"servicePointId,omitempty"`
	// FlaggedForReview is true when the address passed a ReviewRequired
	// validation — the booking was accepted but should be checked manually.
	FlaggedForReview bool `json:"flaggedForReview,omitempty"`
	// BetaWarning is set when the carrier integration is in beta.
	BetaWarning string `json:"betaWarning,omitempty"`
	// AddOnWarnings lists add-ons that were requested but could not be fully
	// applied after a successful booking. The shipment is booked and has a
	// tracking number, but these services are not active.
	// Retry the failed add-ons via PATCH /api/bookings/{trackingNumber}?carrier=.
	AddOnWarnings []string `json:"addOnWarnings,omitempty"`
	// CustomsWarnings lists customs fields that were validated but could not be
	// forwarded to the carrier API because the carrier's wire format does not
	// yet support them. The shipment is booked; customs data must be submitted
	// manually or via the carrier's own portal.
	CustomsWarnings []string `json:"customsWarnings,omitempty"`
	// CNFormType is "CN22" or "CN23" indicating the type of customs declaration
	// form generated for this shipment. Empty when no form was generated (e.g.
	// intra-EU shipments below de minimis).
	CNFormType string `json:"cnFormType,omitempty"`
	// CNDocument is the base64-encoded plain-text CN22 or CN23 customs
	// declaration form generated on-demand at booking time. Decode and print
	// for physical attachment to the parcel, or submit via the carrier portal.
	CNDocument string `json:"cnDocument,omitempty"`
	// NotificationsSent lists notifications that were successfully dispatched
	// at booking time. Callers may store this for auditing.
	NotificationsSent []NotificationRecord `json:"notificationsSent,omitempty"`
	// NotificationsFailed lists notifications that failed at booking time.
	// Callers should store these and retry via POST /api/notifications.
	NotificationsFailed []NotificationRecord `json:"notificationsFailed,omitempty"`
	// DispatchConfirmationNumber is the pickup booking reference returned by
	// DHL Express when pickup.isRequested is true. Use with
	// DELETE /pickups/{dispatchConfirmationNumber} to cancel the pickup
	// independently of the shipment AWB.
	DispatchConfirmationNumber string `json:"dispatchConfirmationNumber,omitempty"`
}

// NotificationRecord describes the outcome of a single notification dispatch.
// Mirrors notification.Record but lives here to keep the adapter package
// free of a dependency on the notification package.
type NotificationRecord struct {
	Event     string `json:"event"`
	Channel   string `json:"channel"`
	URL       string `json:"url"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

// ColliResponse represents the response for an individual colli in a shipment.
type ColliResponse struct {
	ID             string `json:"id"`
	Reference      string `json:"reference,omitempty"`
	TrackingNumber string `json:"trackingNumber,omitempty"`
	LabelURL       string `json:"labelUrl,omitempty"`
	Status         string `json:"status,omitempty"`
}

// TrackingStatus is a carrier-agnostic normalized shipment status.
type TrackingStatus string

const (
	// StatusBooked means the shipment has been booked but not yet collected.
	StatusBooked TrackingStatus = "booked"
	// StatusPickedUp means the carrier has collected the parcel from the sender.
	StatusPickedUp TrackingStatus = "picked_up"
	// StatusInTransit means the parcel is moving through the carrier network.
	StatusInTransit TrackingStatus = "in_transit"
	// StatusOutForDelivery means the parcel is on the delivery vehicle.
	StatusOutForDelivery TrackingStatus = "out_for_delivery"
	// StatusDelivered means the parcel has been delivered to the recipient.
	StatusDelivered TrackingStatus = "delivered"
	// StatusFailed means a delivery attempt failed.
	StatusFailed TrackingStatus = "failed"
	// StatusReturned means the parcel is being returned to the sender.
	StatusReturned TrackingStatus = "returned"
	// StatusDelayed means the parcel is delayed relative to the original ETA.
	StatusDelayed TrackingStatus = "delayed"
	// StatusUnknown is the fallback for any raw status not in the mapping table.
	StatusUnknown TrackingStatus = "unknown"
)

// ProofOfDelivery contains electronic proof of delivery details returned by
// carriers that support ePOD (e.g. DHL Express and DHL Freight).
// Fields are omitted when the carrier has not yet recorded delivery confirmation.
type ProofOfDelivery struct {
	// DocumentURL is a link to the ePOD document (e.g. https://webpod.dhl.com/pod?token=...).
	DocumentURL string `json:"documentUrl,omitempty"`
	// SignatureURL is a link to the digital signature image.
	SignatureURL string `json:"signatureUrl,omitempty"`
	// SignedBy is the name of the person who signed for the shipment.
	SignedBy string `json:"signedBy,omitempty"`
	// Timestamp is the date and time the POD was recorded (ISO 8601).
	Timestamp string `json:"timestamp,omitempty"`
}

// TrackingResponse represents the tracking status of a shipment.
type TrackingResponse struct {
	ShipmentID     string `json:"shipmentId,omitempty"`
	TrackingNumber string `json:"trackingNumber"`
	Carrier        string `json:"carrier"`
	// Status is the raw carrier-specific status string. Preserved for backward compatibility.
	Status string `json:"status"`
	// NormalizedStatus is the carrier-agnostic status. Always present.
	NormalizedStatus TrackingStatus `json:"normalizedStatus"`
	// OriginalStatus is the unmodified raw status string from the carrier.
	OriginalStatus    string           `json:"originalStatus"`
	Events            []TrackingEvent  `json:"events"`
	EstimatedDelivery string           `json:"estimatedDelivery,omitempty"`
	Colli             []ColliTracking  `json:"colli,omitempty"`
	// ProofOfDelivery is populated when the carrier has recorded ePOD.
	// Currently surfaced for DHL Express and DHL Freight.
	ProofOfDelivery *ProofOfDelivery `json:"proofOfDelivery,omitempty"`
	// NotificationsSent lists notifications successfully dispatched during this tracking call.
	NotificationsSent []NotificationRecord `json:"notificationsSent,omitempty"`
	// NotificationsFailed lists notifications that failed during this tracking call.
	// Callers should retry via POST /api/notifications.
	NotificationsFailed []NotificationRecord `json:"notificationsFailed,omitempty"`
}

// ColliTracking represents the tracking status of an individual colli.
type ColliTracking struct {
	ID             string          `json:"id"`
	Reference      string          `json:"reference,omitempty"`
	TrackingNumber string          `json:"trackingNumber,omitempty"`
	Status         string          `json:"status"`
	Events         []TrackingEvent `json:"events"`
}

// TrackingEvent represents a single tracking event.
type TrackingEvent struct {
	Timestamp string `json:"timestamp"`
	// Status is the raw carrier-specific status string. Preserved for backward compatibility.
	Status string `json:"status"`
	// NormalizedStatus is the carrier-agnostic status for this event.
	NormalizedStatus TrackingStatus `json:"normalizedStatus"`
	Location         string         `json:"location,omitempty"`
	Details          string         `json:"details,omitempty"`
}
