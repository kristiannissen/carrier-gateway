// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/package.go.
package validation

import (
	"fmt"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
)

// carrierLimits defines the physical constraints for a carrier's parcels.
// All dimension fields are in centimetres; weight in kilograms.
// A zero value means the constraint is not enforced for that carrier.
type carrierLimits struct {
	// maxWeightKg is the maximum weight per colli.
	maxWeightKg float64

	// maxLength, maxWidth, maxHeight are individual dimension caps.
	// Zero means unconstrained.
	maxLength float64
	maxWidth  float64
	maxHeight float64

	// maxDimensionSum is the PostNord-style L+W+H combined cap.
	// Zero means unconstrained.
	maxDimensionSum float64

	// maxGirth is the standard girth formula: 2*(W+H)+L.
	// Zero means unconstrained.
	maxGirth float64

	// maxColli is the maximum number of colli per shipment.
	// Zero means unconstrained.
	maxColli int
}

// limits maps carrier keys to their physical constraints.
// Adding a new carrier (e.g. "fedex") is one new entry here.
var limits = map[string]carrierLimits{
	"postnord": {
		maxWeightKg:     30,
		maxDimensionSum: 300,
		maxGirth:        300,
		maxColli:        5,
	},
	"bring": {
		maxWeightKg: 30,
		maxLength:   250,
		maxWidth:    120,
		maxHeight:   100,
	},
	"gls": {
		maxWeightKg: 40,
		maxLength:   270,
		maxWidth:    120,
		maxHeight:   120,
		maxGirth:    400,
	},
	"dao": {
		maxWeightKg: 35,
		maxLength:   250,
		maxWidth:    120,
		maxHeight:   120,
	},
	"posti": {
		maxWeightKg: 30,
		maxLength:   200,
		maxWidth:    100,
		maxHeight:   100,
		maxGirth:    300,
	},
	"inpost": {},
}

// carrierName returns the display name for a carrier key used in error messages.
func carrierName(carrier string) string {
	names := map[string]string{
		"postnord": "PostNord",
		"bring":    "Bring",
		"gls":      "GLS",
		"dao":      "DAO",
		"posti":    "Posti",
		"inpost":   "InPost",
	}
	if name, ok := names[carrier]; ok {
		return name
	}
	return carrier
}

// ValidateShipment validates the entire shipment for the given carrier,
// including the colli count limit and per-colli physical constraints.
// Returns the first validation error encountered.
func ValidateShipment(carrier string, shipment adapter.Shipment) error {
	l, ok := limits[carrier]
	if !ok {
		// Unknown carrier — no physical limits to enforce.
		return nil
	}

	cn := carrierName(carrier)

	if l.maxColli > 0 && len(shipment.Colli) > l.maxColli {
		return fmt.Errorf("%s supports a maximum of %d colli per shipment", cn, l.maxColli)
	}

	for i, colli := range shipment.Colli {
		if err := validateColli(cn, l, i, colli); err != nil {
			return err
		}
	}

	return nil
}

// validateColli applies all physical constraints to a single colli.
func validateColli(cn string, l carrierLimits, index int, colli adapter.Colli) error {
	d := colli.Dimensions

	if l.maxWeightKg > 0 && colli.Weight > l.maxWeightKg {
		return fmt.Errorf(
			"colli %d: weight %.2f kg exceeds %s limit of %.0f kg",
			index, colli.Weight, cn, l.maxWeightKg,
		)
	}

	if l.maxLength > 0 && d.Length > l.maxLength {
		return fmt.Errorf(
			"colli %d: length %.0f cm exceeds %s limit of %.0f cm",
			index, d.Length, cn, l.maxLength,
		)
	}

	if l.maxWidth > 0 && d.Width > l.maxWidth {
		return fmt.Errorf(
			"colli %d: width %.0f cm exceeds %s limit of %.0f cm",
			index, d.Width, cn, l.maxWidth,
		)
	}

	if l.maxHeight > 0 && d.Height > l.maxHeight {
		return fmt.Errorf(
			"colli %d: height %.0f cm exceeds %s limit of %.0f cm",
			index, d.Height, cn, l.maxHeight,
		)
	}

	if l.maxDimensionSum > 0 {
		sum := d.Length + d.Width + d.Height
		if sum > l.maxDimensionSum {
			return fmt.Errorf(
				"colli %d: combined dimensions %.0f cm (L+W+H) exceed %s limit of %.0f cm",
				index, sum, cn, l.maxDimensionSum,
			)
		}
	}

	if l.maxGirth > 0 {
		girth := 2*(d.Width+d.Height) + d.Length
		if girth > l.maxGirth {
			return fmt.Errorf(
				"colli %d: girth %.0f cm (2×(W+H)+L) exceeds %s limit of %.0f cm",
				index, girth, cn, l.maxGirth,
			)
		}
	}

	return nil
}
