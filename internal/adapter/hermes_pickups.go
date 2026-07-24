// Package adapter provides the Hermes Germany ManifestAdapter and PickupQuerier implementation.
// This file is located at /internal/adapter/hermes_pickups.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"
)

// hermesPickupOrderData mirrors the fields of the HSI PickupOrderData schema
// that map onto PickupInfo. It is returned by GET /pickuporders (both the
// list endpoint and, filtered client-side, a single-order lookup — the HSI
// API exposes no per-ID GET). The schema also includes shipmentID,
// pickupDate, pickupName, pickupAddress, volume, and parcelCount, which are
// omitted here as PickupInfo has no equivalent fields for them.
type hermesPickupOrderData struct {
	PickupOrderID     string `json:"pickupOrderID"`
	ActualOrderState  string `json:"actualOrderState"`
	PickupTimeSlot    string `json:"pickupTimeSlot"`
	OrderCreationDate string `json:"orderCreationDate"`
}

// hermesPickupAddressPayload converts a unified PickupAddress to the Hermes
// PickupAddress schema. Returns nil when the address has no street set, which
// signals the caller to omit the field entirely so Hermes falls back to the
// account's configured pickup address.
func hermesPickupAddressPayload(a PickupAddress) map[string]any {
	if a.Street == "" {
		return nil
	}
	return map[string]any{
		"street":      a.Street,
		"houseNumber": a.HouseNumber,
		"zipCode":     a.PostalCode,
		"town":        a.City,
		"countryCode": a.Country,
	}
}

// hermesPickupTimeSlot maps a requested ready time (HH:MM) to the nearest
// Hermes pickupTimeSlot enum value. Returns "" when readyTime is empty or
// unparseable, in which case the field is omitted and Hermes applies its
// standard collection window.
func hermesPickupTimeSlot(readyTime string) string {
	if len(readyTime) < 2 {
		return ""
	}
	var hour int
	if _, err := fmt.Sscanf(readyTime[:2], "%d", &hour); err != nil {
		return ""
	}
	switch {
	case hour < 12:
		return "BETWEEN_10_AND_13"
	case hour < 14:
		return "BETWEEN_12_AND_15"
	default:
		return "BETWEEN_14_AND_17"
	}
}

