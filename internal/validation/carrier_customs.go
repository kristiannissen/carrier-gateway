// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/carrier_customs.go.
package validation

import (
	"fmt"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// carrierCustomsRule holds carrier-specific constraints on the customs block.
type carrierCustomsRule struct {
	// maxItems is the maximum number of customs line items the carrier accepts.
	// 0 means no limit enforced pre-flight.
	maxItems int
	// requiresEORIForNonEU indicates the carrier requires an EORI or VAT number
	// on the exporter side for every non-EU shipment.
	requiresEORIForNonEU bool
}

// carrierCustomsRules maps carrier keys to their pre-flight customs constraints.
// Values are sourced from carrier API documentation and terms of service.
var carrierCustomsRules = map[string]carrierCustomsRule{
	// DHL cCustoms API: maximum 99 items per consignment (wire-format limit).
	"dhl": {maxItems: 99, requiresEORIForNonEU: true},
	// PostNord CN22/CN23: maximum 5 items; the adapter truncates with a warning
	// but pre-flight validation surfaces this earlier.
	"postnord": {maxItems: 5, requiresEORIForNonEU: false},
	// GLS Customs API v3: no documented item cap enforced pre-flight;
	// the API validates server-side.
	"gls": {maxItems: 0, requiresEORIForNonEU: false},
}

// ValidateCarrierCustomsRules checks carrier-specific constraints on the customs
// block before it is forwarded to the carrier API. It complements the
// carrier-agnostic rules in ValidateCustoms.
//
// Returns nil when no rules are defined for the carrier or all rules pass.
func ValidateCarrierCustomsRules(carrier string, c adapter.Customs) error {
	rule, ok := carrierCustomsRules[carrier]
	if !ok {
		return nil
	}

	if rule.maxItems > 0 && len(c.Items) > rule.maxItems {
		return fmt.Errorf(
			"%s customs: %d items exceeds the carrier limit of %d per consignment; split into multiple shipments",
			carrier, len(c.Items), rule.maxItems,
		)
	}

	if rule.requiresEORIForNonEU && c.ExporterVATNumber == "" {
		return fmt.Errorf(
			"%s customs: exporter VAT or EORI number is required for non-EU shipments",
			carrier,
		)
	}

	return nil
}
