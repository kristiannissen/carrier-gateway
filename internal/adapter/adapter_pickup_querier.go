// Package adapter provides the PickupQuerier interface and pickup query types.
// This file is located at /internal/adapter/adapter_pickup_querier.go.
package adapter

import "context"

// PickupInfo is a normalized pickup order returned by GetPickupByID and ListPickups.
type PickupInfo struct {
	// ID is the carrier-internal pickup order UUID.
	ID string `json:"id"`
	// Carrier is the carrier key.
	Carrier string `json:"carrier"`
	// Status is the current pickup order status (e.g. "CREATED", "COLLECTED", "CANCELLED").
	Status string `json:"status"`
	// ConfirmationNumber is the carrier-issued tracking reference for the pickup.
	ConfirmationNumber string `json:"confirmationNumber,omitempty"`
	// ReadyTime is the pickup window open time as returned by the carrier (RFC3339 or HH:MM).
	ReadyTime string `json:"readyTime,omitempty"`
	// CloseTime is the pickup window close time as returned by the carrier (RFC3339 or HH:MM).
	CloseTime string `json:"closeTime,omitempty"`
	// CreatedAt is the time the pickup order was created (RFC3339).
	CreatedAt string `json:"createdAt,omitempty"`
	// UpdatedAt is the time the pickup order was last modified (RFC3339).
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// ListPickupsRequest is the input to ListPickups.
type ListPickupsRequest struct {
	// Carrier is the carrier key.
	Carrier string `json:"carrier"`
	// Page is the zero-based page index.
	Page int `json:"page"`
	// Size is the number of items per page. Defaults to 20 when zero.
	Size int `json:"size"`
	// Sort is the list of sorting criteria in "property,(asc|desc)" format.
	Sort []string `json:"sort,omitempty"`
}

// PickupList is the paged result from ListPickups.
type PickupList struct {
	// Carrier is the carrier key.
	Carrier string `json:"carrier"`
	// Page is the current zero-based page index.
	Page int `json:"page"`
	// Count is the number of items on the current page.
	Count int `json:"count"`
	// TotalPages is the total number of pages available.
	TotalPages int `json:"totalPages"`
	// PerPage is the number of items per page requested.
	PerPage int `json:"perPage"`
	// Items is the list of pickup orders on the current page.
	Items []PickupInfo `json:"items"`
}

// PickupCutoffTime holds the latest hour at which a same-day pickup order can
// be created for a given postal code. The pickup window start must fall before
// the cutoff; the end must be at least 120 minutes after both the cutoff and
// the current time.
type PickupCutoffTime struct {
	// Carrier is the carrier key.
	Carrier string `json:"carrier"`
	// PostalCode is the postal code for which the cutoff applies.
	PostalCode string `json:"postalCode"`
	// CutoffTime is the latest order submission time (e.g. "13:00:00").
	CutoffTime string `json:"cutoffTime"`
}

// PickupQuerier is implemented by carriers that support querying pickup orders.
// It is kept separate from ManifestAdapter so carriers that support booking but
// not querying do not need to implement it.
//
// The handler type-asserts a CarrierAdapter to PickupQuerier at request time.
// If the assertion fails the carrier does not support pickup queries and the
// handler returns HTTP 501.
type PickupQuerier interface {
	// GetPickupByID retrieves details for a single pickup order.
	GetPickupByID(ctx context.Context, orderID string) (*PickupInfo, error)

	// ListPickups returns a paged list of pickup orders for the organisation.
	ListPickups(ctx context.Context, req ListPickupsRequest) (*PickupList, error)

	// GetCutoffTime returns the latest time at which a same-day pickup can be
	// created for the given postal code. Use this before BookPickup to check
	// same-day eligibility.
	GetCutoffTime(ctx context.Context, postalCode, countryCode string) (*PickupCutoffTime, error)
}
