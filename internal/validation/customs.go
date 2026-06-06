// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/customs.go.
package validation

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
)

// nonEUDestinations is the set of country codes that are not part of the EU
// customs union and therefore require full customs declarations.
var nonEUDestinations = map[string]bool{
	"NO": true, // Norway — EEA but not EU customs union
	"CH": true, // Switzerland
	"GB": true, // United Kingdom
	"IS": true, // Iceland
	"LI": true, // Liechtenstein
	"US": true, // United States
	"CA": true, // Canada
	"AU": true, // Australia
	"JP": true, // Japan
	"CN": true, // China
}

// euCountries is the set of EU member state country codes.
var euCountries = map[string]bool{
	"AT": true, "BE": true, "BG": true, "CY": true, "CZ": true,
	"DE": true, "DK": true, "EE": true, "ES": true, "FI": true,
	"FR": true, "GR": true, "HR": true, "HU": true, "IE": true,
	"IT": true, "LT": true, "LU": true, "LV": true, "MT": true,
	"NL": true, "PL": true, "PT": true, "RO": true, "SE": true,
	"SI": true, "SK": true,
}

// validIncoterms is the set of accepted Incoterms 2020 trade terms.
var validIncoterms = map[string]bool{
	"EXW": true, "FCA": true, "CPT": true, "CIP": true,
	"DAP": true, "DPU": true, "DDP": true, "FAS": true,
	"FOB": true, "CFR": true, "CIF": true,
}

// seaOnlyIncoterms is the set of Incoterms 2020 terms that are only valid
// for sea and inland waterway transport.
var seaOnlyIncoterms = map[string]bool{
	"FAS": true,
	"FOB": true,
	"CFR": true,
	"CIF": true,
}

// validTransportModes is the set of accepted transport mode values.
var validTransportModes = map[string]bool{
	"sea":  true,
	"air":  true,
	"road": true,
	"rail": true,
}

// deMinimisThreshold holds the de minimis value and expected currency for a
// destination country.
type deMinimisThreshold struct {
	value    float64
	currency string
}

var deMinimisThresholds = map[string]deMinimisThreshold{
	"NO": {value: 350.0, currency: "NOK"},
	"US": {value: 800.0, currency: "USD"},
	"CA": {value: 150.0, currency: "CAD"},
	"AU": {value: 1000.0, currency: "AUD"},
	"GB": {value: 135.0, currency: "GBP"},
	"CH": {value: 65.0, currency: "CHF"},
	"JP": {value: 10000.0, currency: "JPY"},
}

// validISO4217 is a representative set of ISO 4217 currency codes.
var validISO4217 = map[string]bool{
	"DKK": true, "SEK": true, "NOK": true, "EUR": true,
	"PLN": true, "GBP": true, "CHF": true, "USD": true,
	"ISK": true, "CAD": true, "AUD": true, "JPY": true,
	"CNY": true,
}

// vatFormats maps country codes to a regex that validates the VAT number format.
var vatFormats = map[string]*regexp.Regexp{
	"DK": regexp.MustCompile(`^\d{8}$`),
	"SE": regexp.MustCompile(`^SE\d{10}$`),
	"FI": regexp.MustCompile(`^\d{8}$`),
	"NO": regexp.MustCompile(`^\d{9}$`),
	"DE": regexp.MustCompile(`^DE\d{9}$`),
	"FR": regexp.MustCompile(`^FR[A-Z0-9]{2}\d{9}$`),
	"NL": regexp.MustCompile(`^NL\d{9}B\d{2}$`),
	"PL": regexp.MustCompile(`^\d{10}$`),
}

// prohibitedHSPrefixes lists HS chapter prefixes that require special
// permits for import into Norway.
var prohibitedHSPrefixes = []string{"22", "24"}

// hsCodeRegex matches a valid 6-10 digit HS code. Compiled once at startup
// rather than on every call to validateHSCode.
var hsCodeRegex = regexp.MustCompile(`^\d{6,10}$`)

// euDeMinimisEUR is the de minimis threshold for EU B2C shipments.
const euDeMinimisEUR = 150.0

// ValidateCustoms validates the Customs block for a shipment from origin to
// destination with the given shipment type ("B2B" or "B2C").
func ValidateCustoms(c adapter.Customs, origin, destination, shipmentType string) error {
	if destination == "AX" {
		return fmt.Errorf("åland Islands (AX) require special VAT handling: contact your carrier")
	}

	if err := validateTransportMode(c); err != nil {
		return err
	}

	if nonEUDestinations[destination] {
		return validateNonEUCustoms(c, origin, destination, shipmentType)
	}

	if euCountries[destination] {
		return validateEUCustoms(c, destination, shipmentType)
	}

	return nil
}

