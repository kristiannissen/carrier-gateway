// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/address.go.
package validation

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
)

// ReviewRequired is returned when an address cannot be fully validated but
// should not be rejected outright. Callers surface this as a 200 with
// flaggedForReview: true rather than a hard error.
var ReviewRequired = errors.New("address flagged for manual review")

// postalCodeRules maps ISO 3166-1 alpha-2 country codes to the regex that
// the country's postal authority mandates.
var postalCodeRules = map[string]*regexp.Regexp{
	"DK": regexp.MustCompile(`^\d{4}$`),
	"NO": regexp.MustCompile(`^\d{4}$`),
	"SE": regexp.MustCompile(`^\d{5}$`),
	"FI": regexp.MustCompile(`^\d{5}$`),
	"PL": regexp.MustCompile(`^\d{2}-\d{3}$`),
	"DE": regexp.MustCompile(`^\d{5}$`),
	"FR": regexp.MustCompile(`^\d{5}$`),
	"NL": regexp.MustCompile(`^\d{4}\s?[A-Z]{2}$`),
}

// houseNumberRequired is the set of carriers that mandate HouseNumber as a
// distinct field. France is exempted at call time.
var houseNumberRequired = map[string]bool{
	"inpost": true,
	"gls":    true,
	"dao":    true,
}

// nordicCountries is the set of country codes for which a non-empty Street
// is mandatory regardless of carrier.
var nordicCountries = map[string]bool{
	"DK": true,
	"NO": true,
	"SE": true,
	"FI": true,
}

// ValidateAddress validates addr for the given carrier and country.
//
// Rules enforced:
//   - Street is required for Nordic countries (DK, NO, SE, FI).
//   - City is used as municipality proxy for Finland; empty City is an error.
//   - HouseNumber is required when the carrier mandates it, unless country is FR.
//   - Postal code must match the country's known format where a rule exists.
//   - For countries with no known rule, a non-standard postal code returns
//     ReviewRequired so the caller flags the shipment rather than rejects it.
func ValidateAddress(addr adapter.Address, carrier, country string) error {
	if country == "" {
		country = addr.Country
	}

	if nordicCountries[country] && addr.Street == "" {
		return fmt.Errorf("street name is required for %s", country)
	}

	if country == "FI" && addr.City == "" {
		return fmt.Errorf("municipality is required for Finnish addresses")
	}

	if houseNumberRequired[carrier] && country != "FR" && addr.HouseNumber == "" {
		return fmt.Errorf("house number is required for %s shipments to %s", carrier, country)
	}

	if rule, ok := postalCodeRules[country]; ok {
		if addr.PostalCode == "" {
			return fmt.Errorf("postal code is required for %s", country)
		}
		if !rule.MatchString(addr.PostalCode) {
			return fmt.Errorf("invalid %s postal code: %s", countryName(country), addr.PostalCode)
		}
		return nil
	}

	// No rule for this country — flag non-standard codes for manual review.
	if addr.PostalCode != "" && !regexp.MustCompile(`^[A-Z0-9\s\-]{3,10}$`).MatchString(addr.PostalCode) {
		return fmt.Errorf("%w: unrecognised postal code format for country %s", ReviewRequired, country)
	}

	return nil
}

// IsReviewRequired reports whether err wraps ReviewRequired.
func IsReviewRequired(err error) bool {
	return errors.Is(err, ReviewRequired)
}

// countryName returns a human-readable adjective for use in error messages.
func countryName(code string) string {
	names := map[string]string{
		"DK": "Danish",
		"NO": "Norwegian",
		"SE": "Swedish",
		"FI": "Finnish",
		"PL": "Polish",
		"DE": "German",
		"FR": "French",
		"NL": "Dutch",
	}
	if name, ok := names[code]; ok {
		return name
	}
	return code
}
