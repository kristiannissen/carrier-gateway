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
}

// euCountries is the set of EU member state country codes.
// Used to determine de minimis thresholds and VAT rules.
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

// validISO4217 is a representative set of ISO 4217 currency codes relevant
// to Nordic and EU shipments. Extend as needed.
var validISO4217 = map[string]bool{
	"DKK": true, "SEK": true, "NOK": true, "EUR": true,
	"PLN": true, "GBP": true, "CHF": true, "USD": true,
	"ISK": true,
}

// vatFormats maps country codes to a regex that validates the country's VAT
// number format. SE numbers carry the "SE" prefix per EU convention.
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
// permits for import into Norway. Chapter 22 = beverages/alcohol,
// chapter 24 = tobacco.
var prohibitedHSPrefixes = []string{"22", "24"}

// euDeMinimiEUR is the de minimis threshold in EUR above which customs
// declarations are required for EU destinations (B2C).
const euDeMinimisEUR = 150.0

// norwayDeMinimisNOK is the de minimis threshold in NOK above which customs
// declarations are required for Norway (B2C).
const norwayDeMinimisNOK = 350.0

// ValidateCustoms validates the Customs block for a shipment from origin to
// destination with the given shipment type ("B2B" or "B2C").
//
// Rules enforced:
//   - Non-EU destinations (NO, CH, GB, etc.): full customs declaration required
//     unless de minimis applies (Norway only in NOK; mismatched currency → ReviewRequired).
//   - EU destinations: B2B requires ImporterVATNumber; CustomsValue > 150 EUR
//     requires HSCode; Åland Islands (AX) always hard-errors.
//   - VAT number formats validated against origin/destination country rules.
//   - Prohibited HS codes (alcohol ch.22, tobacco ch.24) error for Norway.
func ValidateCustoms(c adapter.Customs, origin, destination, shipmentType string) error {
	// Åland Islands — special VAT territory, hard error regardless of direction.
	if destination == "AX" {
		return fmt.Errorf("Åland Islands (AX) require special VAT handling: contact your carrier")
	}

	if nonEUDestinations[destination] {
		return validateNonEUCustoms(c, origin, destination, shipmentType)
	}

	if euCountries[destination] {
		return validateEUCustoms(c, destination, shipmentType)
	}

	// Unknown destination — no customs rules to enforce.
	return nil
}

// validateNonEUCustoms enforces full customs declaration rules for
// destinations outside the EU customs union.
func validateNonEUCustoms(c adapter.Customs, origin, destination, shipmentType string) error {
	// De minimis check for Norway (B2C only).
	if destination == "NO" && strings.EqualFold(shipmentType, "B2C") {
		if c.CustomsCurrency == "NOK" && c.CustomsValue <= norwayDeMinimisNOK {
			return nil // below threshold — customs fields not required
		}
		if c.CustomsCurrency != "NOK" && c.CustomsCurrency != "" {
			return fmt.Errorf("%w: cannot determine Norway de minimis without NOK value", ReviewRequired)
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

	// Prohibited items for Norway.
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
	// B2C de minimis — below 150 EUR no customs fields are required.
	if strings.EqualFold(shipmentType, "B2C") {
		if c.CustomsCurrency == "EUR" && c.CustomsValue <= euDeMinimisEUR {
			return nil
		}
		if c.CustomsCurrency != "EUR" && c.CustomsCurrency != "" {
			// Non-EUR value — cannot determine de minimis; flag for review.
			return fmt.Errorf("%w: cannot determine EU de minimis without EUR value", ReviewRequired)
		}
	}

	// B2B — importer VAT number required.
	if strings.EqualFold(shipmentType, "B2B") {
		if c.ImporterVATNumber == "" {
			return fmt.Errorf("importer VAT number is required for B2B shipments to %s", destination)
		}
		if err := validateVATNumber(c.ImporterVATNumber, destination); err != nil {
			return fmt.Errorf("invalid importer VAT number: %w", err)
		}
	}

	// CustomsValue above EU de minimis requires HSCode.
	if c.CustomsCurrency == "EUR" && c.CustomsValue > euDeMinimisEUR && c.HSCode == "" {
		return fmt.Errorf("HS code is required for EU shipments with customs value > %.0f EUR", euDeMinimisEUR)
	}
	if c.HSCode != "" {
		if err := validateHSCode(c.HSCode); err != nil {
			return err
		}
	}

	// Incoterms optional for EU — no error if absent.

	return nil
}

// validateHSCode checks that the HS code is 6-10 digits.
func validateHSCode(code string) error {
	if !regexp.MustCompile(`^\d{6,10}$`).MatchString(code) {
		return fmt.Errorf("HS code must be 6-10 digits, got %q", code)
	}
	return nil
}

// validateVATNumber validates a VAT number against the known format for
// the given country code. Returns nil if no format rule exists for the country.
func validateVATNumber(number, country string) error {
	rule, ok := vatFormats[country]
	if !ok {
		return nil // no rule for this country — accept
	}
	if !rule.MatchString(number) {
		return fmt.Errorf("invalid %s VAT number format: %q", countryName(country), number)
	}
	return nil
}
