// Package handler provides the HTTP handler for booking shipments.
// This file is located at /internal/handler/bookings.go.
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
	"github.com/kristiannissen/logistics-gateway/internal/parser"
	"github.com/kristiannissen/logistics-gateway/internal/validation"
)

// BookShipment handles POST /bookings.
func (c *Config) BookShipment(w http.ResponseWriter, r *http.Request) {
	log := c.loggerFor(r)

	if r.Method != http.MethodPost {
		c.writeError(w, r, http.StatusMethodNotAllowed, "method not allowed", "only POST is supported")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	p, err := parser.ForRequest(r)
	if err != nil {
		c.writeError(w, r, http.StatusUnsupportedMediaType, "unsupported content type", err.Error())
		return
	}

	request, err := p.Parse(body)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "failed to parse request", err.Error())
		return
	}

	flagged, err := validateBookingRequest(request)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "validation failed", err.Error())
		return
	}

	if flagged {
		log.Warn("shipment flagged for manual review",
			zap.String("carrier", request.Carrier),
			zap.String("idempotencyKey", request.IdempotencyKey),
			zap.String("senderPostalCode", request.Shipment.Sender.PostalCode),
			zap.String("receiverPostalCode", request.Shipment.Receiver.PostalCode),
		)
	}

	carrierAdapter, err := c.selectAdapter(request.Carrier)
	if err != nil {
		c.writeError(w, r, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	response, err := carrierAdapter.BookShipment(r.Context(), *request)
	if err != nil {
		log.Error("failed to book shipment",
			zap.Error(err),
			zap.String("carrier", request.Carrier),
			zap.String("idempotencyKey", request.IdempotencyKey),
		)
		c.writeError(w, r, http.StatusInternalServerError, "booking failed", err.Error())
		return
	}

	response.FlaggedForReview = flagged

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error("failed to write response", zap.Error(err))
	}
}

// validateBookingRequest runs all stateless validation rules against the
// request. It returns (flagged, error) where flagged is true when the request
// passes hard validation but contains an address that could not be fully
// verified (rural/unrecognised format) and should be reviewed manually.
func validateBookingRequest(request *adapter.BookingRequest) (flagged bool, err error) {
	if request.Carrier == "" {
		return false, fmt.Errorf("carrier is required")
	}

	if request.Shipment.Sender.Name == "" || request.Shipment.Sender.Street == "" ||
		request.Shipment.Sender.City == "" || request.Shipment.Sender.Country == "" {
		return false, fmt.Errorf("sender address is incomplete")
	}
	if request.Shipment.Receiver.Name == "" || request.Shipment.Receiver.Street == "" ||
		request.Shipment.Receiver.City == "" || request.Shipment.Receiver.Country == "" {
		return false, fmt.Errorf("receiver address is incomplete")
	}

	if len(request.Shipment.Colli) == 0 {
		return false, fmt.Errorf("shipment must have at least one colli")
	}
	if request.Shipment.TotalWeight <= 0 {
		return false, fmt.Errorf("total weight must be greater than 0")
	}
	for i, colli := range request.Shipment.Colli {
		if colli.Weight <= 0 {
			return false, fmt.Errorf("colli %d: weight must be greater than 0", i)
		}
		for j, item := range colli.Items {
			if item.Weight <= 0 {
				return false, fmt.Errorf("colli %d, item %d: weight must be greater than 0", i, j)
			}
			if item.Quantity <= 0 {
				return false, fmt.Errorf("colli %d, item %d: quantity must be greater than 0", i, j)
			}
		}
	}
	var colliTotalWeight float64
	for _, colli := range request.Shipment.Colli {
		colliTotalWeight += colli.Weight
	}
	if colliTotalWeight != request.Shipment.TotalWeight {
		return false, fmt.Errorf("total weight does not match sum of colli weights (expected %.2f, got %.2f)",
			colliTotalWeight, request.Shipment.TotalWeight)
	}

	// Idempotency key.
	if err := validation.ValidateIdempotencyKey(request.IdempotencyKey); err != nil {
		return false, err
	}

	// Address validation — sender and receiver.
	for _, party := range []struct {
		label string
		addr  adapter.Address
	}{
		{"sender", request.Shipment.Sender},
		{"receiver", request.Shipment.Receiver},
	} {
		if err := validation.ValidateAddress(party.addr, request.Carrier, party.addr.Country); err != nil {
			if validation.IsReviewRequired(err) {
				flagged = true
				continue
			}
			return false, fmt.Errorf("%s: %w", party.label, err)
		}
	}

	// Package validation — weight, dimensions, girth, colli count.
	if err := validation.ValidateShipment(request.Carrier, request.Shipment); err != nil {
		return false, err
	}

	// Customs validation — only when a Customs block is present.
	c := request.Shipment.Customs
	if c.Incoterms != "" || c.HSCode != "" || c.CustomsValue > 0 ||
		c.ImporterOfRecord != "" || c.ImporterVATNumber != "" || c.ExporterVATNumber != "" {
		if err := validation.ValidateCustoms(
			c,
			request.Shipment.Sender.Country,
			request.Shipment.Receiver.Country,
			c.ShipmentType,
		); err != nil {
			if validation.IsReviewRequired(err) {
				flagged = true
			} else {
				return false, err
			}
		}
	}

	return flagged, nil
}
