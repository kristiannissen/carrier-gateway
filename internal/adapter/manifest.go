// Package adapter provides the ManifestAdapter interface and pickup/manifest types.
// This file is located at /internal/adapter/manifest.go.
package adapter

import "context"

// PickupContact is the contact person at the pickup location.
type PickupContact struct {
	// Name is the contact person's full name.
	Name string `json:"name"`
	// Phone is the contact phone number in E.164 format.
	Phone string `json:"phone"`
	// Email is the contact email address.
	Email string `json:"email"`
}

// PickupWindow describes the requested collection time window.
// ReadyTime and CloseTime are hints — carriers use them where supported
// and return their actual confirmed window in PickupResponse.
type PickupWindow struct {
	// Date is the requested collection date in ISO 8601 format (YYYY-MM-DD).
	Date string `json:"date"`
	// ReadyTime is the requested time parcels are ready, in HH:MM format.
	// Optional — carriers that do not accept a time window ignore it.
	ReadyTime string `json:"readyTime,omitempty"`
	// CloseTime is the requested latest collection time, in HH:MM format.
	// Optional — carriers that do not accept a time window ignore it.
	CloseTime string `json:"closeTime,omitempty"`
	// Location is a free-text description of the collection point (e.g. "reception").
	Location string `json:"location,omitempty"`
	// SpecialInstructions is a free-text message passed to the driver.
	SpecialInstructions string `json:"specialInstructions,omitempty"`
}

// PickupAddress is the address where the carrier should collect.
// When omitted, adapters fall back to the configured sender address.
type PickupAddress struct {
	// Street is the street name.
	Street string `json:"street,omitempty"`
	// HouseNumber is the building number, kept separate for carriers that require it distinct.
	HouseNumber string `json:"houseNumber,omitempty"`
	// City is the city name.
	City string `json:"city,omitempty"`
	// PostalCode is the postal/zip code.
	PostalCode string `json:"postalCode,omitempty"`
	// Country is the ISO 3166-1 alpha-2 country code.
	Country string `json:"country,omitempty"`
}

// PickupRequest is the input to BookPickup and UpdatePickup.
type PickupRequest struct {
	// Carrier is the carrier key (e.g. "bring", "gls").
	Carrier string `json:"carrier"`
	// Pickup describes the requested collection window and location details.
	Pickup PickupWindow `json:"pickup"`
	// Contact is the person to reach at the pickup location.
	Contact PickupContact `json:"contact"`
	// Address is the collection address. Omit to use the carrier's configured sender address.
	Address PickupAddress `json:"address,omitempty"`
	// EstimatedParcels is the approximate number of parcels to be collected.
	EstimatedParcels int `json:"estimatedParcels,omitempty"`
	// EstimatedWeight is the approximate total weight in kg.
	EstimatedWeight float64 `json:"estimatedWeight,omitempty"`
	// TrackingNumbers holds carrier item IDs required by carriers that reference
	// already-booked shipments at pickup scheduling time (e.g. PostNord /v3/pickups/ids).
	TrackingNumbers []string `json:"trackingNumbers,omitempty"`
}

// PickupResponse is returned after a successful BookPickup or UpdatePickup call.
type PickupResponse struct {
	// Carrier is the carrier key.
	Carrier string `json:"carrier"`
	// ConfirmationNumber is the carrier-issued pickup reference.
	ConfirmationNumber string `json:"confirmationNumber"`
	// Date is the confirmed collection date (YYYY-MM-DD).
	Date string `json:"date"`
	// ReadyTime is the confirmed earliest collection time from the carrier (HH:MM).
	// May differ from the requested time, or be absent if the carrier does not return a window.
	ReadyTime string `json:"readyTime,omitempty"`
	// CloseTime is the confirmed latest collection time from the carrier (HH:MM).
	// May differ from the requested time, or be absent if the carrier does not return a window.
	CloseTime string `json:"closeTime,omitempty"`
	// Status is "booked" for BookPickup and "updated" for UpdatePickup.
	Status string `json:"status"`
}

