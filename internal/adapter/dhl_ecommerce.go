// Package adapter provides the DHL eCommerce Americas implementation of CarrierAdapter and ManifestAdapter.
// This file is located at /internal/adapter/dhl_ecommerce.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// dhlECSTokenCache holds a cached OAuth2 access token with its expiry time.
type dhlECSTokenCache struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// valid reports whether the cached token is present and not yet expired.
// A 30-second buffer is applied to avoid using a token that expires mid-request.
func (c *dhlECSTokenCache) valid() bool {
	return c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second))
}

// DHLECSAdapter implements CarrierAdapter and ManifestAdapter for
// DHL eCommerce Americas (api.dhlecs.com, formerly DHL eCommerce Solutions).
//
// Authentication: OAuth2 client_credentials flow.
// Token endpoint: POST /auth/v4/accesstoken with application/x-www-form-urlencoded body.
//
// Manifest flow (async, two-step):
//  1. POST /shipping/v4/manifest → returns requestID.
//  2. Poll GET /shipping/v4/manifest/{pickup}/{requestID} until status is COMPLETED,
//     then decode the base64 PDF from manifests[0].manifestData.
//
// Booking, tracking, and label retrieval are not yet implemented for this carrier.
// BookShipment, TrackShipment, FetchLabel, CancelShipment, and UpdateShipment
// all return ErrNotSupported.
type DHLECSAdapter struct {
	// PickupAccountNumber is the DHL eCommerce pickup account number.
	// Passed as the "pickup" field in every manifest request.
	PickupAccountNumber string
	// ClientID is the OAuth2 client_id.
	ClientID string
	// ClientSecret is the OAuth2 client_secret.
	ClientSecret string
	// BaseURL is the DHL eCommerce Americas API base URL.
	// Production: https://api.dhlecs.com
	// Sandbox:    https://api-sandbox.dhlecs.com
	BaseURL string
	// PollInterval is the time between manifest status poll attempts.
	// Defaults to 2 seconds.
	PollInterval time.Duration
	// PollTimeout is the maximum time to wait for a manifest to reach COMPLETED.
	// Defaults to 60 seconds.
	PollTimeout time.Duration
	HTTPClient  *http.Client
	tokenCache  dhlECSTokenCache
	log         *zap.Logger
}

