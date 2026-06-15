// Package adapter provides the InPost ReturnAdapter implementation.
// This file is located at /internal/adapter/inpost_returns.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kristiannissen/carrier-gateway/internal/requestid"
)

// inpostReturnCountries is the set of ISO country codes for which InPost
// supports return shipments via API. Other markets are not offered.
var inpostReturnCountries = map[string]bool{
	"PL": true,
	"IT": true,
	"GB": true,
}

// BookReturn creates a new InPost return shipment via the Returns API v1.
//
// Returns are available in Poland (PL), Italy (IT), and the United Kingdom (GB)
// only. A country gate on req.Sender.Country enforces this before any API call.
//
// The minimal request requires only req.Sender. When req.EnableDropOffCode is
// true (the default), the API issues a short numeric code the customer can use
// at any InPost locker without printing a label.
//
// When req.Colli is provided, the first element is used as the single parcel
// (the InPost Returns API accepts exactly one parcel). When absent, the
// organisation account defaults apply.
func (a *InPostAdapter) BookReturn(ctx context.Context, req ReturnRequest) (*ReturnResponse, error) {
	country := strings.ToUpper(req.Sender.Country)
	if !inpostReturnCountries[country] {
		return nil, fmt.Errorf("inpost: returns are only available for PL, IT, and GB (got %q)", req.Sender.Country)
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpost: obtain bearer token: %w", err)
	}

	first, last := inpostSplitName(req.Sender.Name)
	sender := map[string]any{
		"firstName": first,
		"lastName":  last,
		"email":     req.Sender.Email,
		"phone":     req.Sender.Phone,
	}
	if req.Sender.Name != "" {
		sender["companyName"] = req.Sender.Name
	}

	origin := map[string]any{"countryCode": country}
	if req.Sender.State != "" {
		// GB shipments require subdivisionCode (e.g. "GB-ENG", "GB-NIR").
		origin["subdivisionCode"] = req.Sender.State
	}

	payload := map[string]any{
		"sender":            sender,
		"origin":            origin,
		"enableDropOffCode": req.EnableDropOffCode,
	}

	if req.Receiver.Name != "" || req.Receiver.Email != "" || req.Receiver.Street != "" {
		payload["recipient"] = inpostParty(req.Receiver)
	}
	if req.Receiver.Street != "" || req.Receiver.City != "" {
		payload["destination"] = inpostDestination(req.Receiver)
	}
	if req.ExpiresAt != "" {
		payload["expirationDate"] = req.ExpiresAt
	}

	// Returns API accepts exactly one parcel; additional colli are silently dropped.
	if len(req.Colli) > 0 {
		c := req.Colli[0]
		payload["parcels"] = []map[string]any{
			{
				"dimensions": map[string]any{
					"length": fmt.Sprintf("%.0f", c.Dimensions.Length),
					"width":  fmt.Sprintf("%.0f", c.Dimensions.Width),
					"height": fmt.Sprintf("%.0f", c.Dimensions.Height),
					"unit":   "CM",
				},
				"weight": map[string]any{
					// Returns API uses KG as a decimal string (not grams).
					"amount": fmt.Sprintf("%.2f", c.Weight),
					"unit":   "KG",
				},
			},
		}
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("inpost: marshal return request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/returns/v1/organizations/%s/shipments", a.BaseURL, a.OrgID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("inpost: create return request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if id := requestid.FromContext(ctx); id != "" {
		httpReq.Header.Set("X-Request-Id", id)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("inpost: return API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("inpost: read return response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, inpostAPIError("return booking", resp.StatusCode, body)
	}

	var returnResp struct {
		ID             string `json:"id"`
		ExpirationDate string `json:"expirationDate"`
		Parcels        []struct {
			TrackingNumber string `json:"trackingNumber"`
			DropOffCode    string `json:"dropOffCode"`
		} `json:"parcels"`
	}
	if err := json.Unmarshal(body, &returnResp); err != nil {
		return nil, fmt.Errorf("inpost: decode return response: %w", err)
	}

	r := &ReturnResponse{
		ShipmentID: returnResp.ID,
		Carrier:    "inpost",
		Status:     "booked",
		ExpiresAt:  returnResp.ExpirationDate,
	}
	if len(returnResp.Parcels) > 0 {
		r.TrackingNumber = returnResp.Parcels[0].TrackingNumber
		r.DropOffCode = returnResp.Parcels[0].DropOffCode
	}
	return r, nil
}

// GetReturnShipment retrieves details for a single InPost return shipment.
// shipmentID is the UUID returned by BookReturn (ReturnResponse.ShipmentID).
// Scopes: api:returns:read.
func (a *InPostAdapter) GetReturnShipment(ctx context.Context, shipmentID string) (*ReturnShipmentInfo, error) {
	if shipmentID == "" {
		return nil, fmt.Errorf("inpost: shipmentID must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpost: obtain bearer token: %w", err)
	}

	endpoint := fmt.Sprintf("%s/returns/v1/organizations/%s/shipments/%s",
		a.BaseURL, a.OrgID, shipmentID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("inpost: create get-return-shipment request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if id := requestid.FromContext(ctx); id != "" {
		httpReq.Header.Set("X-Request-Id", id)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("inpost: get-return-shipment API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("inpost: read get-return-shipment response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, inpostAPIError("get-return-shipment", resp.StatusCode, body)
	}

	var r struct {
		ID             string `json:"id"`
		ExpirationDate string `json:"expirationDate"`
		Parcels        []struct {
			ID             string `json:"id"`
			TrackingNumber string `json:"trackingNumber"`
			DropOffCode    string `json:"dropOffCode"`
		} `json:"parcels"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("inpost: decode get-return-shipment response: %w", err)
	}

	parcels := make([]ReturnParcelInfo, len(r.Parcels))
	for i, p := range r.Parcels {
		parcels[i] = ReturnParcelInfo{
			ID:             p.ID,
			TrackingNumber: p.TrackingNumber,
			DropOffCode:    p.DropOffCode,
		}
	}

	return &ReturnShipmentInfo{
		ID:             r.ID,
		Carrier:        "inpost",
		ExpirationDate: r.ExpirationDate,
		Parcels:        parcels,
	}, nil
}

// FetchReturnLabel retrieves the shipping label for an InPost return parcel
// via the Returns API v1 label endpoint.
//
// Supported formats: PDF A4 (default — note A6 is NOT available for returns),
// ZPL 203 dpi, ZPL 300 dpi, EPL2 203 dpi.
// trackingNumber in req is the long parcel tracking number from BookReturn.
func (a *InPostAdapter) FetchReturnLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpost: obtain bearer token: %w", err)
	}

	// Return labels default to A4; A6 is not supported on the returns endpoint.
	accept := inpostLabelAccept(req.Format, "A4")
	endpoint := fmt.Sprintf("%s/returns/v1/organizations/%s/shipments/%s/label",
		a.BaseURL, a.OrgID, req.TrackingNumber)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("inpost: create return label request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Accept", accept)
	if id := requestid.FromContext(ctx); id != "" {
		httpReq.Header.Set("X-Request-Id", id)
	}

	return fetchLabelFromURL(ctx, a.HTTPClient, httpReq, req, "inpost")
}
