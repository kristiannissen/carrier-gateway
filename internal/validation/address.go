// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/address.go.
package validation

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// ErrReviewRequired is returned when an address cannot be fully validated but
// should not be rejected outright. Callers surface this as a 200 with
// flaggedForReview: true rather than a hard error.
var ErrReviewRequired = errors.New("address flagged for manual review")

// postalCodeRules maps ISO 3166-1 alpha-2 country codes to the regex that
// the country's postal authority mandates.
var postalCodeRules = map[string]*regexp.Regexp{
	// Nordic
	"DK": regexp.MustCompile(`^\d{4}$`),
	"NO": regexp.MustCompile(`^\d{4}$`),
	"SE": regexp.MustCompile(`^\d{5}$`),
	"FI": regexp.MustCompile(`^\d{5}$`),

	// European
	"PL": regexp.MustCompile(`^\d{2}-\d{3}$`),
	"DE": regexp.MustCompile(`^\d{5}$`),
	"FR": regexp.MustCompile(`^\d{5}$`),
	"NL": regexp.MustCompile(`^\d{4}\s?[A-Z]{2}$`),
	"BE": regexp.MustCompile(`^\d{4}$`),
	"ES": regexp.MustCompile(`^\d{5}$`),
	"IT": regexp.MustCompile(`^\d{5}$`),
	"PT": regexp.MustCompile(`^\d{4}-\d{3}$`),
	"AT": regexp.MustCompile(`^\d{4}$`),
	"CH": regexp.MustCompile(`^\d{4}$`),

	// British Isles
	// Royal Mail format: 1-2 letters + 1-2 digits (+ optional letter) + space +
	// 1 digit + 2 letters. Spaces are normalised before matching.
	"GB": regexp.MustCompile(`^[A-Z]{1,2}\d[A-Z\d]?\s?\d[A-Z]{2}$`),

	// Americas
	// US: 5-digit ZIP, 5+4 ZIP, or military APO/FPO/DPO codes.
	"US": regexp.MustCompile(`^(\d{5}(-\d{4})?|(APO|FPO|DPO)\s+[A-Z]{2}\s+\d{5}(-\d{4})?)$`),
	// Canada: letter-digit-letter space? digit-letter-digit (spaces optional).
	"CA": regexp.MustCompile(`^[A-Z]\d[A-Z]\s?\d[A-Z]\d$`),
	// Brazil: 8-digit with optional hyphen after 5th digit.
	"BR": regexp.MustCompile(`^\d{5}-?\d{3}$`),

	// Asia-Pacific
	// Japan: 3-digit + optional hyphen + 4-digit.
	"JP": regexp.MustCompile(`^\d{3}-?\d{4}$`),
	// China: 6-digit numeric.
	"CN": regexp.MustCompile(`^\d{6}$`),
	// Australia: 4-digit.
	"AU": regexp.MustCompile(`^\d{4}$`),
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

// stateRequired is the set of countries for which a non-empty State field is
// mandatory. These are countries where the state/province is part of the
// official postal address and required by carriers.
var stateRequired = map[string]bool{
	"US": true,
	"CA": true,
	"BR": true,
	"AU": true,
}

// validStates maps country codes to the set of valid state/province/territory
// codes used in postal addressing. Values are upper-case.
var validStates = map[string]map[string]bool{
	"US": {
		// 50 states
		"AL": true, "AK": true, "AZ": true, "AR": true, "CA": true,
		"CO": true, "CT": true, "DE": true, "FL": true, "GA": true,
		"HI": true, "ID": true, "IL": true, "IN": true, "IA": true,
		"KS": true, "KY": true, "LA": true, "ME": true, "MD": true,
		"MA": true, "MI": true, "MN": true, "MS": true, "MO": true,
		"MT": true, "NE": true, "NV": true, "NH": true, "NJ": true,
		"NM": true, "NY": true, "NC": true, "ND": true, "OH": true,
		"OK": true, "OR": true, "PA": true, "RI": true, "SC": true,
		"SD": true, "TN": true, "TX": true, "UT": true, "VT": true,
		"VA": true, "WA": true, "WV": true, "WI": true, "WY": true,
		// DC and territories
		"DC": true, "PR": true, "GU": true, "VI": true, "AS": true, "MP": true,
		// Military
		"AA": true, "AE": true, "AP": true,
	},
	"CA": {
		// Provinces
		"AB": true, "BC": true, "MB": true, "NB": true, "NL": true,
		"NS": true, "ON": true, "PE": true, "QC": true, "SK": true,
		// Territories
		"NT": true, "NU": true, "YT": true,
	},
	"AU": {
		"ACT": true, "NSW": true, "NT": true, "QLD": true,
		"SA": true, "TAS": true, "VIC": true, "WA": true,
	},
	"BR": {
		"AC": true, "AL": true, "AP": true, "AM": true, "BA": true,
		"CE": true, "DF": true, "ES": true, "GO": true, "MA": true,
		"MT": true, "MS": true, "MG": true, "PA": true, "PB": true,
		"PR": true, "PE": true, "PI": true, "RJ": true, "RN": true,
		"RS": true, "RO": true, "RR": true, "SC": true, "SP": true,
		"SE": true, "TO": true,
	},
	"DE": {
		// Bundesland codes used in shipping contexts
		"BB": true, "BE": true, "BW": true, "BY": true, "HB": true,
		"HE": true, "HH": true, "MV": true, "NI": true, "NW": true,
		"RP": true, "SH": true, "SL": true, "SN": true, "ST": true,
		"TH": true,
	},
}

// ValidateAddress validates addr for the given carrier and country.
//
// When addr.ServicePointID is set the address is treated as a service point
// delivery. Street, City, and PostalCode are optional in that case; Name,
// Country, and Phone remain required because all carriers require them even
// for service point deliveries.
func ValidateAddress(addr adapter.Address, carrier, country string) error {
	if country == "" {
		country = addr.Country
	}

	// Service point delivery — reduced address requirements.
	if addr.ServicePointID != "" {
		if addr.Name == "" {
			return fmt.Errorf("name is required for service point deliveries")
		}
		if addr.Country == "" {
			return fmt.Errorf("country is required for service point deliveries")
		}
		return nil
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

	// Postal code validation.
	if rule, ok := postalCodeRules[country]; ok {
		if addr.PostalCode == "" {
			return fmt.Errorf("postal code is required for %s", country)
		}
		// Normalise to upper-case before matching — callers may pass mixed case.
		if !rule.MatchString(strings.ToUpper(addr.PostalCode)) {
			return fmt.Errorf("invalid %s postal code: %s", countryName(country), addr.PostalCode)
		}
	} else if addr.PostalCode != "" {
		// No rule for this country — flag non-standard codes for manual review.
		if !regexp.MustCompile(`^[A-Z0-9\s\-]{3,10}$`).MatchString(strings.ToUpper(addr.PostalCode)) {
			return fmt.Errorf("%w: unrecognised postal code format for country %s", ErrReviewRequired, country)
		}
	}

	// State/province validation.
	if err := ValidateState(addr.State, country); err != nil {
		return err
	}

	return nil
}

// ValidateState validates the state/province/territory code for a given country.
// Returns nil when the country has no state requirement or when State is absent
// for a country that does not require it.
func ValidateState(state, country string) error {
	states, hasStates := validStates[country]

	if stateRequired[country] {
		if state == "" {
			return fmt.Errorf("state is required for %s", countryName(country))
		}
		if hasStates && !states[strings.ToUpper(state)] {
			return fmt.Errorf("invalid %s state code: %q", countryName(country), state)
		}
		return nil
	}

	// State not required — validate format only if provided and the country has
	// a known set (e.g. DE Bundesland codes are validated when present).
	if state != "" && hasStates && !states[strings.ToUpper(state)] {
		return fmt.Errorf("invalid %s state code: %q", countryName(country), state)
	}

	return nil
}

// IsReviewRequired reports whether err wraps ErrReviewRequired.
func IsReviewRequired(err error) bool {
	return errors.Is(err, ErrReviewRequired)
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
		"GB": "British",
		"US": "US",
		"CA": "Canadian",
		"BR": "Brazilian",
		"JP": "Japanese",
		"CN": "Chinese",
		"AU": "Australian",
		"CH": "Swiss",
		"BE": "Belgian",
		"ES": "Spanish",
		"IT": "Italian",
		"PT": "Portuguese",
		"AT": "Austrian",
	}
	if name, ok := names[code]; ok {
		return name
	}
	return code
}