// NewDHLECSAdapter creates a DHLECSAdapter ready for production use.
// pickupAccountNumber is the DHL eCommerce pickup account number.
// clientID and clientSecret are the OAuth2 credentials.
func NewDHLECSAdapter(pickupAccountNumber, clientID, clientSecret string, log *zap.Logger) *DHLECSAdapter {
	return &DHLECSAdapter{
		PickupAccountNumber: pickupAccountNumber,
		ClientID:            clientID,
		ClientSecret:        clientSecret,
		BaseURL:             "https://api.dhlecs.com",
		PollInterval:        2 * time.Second,
		PollTimeout:         60 * time.Second,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// fetchToken obtains a new OAuth2 access token using the client_credentials flow.
// Endpoint: POST /auth/v4/accesstoken (application/x-www-form-urlencoded).
func (a *DHLECSAdapter) fetchToken(ctx context.Context) error {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", a.ClientID)
	form.Set("client_secret", a.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BaseURL+"/auth/v4/accesstoken", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create DHL ECS token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("DHL ECS token request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read DHL ECS token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("DHL ECS token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to decode DHL ECS token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return fmt.Errorf("DHL ECS token response contained no access_token")
	}

	a.tokenCache.mu.Lock()
	a.tokenCache.accessToken = tokenResp.AccessToken
	a.tokenCache.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	a.tokenCache.mu.Unlock()

	return nil
}

// bearerToken returns a valid Bearer token, refreshing if expired.
func (a *DHLECSAdapter) bearerToken(ctx context.Context) (string, error) {
	a.tokenCache.mu.Lock()
	valid := a.tokenCache.valid()
	token := a.tokenCache.accessToken
	a.tokenCache.mu.Unlock()

	if valid {
		return token, nil
	}
	if err := a.fetchToken(ctx); err != nil {
		return "", err
	}
	a.tokenCache.mu.Lock()
	token = a.tokenCache.accessToken
	a.tokenCache.mu.Unlock()
	return token, nil
}

// dhlECSCreateRequest is the body sent to POST /shipping/v4/manifest.
type dhlECSCreateRequest struct {
	Pickup    string              `json:"pickup"`
	Products  []string            `json:"products,omitempty"`
	Manifests []dhlECSManifestIn  `json:"manifests"`
}

// dhlECSManifestIn holds the package identifiers for one manifest group.
type dhlECSManifestIn struct {
	PackageIDs []string `json:"packageIds,omitempty"`
}

// dhlECSCreateResponse is the body returned by POST /shipping/v4/manifest.
type dhlECSCreateResponse struct {
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Link      string `json:"link"`
}

// dhlECSGetResponse is the body returned by GET /shipping/v4/manifest/{pickup}/{requestId}.
type dhlECSGetResponse struct {
	Timestamp string `json:"timestamp"`
	Pickup    string `json:"pickup"`
	RequestID string `json:"requestId"`
	Status    string `json:"status"`
	Link      string `json:"link"`
	Manifests []struct {
		CreatedOn          string `json:"createdOn"`
		ManifestID         string `json:"manifestId"`
		DistributionCenter string `json:"distributionCenter"`
		IsInternational    bool   `json:"isInternational"`
		Total              int    `json:"total"`
		ManifestData       string `json:"manifestData"` // base64 PDF
		EncodeType         string `json:"encodeType"`
		Format             string `json:"format"`
	} `json:"manifests"`
	ManifestSummary struct {
		Total   int `json:"total"`
		Invalid struct {
			Total int `json:"total"`
		} `json:"invalid"`
	} `json:"manifestSummary"`
}

// CloseManifest triggers an end-of-day manifest for DHL eCommerce Americas and
// returns the handover PDF once the carrier confirms completion.
//
// When req.TrackingNumbers is non-empty, only those package IDs are manifested.
// When empty, all open packages for the pickup account are manifested.
//
// The call blocks until the manifest reaches COMPLETED or PollTimeout elapses.
func (a *DHLECSAdapter) CloseManifest(ctx context.Context, req ManifestRequest) (*ManifestResponse, error) {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("DHL ECS manifest: failed to obtain token: %w", err)
	}

	body, err := a.buildCreatePayload(req)
	if err != nil {
		return nil, fmt.Errorf("DHL ECS manifest: failed to build payload: %w", err)
	}

	requestID, err := a.postManifest(ctx, token, body)
	if err != nil {
		return nil, fmt.Errorf("DHL ECS manifest: create request failed: %w", err)
	}

	a.log.Info("DHL ECS manifest job submitted",
		zap.String("requestID", requestID),
		zap.String("pickup", a.PickupAccountNumber),
	)

	result, err := a.pollManifest(ctx, token, requestID)
	if err != nil {
		return nil, fmt.Errorf("DHL ECS manifest: poll failed: %w", err)
	}
	result.Date = req.Date
	return result, nil
}

// buildCreatePayload marshals the POST /shipping/v4/manifest request body.
func (a *DHLECSAdapter) buildCreatePayload(req ManifestRequest) ([]byte, error) {
	cr := dhlECSCreateRequest{
		Pickup: a.PickupAccountNumber,
	}
	if len(req.TrackingNumbers) > 0 {
		cr.Manifests = []dhlECSManifestIn{{PackageIDs: req.TrackingNumbers}}
	} else {
		cr.Manifests = []dhlECSManifestIn{}
	}

	return json.Marshal(cr)
}

// postManifest sends POST /shipping/v4/manifest and returns the requestID.
func (a *DHLECSAdapter) postManifest(ctx context.Context, token string, payload []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BaseURL+"/shipping/v4/manifest", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create manifest POST request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("manifest POST request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read manifest POST response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("manifest POST returned status %d: %s", resp.StatusCode, string(body))
	}

	var cr dhlECSCreateResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("failed to decode manifest POST response: %w", err)
	}
	if cr.RequestID == "" {
		return "", fmt.Errorf("manifest POST response contained no requestId")
	}
	return cr.RequestID, nil
}

// pollManifest polls GET /shipping/v4/manifest/{pickup}/{requestID} until status
// is COMPLETED or PollTimeout elapses.
func (a *DHLECSAdapter) pollManifest(ctx context.Context, token, requestID string) (*ManifestResponse, error) {
	deadline := time.Now().Add(a.PollTimeout)
	pollURL := fmt.Sprintf("%s/shipping/v4/manifest/%s/%s", a.BaseURL, a.PickupAccountNumber, requestID)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("manifest job %s did not complete within %s", requestID, a.PollTimeout)
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("manifest poll cancelled: %w", ctx.Err())
		case <-time.After(a.PollInterval):
		}

		gr, err := a.getManifest(ctx, token, pollURL)
		if err != nil {
			return nil, err
		}

		if gr.Status != "COMPLETED" {
			a.log.Debug("DHL ECS manifest not yet complete",
				zap.String("requestID", requestID),
				zap.String("status", gr.Status),
			)
			continue
		}

		return a.buildManifestResponse(gr), nil
	}
}