// validateTransportMode checks that TransportMode is recognised and that
// sea-only Incoterms are not used with non-sea modes.
func validateTransportMode(c adapter.Customs) error {
	if c.TransportMode == "" {
		return nil
	}

	mode := strings.ToLower(c.TransportMode)
	if !validTransportModes[mode] {
		return fmt.Errorf(
			"invalid transport mode %q: accepted values are sea, air, road, rail",
			c.TransportMode,
		)
	}

	if c.Incoterms != "" && seaOnlyIncoterms[c.Incoterms] && mode != "sea" {
		return fmt.Errorf(
			"incoterms %s is only valid for sea transport; shipment transport mode is %q",
			c.Incoterms, mode,
		)
	}

	return nil
}

// validateNonEUCustoms enforces full customs declaration rules for
// destinations outside the EU customs union.
func validateNonEUCustoms(c adapter.Customs, origin, destination, shipmentType string) error {
	if strings.EqualFold(shipmentType, "B2C") {
		if threshold, ok := deMinimisThresholds[destination]; ok && threshold.value > 0 {
			switch {
			case c.CustomsCurrency == threshold.currency && c.CustomsValue <= threshold.value:
				return nil
			case c.CustomsCurrency != threshold.currency && c.CustomsCurrency != "":
				return fmt.Errorf(
					"%w: cannot determine %s de minimis without %s value (got %s)",
					ErrReviewRequired, destination, threshold.currency, c.CustomsCurrency,
				)
			}
		}
	}

	if c.Incoterms == "" {
		return fmt.Errorf("incoterms is required for shipments to %s", destination)
	}
	if !validIncoterms[c.Incoterms] {
		return fmt.Errorf("invalid incoterms %q: must be one of EXW FCA CPT CIP DAP DPU DDP FAS FOB CFR CIF", c.Incoterms)
	}

	if c.HSCode == "" {
		return fmt.Errorf("HS code is required for shipments to %s", destination)
	}
	if err := validateHSCode(c.HSCode); err != nil {
		return err
	}

	if destination == "NO" {
		for _, prefix := range prohibitedHSPrefixes {
			if strings.HasPrefix(c.HSCode, prefix) {
				return fmt.Errorf(
					"HS code %s (chapter %s) requires a special import permit for Norway",
					c.HSCode, prefix,
				)
			}
		}
	}

	if c.ImporterOfRecord == "" {
		return fmt.Errorf("importer of record is required for shipments to %s", destination)
	}

	if c.ExporterVATNumber == "" {
		return fmt.Errorf("exporter VAT number is required for shipments from %s", origin)
	}
	if err := validateVATNumber(c.ExporterVATNumber, origin); err != nil {
		return fmt.Errorf("invalid exporter VAT number: %w", err)
	}

	if c.CustomsValue <= 0 {
		return fmt.Errorf("customs value must be greater than 0")
	}

	if c.CustomsCurrency == "" {
		return fmt.Errorf("customs currency is required")
	}
	if !validISO4217[c.CustomsCurrency] {
		return fmt.Errorf("invalid customs currency: %q is not a recognised ISO 4217 code", c.CustomsCurrency)
	}

	return nil
}

// validateEUCustoms enforces intra-EU customs and VAT rules.
func validateEUCustoms(c adapter.Customs, destination, shipmentType string) error {
	if strings.EqualFold(shipmentType, "B2C") {
		if c.CustomsCurrency == "EUR" && c.CustomsValue <= euDeMinimisEUR {
			return nil
		}
		if c.CustomsCurrency != "EUR" && c.CustomsCurrency != "" {
			return fmt.Errorf("%w: cannot determine EU de minimis without EUR value", ErrReviewRequired)
		}
	}

	if strings.EqualFold(shipmentType, "B2B") {
		if c.ImporterVATNumber == "" {
			return fmt.Errorf("importer VAT number is required for B2B shipments to %s", destination)
		}
		if err := validateVATNumber(c.ImporterVATNumber, destination); err != nil {
			return fmt.Errorf("invalid importer VAT number: %w", err)
		}
	}

	if c.CustomsCurrency == "EUR" && c.CustomsValue > euDeMinimisEUR && c.HSCode == "" {
		return fmt.Errorf("HS code is required for EU shipments with customs value > %.0f EUR", euDeMinimisEUR)
	}
	if c.HSCode != "" {
		if err := validateHSCode(c.HSCode); err != nil {
			return err
		}
	}

	return nil
}

// validateHSCode checks that the HS code is 6-10 digits.
// Uses a package-level compiled regex to avoid recompilation on every call.
func validateHSCode(code string) error {
	if !hsCodeRegex.MatchString(code) {
		return fmt.Errorf("HS code must be 6-10 digits, got %q", code)
	}
	return nil
}

// validateVATNumber validates a VAT number against the known format for
// the given country code. Returns nil if no format rule exists for the country.
func validateVATNumber(number, country string) error {
	rule, ok := vatFormats[country]
	if !ok {
		return nil
	}
	if !rule.MatchString(number) {
		return fmt.Errorf("invalid %s VAT number format: %q", countryName(country), number)
	}
	return nil
}
