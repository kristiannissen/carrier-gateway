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
)

// BookShipment handles POST /bookings.
// Request body: BookingRequest (JSON).
// Response: BookingResponse (JSON) or ErrorResponse.
func (c *Config) BookShipment(w http.ResponseWriter, r *http.Request) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		c.writeError(w, http.StatusMethodNotAllowed, "method not allowed", "only POST is supported")
		return
	}

	// Read and parse the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		c.writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Select parser based on Content-Type - JSON, XML, EDIFACT
	p, err := parser.ForRequest(r)
	if err != nil {
		c.writeError(w, http.StatusUnsupportedMediaType, "unsupported content type", err.Error())
		return
	}

	request, err := p.Parse(body)
	if err != nil {
		c.writeError(w, http.StatusBadRequest, "failed to parse request", err.Error())
		return
	}

	// Validate the request
	if err := validateBookingRequest(request); err != nil {
		c.writeError(w, http.StatusBadRequest, "validation failed", err.Error())
		return
	}

	// Get the appropriate carrier adapter
	carrierAdapter, err := c.getAdapter(request.Carrier)
	if err != nil {
		c.writeError(w, http.StatusBadRequest, "unsupported carrier", err.Error())
		return
	}

	// Book the shipment
	response, err := carrierAdapter.BookShipment(r.Context(), *request)
	if err != nil {
		c.Log.Error("failed to book shipment", zap.Error(err), zap.String("carrier", request.Carrier))
		c.writeError(w, http.StatusInternalServerError, "booking failed", err.Error())
		return
	}

	// Return the response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		c.Log.Error("failed to write response", zap.Error(err))
	}
}

// validateBookingRequest validates a BookingRequest.
func validateBookingRequest(request *adapter.BookingRequest) error {
	// Validate carrier
	if request.Carrier == "" {
		return fmt.Errorf("carrier is required")
	}

	// Validate sender address
	if request.Shipment.Sender.Name == "" || request.Shipment.Sender.Street == "" ||
		request.Shipment.Sender.City == "" || request.Shipment.Sender.Country == "" {
		return fmt.Errorf("sender address is incomplete")
	}

	// Validate receiver address
	if request.Shipment.Receiver.Name == "" || request.Shipment.Receiver.Street == "" ||
		request.Shipment.Receiver.City == "" || request.Shipment.Receiver.Country == "" {
		return fmt.Errorf("receiver address is incomplete")
	}

	// Validate colli list (must have at least 1 colli)
	if len(request.Shipment.Colli) == 0 {
		return fmt.Errorf("shipment must have at least one colli")
	}

	// Validate total weight
	if request.Shipment.TotalWeight <= 0 {
		return fmt.Errorf("total weight must be greater than 0")
	}

	// Validate each colli
	for i, colli := range request.Shipment.Colli {
		if colli.Weight <= 0 {
			return fmt.Errorf("colli %d: weight must be greater than 0", i)
		}
		for j, item := range colli.Items {
			if item.Weight <= 0 {
				return fmt.Errorf("colli %d, item %d: weight must be greater than 0", i, j)
			}
			if item.Quantity <= 0 {
				return fmt.Errorf("colli %d, item %d: quantity must be greater than 0", i, j)
			}
		}
	}

	// Validate total weight matches sum of colli weights
	var colliTotalWeight float64
	for _, colli := range request.Shipment.Colli {
		colliTotalWeight += colli.Weight
	}
	if colliTotalWeight != request.Shipment.TotalWeight {
		return fmt.Errorf("total weight does not match sum of colli weights (expected %.2f, got %.2f)",
			colliTotalWeight, request.Shipment.TotalWeight)
	}

	return nil
}