// ManifestRequest is the input to CloseManifest.
type ManifestRequest struct {
	// Carrier is the carrier key.
	Carrier string `json:"carrier"`
	// Date is the shipping day to close in ISO 8601 format (YYYY-MM-DD).
	Date string `json:"date"`
	// TrackingNumbers is the list of shipment tracking numbers to include.
	// Required for carriers that need an explicit list; ignored by carriers
	// that close all open shipments for the account automatically.
	TrackingNumbers []string `json:"trackingNumbers,omitempty"`
}

// ManifestResponse is returned by CloseManifest.
type ManifestResponse struct {
	// Carrier is the carrier key.
	Carrier string `json:"carrier"`
	// Date is the shipping day that was closed.
	Date string `json:"date"`
	// Status is always "closed" on success.
	Status string `json:"status"`
	// ParcelsConfirmed is the number of parcels confirmed in the manifest,
	// where returned by the carrier.
	ParcelsConfirmed int `json:"parcelsConfirmed,omitempty"`
	// ManifestDocument is the base64-encoded manifest document, where returned by the carrier.
	ManifestDocument string `json:"manifestDocument,omitempty"`
	// ManifestDocumentFormat is the format of ManifestDocument (e.g. "PDF").
	ManifestDocumentFormat string `json:"manifestDocumentFormat,omitempty"`
	// Warnings holds any non-fatal issues encountered during the close.
	Warnings []string `json:"warnings"`
}

// PickupSlot is a single available collection window returned by GetPickupAvailability.
type PickupSlot struct {
	// Date is the available collection date in ISO 8601 format (YYYY-MM-DD).
	Date string `json:"date"`
	// StartTime is the earliest collection time in HH:MM format.
	StartTime string `json:"startTime"`
	// EndTime is the latest collection time in HH:MM format.
	EndTime string `json:"endTime"`
}

// PickupAvailabilityRequest is the input to GetPickupAvailability.
type PickupAvailabilityRequest struct {
	// Carrier is the carrier key (e.g. "omniva").
	Carrier string `json:"carrier"`
	// Address is the address where availability is checked.
	Address PickupAddress `json:"address"`
}

// PickupAvailabilityResponse holds available collection timeslots.
type PickupAvailabilityResponse struct {
	// Carrier is the carrier key.
	Carrier string `json:"carrier"`
	// Slots is the list of available collection windows, ordered by date/time.
	Slots []PickupSlot `json:"slots"`
}

// ManifestAdapter is implemented by carriers that support pickup scheduling
// or end-of-day manifest operations. It is kept separate from CarrierAdapter
// so carriers that do not support these operations are not required to implement it.
//
// The handler type-asserts a CarrierAdapter to ManifestAdapter at request time.
// If the assertion fails, the carrier does not implement this interface and the
// handler returns HTTP 501. If the assertion succeeds but the carrier returns
// ErrNotSupported for a specific method, the handler also returns HTTP 501.
type ManifestAdapter interface {
	// BookPickup schedules a carrier collection at the warehouse.
	BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error)

	// UpdatePickup modifies a previously scheduled pickup identified by confirmationNumber.
	UpdatePickup(ctx context.Context, confirmationNumber string, req PickupRequest) (*PickupResponse, error)

	// CancelPickup cancels a previously scheduled pickup identified by confirmationNumber.
	CancelPickup(ctx context.Context, carrier, confirmationNumber string) error

	// CloseManifest closes the shipping day for a carrier and returns the handover
	// document where available. For carriers such as GLS that require an explicit
	// close call before the driver arrives, this also submits that instruction.
	CloseManifest(ctx context.Context, req ManifestRequest) (*ManifestResponse, error)

	// GetPickupAvailability returns the available collection timeslots for the
	// given address. Callers should invoke this before BookPickup to select a
	// valid window, avoiding availability-zone errors from the carrier.
	// Carriers that do not require pre-flight availability checks return
	// ErrNotSupported; callers may proceed to BookPickup directly in that case.
	GetPickupAvailability(ctx context.Context, req PickupAvailabilityRequest) (*PickupAvailabilityResponse, error)
}