// getManifest performs one GET poll and returns the parsed response.
func (a *DHLECSAdapter) getManifest(ctx context.Context, token, pollURL string) (*dhlECSGetResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create manifest GET request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("manifest GET request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest GET response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest GET returned status %d: %s", resp.StatusCode, string(body))
	}

	var gr dhlECSGetResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return nil, fmt.Errorf("failed to decode manifest GET response: %w", err)
	}
	return &gr, nil
}

// buildManifestResponse converts a completed dhlECSGetResponse into a ManifestResponse.
// If multiple manifest documents are returned, the first is used; additional ones are
// noted in Warnings.
func (a *DHLECSAdapter) buildManifestResponse(gr *dhlECSGetResponse) *ManifestResponse {
	r := &ManifestResponse{
		Carrier:  "dhl_ecommerce",
		Status:   "closed",
		Warnings: []string{},
	}

	if gr.ManifestSummary.Invalid.Total > 0 {
		r.Warnings = append(r.Warnings,
			fmt.Sprintf("%d package(s) could not be manifested", gr.ManifestSummary.Invalid.Total))
	}

	if len(gr.Manifests) == 0 {
		r.Warnings = append(r.Warnings, "carrier returned no manifest documents")
		return r
	}

	first := gr.Manifests[0]
	r.ParcelsConfirmed = first.Total
	r.ManifestDocument = first.ManifestData
	r.ManifestDocumentFormat = first.Format

	if len(gr.Manifests) > 1 {
		r.Warnings = append(r.Warnings,
			fmt.Sprintf("carrier returned %d manifest documents; only the first is included", len(gr.Manifests)))
	}

	return r
}

// ── CarrierAdapter stubs ───────────────────────────────────────────────────────

// BookShipment is not yet implemented for DHL eCommerce Americas.
func (a *DHLECSAdapter) BookShipment(_ context.Context, _ BookingRequest) (*BookingResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "booking", "not yet implemented")
}

// TrackShipment is not yet implemented for DHL eCommerce Americas.
func (a *DHLECSAdapter) TrackShipment(_ context.Context, _ string) (*TrackingResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "tracking", "not yet implemented")
}

// FetchLabel is not yet implemented for DHL eCommerce Americas.
func (a *DHLECSAdapter) FetchLabel(_ context.Context, _ LabelRequest) (*LabelResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "label fetch", "not yet implemented")
}

// CancelShipment is not supported for DHL eCommerce Americas via API.
func (a *DHLECSAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "cancellation", "contact DHL customer service")
}

// UpdateShipment is not supported for DHL eCommerce Americas via API.
func (a *DHLECSAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "post-booking update", "contact DHL customer service")
}

// ── ManifestAdapter stubs ─────────────────────────────────────────────────────

// BookPickup is not supported for DHL eCommerce Americas via API.
func (a *DHLECSAdapter) BookPickup(_ context.Context, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "pickup booking", "contact DHL customer service")
}

// UpdatePickup is not supported for DHL eCommerce Americas via API.
func (a *DHLECSAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "pickup update", "contact DHL customer service")
}

// CancelPickup is not supported for DHL eCommerce Americas via API.
func (a *DHLECSAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return notSupported("DHL eCommerce Americas", "pickup cancellation", "contact DHL customer service")
}

// GetPickupAvailability is not supported for DHL eCommerce Americas.
func (a *DHLECSAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("DHL eCommerce Americas", "pickup availability", "")
}
