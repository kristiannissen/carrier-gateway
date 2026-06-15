// Package adapter provides the ReturnAdapter interface and return-related types.
// This file is located at /internal/adapter/adapter_returns.go.
package adapter

import "context"

// ReturnRequest is the input to BookReturn for carriers that support
// consumer return shipments. The Sender is the customer returning the parcel;
// the Receiver is the merchant or fulfilment destination.
type ReturnRequest struct {
	// Carrier is the carrier key (e.g. "inpost").
	Carrier string `json:"carrier"`
	// Sender is the customer returning the parcel.
	Sender Address `json:"sender"`
	// Receiver is the merchant or warehouse address the parcel is returned to.
	// Optional — carriers that derive the destination from org settings may omit it.
	Receiver Address `json:"receiver,omitempty"`
	// Colli describes the parcels being returned. Optional — if absent the carrier
	// applies default dimensions from the organisation account settings.
	// InPost accepts exactly one colli; the first element is used when multiple are provided.
	Colli []Colli `json:"colli,omitempty"`
	// ExpiresAt is the RFC3339 datetime at which the return shipment expires.
	// Optional. Minimum is 7 days from request time. Carriers that do not support
	// expiration ignore this field.
	ExpiresAt string `json:"expiresAt,omitempty"`
	// EnableDropOffCode, when true, allows the customer to drop off the parcel
	// using a short numeric code without printing a label.
	// Defaults to true for InPost.
	EnableDropOffCode bool `json:"enableDropOffCode,omitempty"`
}

// ReturnResponse is returned after a successful BookReturn call.
type ReturnResponse struct {
	// ShipmentID is the carrier-internal return shipment UUID.
	ShipmentID string `json:"shipmentId"`
	// TrackingNumber is the long parcel tracking number.
	TrackingNumber string `json:"trackingNumber"`
	// DropOffCode is the short numeric code for label-less drop-off.
	// Only populated when EnableDropOffCode was true.
	DropOffCode string `json:"dropOffCode,omitempty"`
	// Carrier is the carrier key.
	Carrier string `json:"carrier"`
	// Status is always "booked" on success.
	Status string `json:"status"`
	// ExpiresAt is the RFC3339 datetime at which the return shipment expires,
	// as confirmed by the carrier.
	ExpiresAt string `json:"expiresAt,omitempty"`
}

// ReturnShipmentInfo is the return shipment detail returned by GetReturnShipment.
type ReturnShipmentInfo struct {
	// ID is the carrier-internal return shipment UUID.
	ID string `json:"id"`
	// Carrier is the carrier key.
	Carrier string `json:"carrier"`
	// ExpirationDate is the RFC3339 datetime at which the return expires.
	ExpirationDate string `json:"expirationDate,omitempty"`
	// Parcels is the list of parcels in the return shipment.
	Parcels []ReturnParcelInfo `json:"parcels,omitempty"`
}

// ReturnParcelInfo describes a single parcel within a return shipment.
type ReturnParcelInfo struct {
	// ID is the carrier-internal parcel UUID.
	ID string `json:"id"`
	// TrackingNumber is the long parcel tracking number.
	TrackingNumber string `json:"trackingNumber"`
	// DropOffCode is the short numeric code for label-less drop-off.
	// Only populated when the return was created with enableDropOffCode=true.
	DropOffCode string `json:"dropOffCode,omitempty"`
}

// ReturnQuerier is implemented by carriers that support querying return shipments.
// It is kept separate from ReturnAdapter so carriers that support booking but not
// querying do not need to implement it.
//
// The handler type-asserts a CarrierAdapter to ReturnQuerier at request time.
// If the assertion fails the carrier does not support return queries and the
// handler returns HTTP 501.
type ReturnQuerier interface {
	// GetReturnShipment retrieves details for a single return shipment by its ID.
	// shipmentID is the UUID returned by BookReturn (ReturnResponse.ShipmentID).
	GetReturnShipment(ctx context.Context, shipmentID string) (*ReturnShipmentInfo, error)
}

// ReturnAdapter is implemented by carriers that support return shipment booking
// via API. It is kept separate from CarrierAdapter so carriers that do not
// support returns are not required to implement it.
//
// The handler type-asserts a CarrierAdapter to ReturnAdapter at request time.
// If the assertion fails the carrier does not support returns and the handler
// returns HTTP 501. If the carrier returns ErrNotSupported the handler also
// returns HTTP 501.
type ReturnAdapter interface {
	// BookReturn creates a new return shipment and returns the tracking number
	// and, when requested, a short drop-off code for label-less collection.
	BookReturn(ctx context.Context, req ReturnRequest) (*ReturnResponse, error)

	// FetchReturnLabel retrieves the shipping label for a return parcel.
	// The label endpoint differs from the standard shipment label endpoint.
	FetchReturnLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error)
}
