// Package adapter provides the InPost ManifestAdapter implementation.
// This file is located at /internal/adapter/inpost_pickups.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/kristiannissen/carrier-gateway/internal/requestid"
)

// BookPickup schedules a one-time courier collection with InPost.
// Pickups are available in Poland only; a country gate on req.Address.Country
// enforces this before any API call is made.
//
// The collection window is built from req.Pickup.Date + ReadyTime/CloseTime.
// When times are absent, 09:00–18:00 UTC is used as a safe default.
func (a *InPostAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	if !strings.EqualFold(req.Address.Country, "PL") {
		return nil, notSupported("InPost", "pickup",
			fmt.Sprintf("pickups are only available in Poland (PL); got %q", req.Address.Country))
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpost: obtain bearer token: %w", err)
	}

	first, last := inpostSplitName(req.Contact.Name)

	addr := map[string]any{
		"countryCode": strings.ToUpper(req.Address.Country),
		"city":        req.Address.City,
		"postalCode":  req.Address.PostalCode,
		"street":      req.Address.Street,
	}
	if req.Address.HouseNumber != "" {
		addr["houseNumber"] = req.Address.HouseNumber
	}
	if req.Pickup.Location != "" {
		addr["locationDescription"] = req.Pickup.Location
	}

	// itemType defaults to PARCEL; RECYCLABLE_PACKAGING is supported for InPost PL.
	itemType := "PARCEL"
	if strings.EqualFold(req.ItemType, "RECYCLABLE_PACKAGING") {
		itemType = "RECYCLABLE_PACKAGING"
	}

	payload := map[string]any{
		"address": addr,
		"contactPerson": map[string]any{
			"firstName": first,
			"lastName":  last,
			"email":     req.Contact.Email,
			"phone":     inpostPhone(req.Contact.Phone),
		},
		"pickupTime": inpostPickupTime(req.Pickup),
		"volume": map[string]any{
			"itemType": itemType,
			"count":    req.EstimatedParcels,
			"totalWeight": map[string]any{
				"amount": req.EstimatedWeight,
				"unit":   "KG",
			},
		},
	}
	if req.Pickup.SpecialInstructions != "" {
		payload["references"] = map[string]any{
			"custom": map[string]any{"note": req.Pickup.SpecialInstructions},
		}
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("inpost: marshal pickup request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/pickups/v1/organizations/%s/one-time-pickups", a.BaseURL, a.OrgID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("inpost: create pickup request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if id := requestid.FromContext(ctx); id != "" {
		httpReq.Header.Set("X-Request-Id", id)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("inpost: pickup API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("inpost: read pickup response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, inpostAPIError("pickup", resp.StatusCode, body)
	}

	var pickupResp struct {
		ID               string `json:"id"`
		CarrierReference struct {
			TrackingNumber string `json:"trackingNumber"`
		} `json:"carrierReference"`
	}
	if err := json.Unmarshal(body, &pickupResp); err != nil {
		return nil, fmt.Errorf("inpost: decode pickup response: %w", err)
	}

	return &PickupResponse{
		Carrier:            "inpost",
		ConfirmationNumber: pickupResp.ID,
		Date:               req.Pickup.Date,
		ReadyTime:          req.Pickup.ReadyTime,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported for InPost.
// InPost has no pickup update endpoint — cancel and rebook instead.
func (a *InPostAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("InPost", "pickup update",
		"InPost has no pickup update endpoint — cancel the existing pickup and create a new one")
}

// CancelPickup cancels a one-time pickup order with InPost.
// confirmationNumber is the pickup UUID returned by BookPickup.
func (a *InPostAdapter) CancelPickup(ctx context.Context, _, confirmationNumber string) error {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return fmt.Errorf("inpost: obtain bearer token: %w", err)
	}

	endpoint := fmt.Sprintf("%s/pickups/v1/organizations/%s/one-time-pickups/%s/cancel",
		a.BaseURL, a.OrgID, confirmationNumber)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, nil)
	if err != nil {
		return fmt.Errorf("inpost: create pickup cancel request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if id := requestid.FromContext(ctx); id != "" {
		httpReq.Header.Set("X-Request-Id", id)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("inpost: pickup cancel API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("inpost: read pickup cancel response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return inpostAPIError("pickup cancel", resp.StatusCode, body)
	}
	return nil
}

// CloseManifest is not supported for InPost.
// InPost uses a drop-off locker network — there is no end-of-day manifest close.
func (a *InPostAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("InPost", "manifest close",
		"InPost uses a drop-off locker network — no end-of-day manifest close is required")
}

// GetPickupAvailability is not supported for InPost in the unified slot format.
// Use the carrier-native GET /pickups/v1/cutoff-time endpoint to check
// same-day eligibility per postal code before calling BookPickup.
func (a *InPostAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("InPost", "pickup availability",
		"use GET /pickups/v1/cutoff-time?postalCode=&countryCode= to check same-day pickup eligibility")
}

// inpostPickupTime converts a PickupWindow to the InPost from/to datetime pair.
// RFC3339 UTC datetimes are constructed from Date + ReadyTime/CloseTime.
// When times are absent, 09:00 and 18:00 UTC are used as safe defaults.
func inpostPickupTime(w PickupWindow) map[string]any {
	return map[string]any{
		"from": inpostPickupDatetime(w.Date, w.ReadyTime, "09:00"),
		"to":   inpostPickupDatetime(w.Date, w.CloseTime, "18:00"),
	}
}

// inpostPickupDatetime combines a YYYY-MM-DD date with an HH:MM time
// (or a fallback default) into a UTC RFC3339-compatible datetime string.
func inpostPickupDatetime(date, t, fallback string) string {
	if t == "" {
		t = fallback
	}
	return date + "T" + t + ":00Z"
}

// GetPickupByID retrieves a single one-time pickup order by its UUID.
// Scopes: api:one-time-pickups:read.
func (a *InPostAdapter) GetPickupByID(ctx context.Context, orderID string) (*PickupInfo, error) {
	if orderID == "" {
		return nil, fmt.Errorf("inpost: orderID must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpost: obtain bearer token: %w", err)
	}

	endpoint := fmt.Sprintf("%s/pickups/v1/organizations/%s/one-time-pickups/%s",
		a.BaseURL, a.OrgID, orderID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("inpost: create get-pickup request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if id := requestid.FromContext(ctx); id != "" {
		httpReq.Header.Set("X-Request-Id", id)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("inpost: get-pickup API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("inpost: read get-pickup response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, inpostAPIError("get-pickup", resp.StatusCode, body)
	}

	var r struct {
		ID               string `json:"id"`
		CreatedTime      string `json:"createdTime"`
		LastModifiedTime string `json:"lastModifiedTime"`
		PickupTime       struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"pickupTime"`
		CarrierReference struct {
			TrackingNumber string `json:"trackingNumber"`
		} `json:"carrierReference"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("inpost: decode get-pickup response: %w", err)
	}

	return &PickupInfo{
		ID:                 r.ID,
		Carrier:            "inpost",
		Status:             r.Status,
		ConfirmationNumber: r.CarrierReference.TrackingNumber,
		ReadyTime:          r.PickupTime.From,
		CloseTime:          r.PickupTime.To,
		CreatedAt:          r.CreatedTime,
		UpdatedAt:          r.LastModifiedTime,
	}, nil
}

// ListPickups retrieves a paged list of one-time pickup orders for the organisation.
// Scopes: api:one-time-pickups:read.
func (a *InPostAdapter) ListPickups(ctx context.Context, req ListPickupsRequest) (*PickupList, error) {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpost: obtain bearer token: %w", err)
	}

	u, err := url.Parse(fmt.Sprintf("%s/pickups/v1/organizations/%s/one-time-pickups", a.BaseURL, a.OrgID))
	if err != nil {
		return nil, fmt.Errorf("inpost: parse list-pickups URL: %w", err)
	}
	q := u.Query()
	if req.Page > 0 {
		q.Set("page", strconv.Itoa(req.Page))
	}
	size := req.Size
	if size <= 0 {
		size = 20
	}
	q.Set("size", strconv.Itoa(size))
	for _, s := range req.Sort {
		q.Add("sort", s)
	}
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("inpost: create list-pickups request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if id := requestid.FromContext(ctx); id != "" {
		httpReq.Header.Set("X-Request-Id", id)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("inpost: list-pickups API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("inpost: read list-pickups response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, inpostAPIError("list-pickups", resp.StatusCode, body)
	}

	var r struct {
		Page       int `json:"page"`
		Count      int `json:"count"`
		TotalPages int `json:"totalPages"`
		PerPage    int `json:"perPage"`
		Items      []struct {
			ID               string `json:"id"`
			CreatedTime      string `json:"createdTime"`
			LastModifiedTime string `json:"lastModifiedTime"`
			PickupTime       struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"pickupTime"`
			CarrierReference struct {
				TrackingNumber string `json:"trackingNumber"`
			} `json:"carrierReference"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("inpost: decode list-pickups response: %w", err)
	}

	items := make([]PickupInfo, len(r.Items))
	for i, it := range r.Items {
		items[i] = PickupInfo{
			ID:                 it.ID,
			Carrier:            "inpost",
			Status:             it.Status,
			ConfirmationNumber: it.CarrierReference.TrackingNumber,
			ReadyTime:          it.PickupTime.From,
			CloseTime:          it.PickupTime.To,
			CreatedAt:          it.CreatedTime,
			UpdatedAt:          it.LastModifiedTime,
		}
	}

	return &PickupList{
		Carrier:    "inpost",
		Page:       r.Page,
		Count:      r.Count,
		TotalPages: r.TotalPages,
		PerPage:    r.PerPage,
		Items:      items,
	}, nil
}

// GetCutoffTime retrieves the latest hour at which a same-day pickup order can
// be created for the given postal code and country. The pickup window start must
// fall before the returned cutoff; the end must be at least 120 minutes later.
// Scopes: api:one-time-pickups:read.
func (a *InPostAdapter) GetCutoffTime(ctx context.Context, postalCode, countryCode string) (*PickupCutoffTime, error) {
	if postalCode == "" || countryCode == "" {
		return nil, fmt.Errorf("inpost: postalCode and countryCode are required for cutoff-time lookup")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("inpost: obtain bearer token: %w", err)
	}

	u, err := url.Parse(a.BaseURL + "/pickups/v1/cutoff-time")
	if err != nil {
		return nil, fmt.Errorf("inpost: parse cutoff-time URL: %w", err)
	}
	q := u.Query()
	q.Set("postalCode", postalCode)
	q.Set("countryCode", countryCode)
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("inpost: create cutoff-time request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if id := requestid.FromContext(ctx); id != "" {
		httpReq.Header.Set("X-Request-Id", id)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("inpost: cutoff-time API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("inpost: read cutoff-time response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, inpostAPIError("cutoff-time", resp.StatusCode, body)
	}

	var r struct {
		PostalCode string `json:"postalCode"`
		CutoffTime string `json:"cutoffTime"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("inpost: decode cutoff-time response: %w", err)
	}

	return &PickupCutoffTime{
		Carrier:    "inpost",
		PostalCode: r.PostalCode,
		CutoffTime: r.CutoffTime,
	}, nil
}

// inpostPhone splits a single E.164 phone number into the InPost
// {prefix, number} structure expected by the Pickups API.
//
// The heuristic assumes a two-digit country code (e.g. +48, +44, +39),
// which covers all active InPost markets (PL, GB, IT). For numbers that
// do not start with + or are too short to split safely, the full value
// is placed in number with an empty prefix.
func inpostPhone(phone string) map[string]any {
	const minDigitsAfterPrefix = 6
	if !strings.HasPrefix(phone, "+") || len(phone) < 4 {
		return map[string]any{"prefix": "", "number": phone}
	}
	rest := phone[1:] // strip leading "+"
	// Try a two-digit country code first (covers PL +48, GB +44, IT +39).
	if len(rest) > 2+minDigitsAfterPrefix {
		return map[string]any{"prefix": "+" + rest[:2], "number": rest[2:]}
	}
	// Fall back: one-digit code (e.g. +1 for North America).
	if len(rest) > 1+minDigitsAfterPrefix {
		return map[string]any{"prefix": "+" + rest[:1], "number": rest[1:]}
	}
	return map[string]any{"prefix": "", "number": phone}
}