// BookPickup schedules a one-time collection order with Hermes via
// POST /pickuporders. pickupDate is the only required field; pickupAddress,
// pickupName, and parcelCount are included only when the caller supplies them,
// otherwise Hermes falls back to the account's configured pickup address.
//
// parcelCount has no single-count field in the HSI schema — it is broken down
// by parcel size (XS/S/M/L/XL). Since PickupRequest carries only a total
// EstimatedParcels count, it is reported as pickupParcelCountM (a best-effort
// bucket) rather than split across sizes.
func (a *HermesAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	if req.Pickup.Date == "" {
		return nil, fmt.Errorf("hermes: pickup date is required")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("hermes: obtain bearer token: %w", err)
	}

	payload := map[string]any{
		"pickupDate": req.Pickup.Date,
	}
	if slot := hermesPickupTimeSlot(req.Pickup.ReadyTime); slot != "" {
		payload["pickupTimeSlot"] = slot
	}
	if req.Contact.Phone != "" {
		payload["phone"] = req.Contact.Phone
	}
	if addr := hermesPickupAddressPayload(req.Address); addr != nil {
		payload["pickupAddress"] = addr
		firstname, lastname := splitName(req.Contact.Name)
		payload["pickupName"] = map[string]any{
			"firstname": firstname,
			"lastname":  lastname,
		}
	}
	if req.EstimatedParcels > 0 {
		payload["parcelCount"] = map[string]any{
			"pickupParcelCountM": req.EstimatedParcels,
		}
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("hermes: marshal pickup request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.OrderBaseURL+"/pickuporders", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("hermes: create pickup request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Accept-Language", "EN")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("hermes: pickup API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("hermes: read pickup response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hermes: pickup API returned status %d: %s", resp.StatusCode, string(body))
	}

	var pickupResp struct {
		PickupOrderID string `json:"pickupOrderID"`
	}
	if err := json.Unmarshal(body, &pickupResp); err != nil {
		return nil, fmt.Errorf("hermes: decode pickup response: %w", err)
	}
	if pickupResp.PickupOrderID == "" {
		return nil, fmt.Errorf("hermes: pickup response contained no pickupOrderID: %s", string(body))
	}

	a.log.Info("hermes pickup order booked", zap.String("pickupOrderID", pickupResp.PickupOrderID))

	return &PickupResponse{
		Carrier:            "hermes",
		ConfirmationNumber: pickupResp.PickupOrderID,
		Date:               req.Pickup.Date,
		ReadyTime:          req.Pickup.ReadyTime,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported by Hermes.
// The HSI API has no PATCH/PUT endpoint for pickup orders — cancel the
// existing order via CancelPickup and create a new one via BookPickup.
func (a *HermesAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("Hermes", "pickup update",
		"the HSI API has no update endpoint for pickup orders — cancel the existing pickup and create a new one")
}

// CancelPickup cancels a Hermes pickup order via DELETE /pickuporders/{id}.
// confirmationNumber is the pickupOrderID returned by BookPickup.
func (a *HermesAdapter) CancelPickup(ctx context.Context, _, confirmationNumber string) error {
	if confirmationNumber == "" {
		return fmt.Errorf("hermes: confirmation number must not be empty")
	}

	token, err := a.bearerToken(ctx)
	if err != nil {
		return fmt.Errorf("hermes: obtain bearer token: %w", err)
	}

	endpoint := fmt.Sprintf("%s/pickuporders/%s", a.OrderBaseURL, confirmationNumber)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, http.NoBody)
	if err != nil {
		return fmt.Errorf("hermes: create pickup cancel request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Accept-Language", "EN")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("hermes: pickup cancel API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("hermes: read pickup cancel response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hermes: pickup cancel API returned status %d: %s", resp.StatusCode, string(body))
	}

	a.log.Info("hermes pickup order cancelled", zap.String("pickupOrderID", confirmationNumber))
	return nil
}

// CloseManifest is not supported by Hermes.
// No end-of-day manifest endpoint exists anywhere in the HSI API surface.
func (a *HermesAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("Hermes", "manifest close", "no end-of-day manifest endpoint exists in the HSI API")
}

// GetPickupAvailability is not supported by Hermes.
// The HSI routing API (GET /routing) resolves delivery routing for an
// address, not collection timeslots — there is no dedicated availability
// endpoint for pickups.
func (a *HermesAdapter) GetPickupAvailability(_ context.Context, _ PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	return nil, notSupported("Hermes", "pickup availability",
		"no dedicated availability endpoint exists in the HSI API")
}

// listPickupOrders fetches every pickup order on the account via
// GET /pickuporders. The HSI API takes no filter or pagination parameters —
// it always returns the full list — so GetPickupByID and ListPickups both
// call this and page/filter client-side.
func (a *HermesAdapter) listPickupOrders(ctx context.Context) ([]hermesPickupOrderData, error) {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("hermes: obtain bearer token: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, a.OrderBaseURL+"/pickuporders", nil)
	if err != nil {
		return nil, fmt.Errorf("hermes: create list-pickups request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Accept-Language", "EN")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("hermes: list-pickups API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("hermes: read list-pickups response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hermes: list-pickups API returned status %d: %s", resp.StatusCode, string(body))
	}

	var listResp struct {
		ListOfPickupOrders []hermesPickupOrderData `json:"listOfPickupOrders"`
	}
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("hermes: decode list-pickups response: %w", err)
	}
	return listResp.ListOfPickupOrders, nil
}

// hermesPickupInfo converts a hermesPickupOrderData to the normalized
// PickupInfo shape. Hermes exposes a single pickupOrderID that serves both as
// the record identifier and the value CancelPickup expects, so both ID and
// ConfirmationNumber are set to it. ReadyTime carries the pickupTimeSlot enum
// value (e.g. "BETWEEN_10_AND_13") since Hermes returns a named slot rather
// than explicit start/end times.
func hermesPickupInfo(o hermesPickupOrderData) PickupInfo {
	return PickupInfo{
		ID:                 o.PickupOrderID,
		Carrier:            "hermes",
		Status:             o.ActualOrderState,
		ConfirmationNumber: o.PickupOrderID,
		ReadyTime:          o.PickupTimeSlot,
		CreatedAt:          o.OrderCreationDate,
	}
}

// GetPickupByID retrieves a single pickup order by its pickupOrderID.
// The HSI API has no per-ID GET, so this fetches the full list via
// GET /pickuporders and filters client-side.
func (a *HermesAdapter) GetPickupByID(ctx context.Context, orderID string) (*PickupInfo, error) {
	if orderID == "" {
		return nil, fmt.Errorf("hermes: orderID must not be empty")
	}

	orders, err := a.listPickupOrders(ctx)
	if err != nil {
		return nil, err
	}
	for _, o := range orders {
		if o.PickupOrderID == orderID {
			info := hermesPickupInfo(o)
			return &info, nil
		}
	}
	return nil, fmt.Errorf("hermes: no pickup order found for id %q", orderID)
}

// ListPickups returns every pickup order on the account, paged client-side.
// The HSI API returns the full unfiltered list on every call — req.Page and
// req.Size are applied locally rather than forwarded as query parameters.
func (a *HermesAdapter) ListPickups(ctx context.Context, req ListPickupsRequest) (*PickupList, error) {
	orders, err := a.listPickupOrders(ctx)
	if err != nil {
		return nil, err
	}

	size := req.Size
	if size <= 0 {
		size = 20
	}
	total := len(orders)
	totalPages := (total + size - 1) / size
	if totalPages == 0 {
		totalPages = 1
	}

	start := req.Page * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}

	page := make([]PickupInfo, end-start)
	for i, o := range orders[start:end] {
		page[i] = hermesPickupInfo(o)
	}

	return &PickupList{
		Carrier:    "hermes",
		Page:       req.Page,
		Count:      len(page),
		TotalPages: totalPages,
		PerPage:    size,
		Items:      page,
	}, nil
}

// GetCutoffTime is not supported by Hermes.
// No cutoff-time endpoint exists anywhere in the HSI API surface.
func (a *HermesAdapter) GetCutoffTime(_ context.Context, _, _ string) (*PickupCutoffTime, error) {
	return nil, notSupported("Hermes", "pickup cutoff time", "no cutoff-time endpoint exists in the HSI API")
}
