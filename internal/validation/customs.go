// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/customs.go.
package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// nonEUDestinations is the set of country codes that are not part of the EU
// customs union and therefore require full customs declarations. It covers all
// non-EU European countries plus known non-European destinations.
// EU membership checks use IsEU from countries.go — do not add EU members here.
var nonEUDestinations = map[string]bool{
	// European — non-EU
	"CH": true, // Switzerland
	"GB": true, // United Kingdom
	"IS": true, // Iceland
	"LI": true, // Liechtenstein
	"ME": true, // Montenegro
	"MK": true, // North Macedonia
	"NO": true, // Norway — EEA but not EU customs union
	"RS": true, // Serbia
	"TR": true, // Turkey
	"UA": true, // Ukraine
	"XK": true, // Kosovo
	// Non-European
	"AU": true, // Australia
	"CA": true, // Canada
	"CN": true, // China
	"JP": true, // Japan
	"US": true, // United States
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
	// Western Balkans and Eastern Europe — all aligned to the EU €150 threshold.
	"ME": {value: 150.0, currency: "EUR"},
	"MK": {value: 150.0, currency: "EUR"},
	"RS": {value: 150.0, currency: "EUR"},
	"TR": {value: 150.0, currency: "EUR"},
	"UA": {value: 150.0, currency: "EUR"},
	"XK": {value: 150.0, currency: "EUR"},
}

// validISO4217 is a representative set of ISO 4217 currency codes.
var validISO4217 = map[string]bool{
	"DKK": true, "SEK": true, "NOK": true, "EUR": true,
	"PLN": true, "GBP": true, "CHF": true, "USD": true,
	"ISK": true, "CAD": true, "AUD": true, "JPY": true,
	"CNY": true,
}

// vatFormats maps country codes to a regex that validates the VAT number format.
// Sources: EU Commission VAT number formats document; non-EU formats from
// respective national tax administrations.
//
// Note: Greece uses "EL" as the VIES member-state code, not "GR".
// ValidateVATNumber accepts both "GR" and "EL" as the country argument and
// normalises to "EL" before matching.
var vatFormats = map[string]*regexp.Regexp{
	// Nordic EU members
	// DK and FI store numeric-only (no country prefix) per existing gateway convention.
	// SE stores SE + 10 digits per VIES convention (12-char total).
	"DK": regexp.MustCompile(`^\d{8}$`),
	"SE": regexp.MustCompile(`^SE\d{10}$`),
	"FI": regexp.MustCompile(`^\d{8}$`),
	// Other EU members (27 total, alphabetical)
	"AT": regexp.MustCompile(`^ATU\d{8}$`),
	"BE": regexp.MustCompile(`^BE\d{10}$`),
	"BG": regexp.MustCompile(`^BG\d{9,10}$`),
	"CY": regexp.MustCompile(`^CY\d{8}[A-Z]$`),
	"CZ": regexp.MustCompile(`^CZ\d{8,10}$`),
	"DE": regexp.MustCompile(`^DE\d{9}$`),
	"EE": regexp.MustCompile(`^EE\d{9}$`),
	"EL": regexp.MustCompile(`^EL\d{9}$`), // Greece — VIES uses EL, not GR
	"ES": regexp.MustCompile(`^ES[A-Z0-9]\d{7}[A-Z0-9]$`),
	"FR": regexp.MustCompile(`^FR[A-Z0-9]{2}\d{9}$`),
	"HR": regexp.MustCompile(`^HR\d{11}$`),
	"HU": regexp.MustCompile(`^HU\d{8}$`),
	"IE": regexp.MustCompile(`^IE\d{7}[A-Z]{1,2}$`),
	"IT": regexp.MustCompile(`^IT\d{11}$`),
	"LT": regexp.MustCompile(`^LT(\d{9}|\d{12})$`),
	"LU": regexp.MustCompile(`^LU\d{8}$`),
	"LV": regexp.MustCompile(`^LV\d{11}$`),
	"MT": regexp.MustCompile(`^MT\d{8}$`),
	"NL": regexp.MustCompile(`^NL\d{9}B\d{2}$`),
	"PL": regexp.MustCompile(`^PL\d{10}$`),
	"PT": regexp.MustCompile(`^PT\d{9}$`),
	"RO": regexp.MustCompile(`^RO\d{2,10}$`),
	"SI": regexp.MustCompile(`^SI\d{8}$`),
	"SK": regexp.MustCompile(`^SK\d{10}$`),
	// Non-EU European
	"GB": regexp.MustCompile(`^GB\d{9}$`),
	"IS": regexp.MustCompile(`^IS\d{10}$`),
	"CH": regexp.MustCompile(`^CHE\d{9}$`),
	"NO": regexp.MustCompile(`^\d{9}$`),
	"ME": regexp.MustCompile(`^\d{9}$`),
	"MK": regexp.MustCompile(`^MK\d{11}$`),
	"RS": regexp.MustCompile(`^\d{10}$`),
	"TR": regexp.MustCompile(`^\d{10}$`),
	"UA": regexp.MustCompile(`^\d{10}$`),
	"XK": regexp.MustCompile(`^\d{10}$`),
}

// prohibitedHSPrefixes lists HS chapter prefixes that require special
// permits for import into Norway.
var prohibitedHSPrefixes = []string{"22", "24"}

// hsCodeRegex matches a valid 6-10 digit HS code. Compiled once at startup
// rather than on every call to validateHSCode.
var hsCodeRegex = regexp.MustCompile(`^\d{6,10}$`)

// iso3166Alpha2 matches a valid ISO 3166-1 alpha-2 country code (two uppercase letters).
var iso3166Alpha2 = regexp.MustCompile(`^[A-Z]{2}$`)

// euDeMinimisEUR is the de minimis threshold for EU B2C shipments.
const euDeMinimisEUR = 150.0

// viesBaseURL is the VIES REST API base URL. Overridable in tests via package-level
// assignment so tests can point at an httptest.Server without DNS tricks.
var viesBaseURL = "https://ec.europa.eu/taxation_customs/vies/rest-api/ms"

// viesTimeout is the hard per-call deadline for VIES. Kept short so a VIES
// outage cannot add noticeable latency to the booking path.
const viesTimeout = 2 * time.Second

// viesResponse is the relevant subset of the VIES REST API JSON response.
type viesResponse struct {
	IsValid   bool   `json:"isValid"`
	UserError string `json:"userError"`
}

// ValidateCustoms validates the Customs block for a shipment from origin to
// destination with the given shipment type ("B2B" or "B2C").
func ValidateCustoms(c adapter.Customs, origin, destination, shipmentType string) error {
	if destination == "AX" {
		return fmt.Errorf("åland Islands (AX) require special VAT handling: contact your carrier")
	}

	if err := validateShipmentType(shipmentType); err != nil {
		return err
	}

	if err := validateCountryOfOrigin(c.CountryOfOrigin); err != nil {
		return err
	}

	if err := validateTransportMode(c); err != nil {
		return err
	}

	if nonEUDestinations[destination] {
		return validateNonEUCustoms(c, origin, destination, shipmentType)
	}

	if IsEU(destination) {
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
func validateHSCode(code string) error {
	if !hsCodeRegex.MatchString(code) {
		return fmt.Errorf("HS code must be 6-10 digits, got %q", code)
	}
	return nil
}

// validateVATNumber validates a VAT number against the known format for the
// given country code. Returns nil if no format rule exists for the country.
//
// Greece: VIES uses the member-state code "EL" rather than the ISO "GR".
// Both are accepted here; the number prefix must use "EL" per VIES convention.
func validateVATNumber(number, country string) error {
	key := country
	if key == "GR" {
		key = "EL"
	}
	rule, ok := vatFormats[key]
	if !ok {
		return nil
	}
	if !rule.MatchString(number) {
		return fmt.Errorf("invalid %s VAT number format: %q", countryName(country), number)
	}
	return nil
}

// validateCountryOfOrigin checks that CountryOfOrigin is a valid ISO 3166-1
// alpha-2 code when present.
func validateCountryOfOrigin(code string) error {
	if code == "" {
		return nil
	}
	if !iso3166Alpha2.MatchString(strings.ToUpper(code)) {
		return fmt.Errorf("countryOfOrigin must be a 2-letter ISO 3166-1 alpha-2 code, got %q", code)
	}
	return nil
}

// validateShipmentType checks that ShipmentType is one of the accepted values.
func validateShipmentType(shipmentType string) error {
	if shipmentType == "" {
		return nil
	}
	switch strings.ToUpper(shipmentType) {
	case "B2B", "B2C":
		return nil
	default:
		return fmt.Errorf("shipmentType must be B2B or B2C, got %q", shipmentType)
	}
}

// RequiresCustomsBlock reports whether a shipment from origin to destination
// requires a non-empty Customs block. True for all non-EU destinations
// regardless of shipment type — B2B requires full customs unconditionally;
// B2C requires it above de minimis and we cannot know the value without the block.
//
// Used by validateBookingRequest to reject requests that omit customs data
// entirely when shipping to a known non-EU destination.
func RequiresCustomsBlock(origin, destination string) bool {
	if origin == destination {
		return false
	}
	return nonEUDestinations[destination]
}

// ValidateVATNumberLive calls the VIES REST API to verify that number is a
// registered, active VAT number for country.
//
// It only makes a network call when country is an EU member state registered
// on VIES. For all other countries (NO, GB, CH, US …) it returns immediately
// (valid=true, unavailable=false) so callers never pay network latency for
// shipments that do not need VIES.
//
// Both VAT numbers on a booking (importer + exporter) should be checked in
// parallel under the same context deadline so the total overhead is one round
// trip, not two.
//
// Return values:
//
//	(true,  false, nil)  — VIES confirmed the number is active
//	(false, false, err)  — VIES confirmed the number is invalid or not found
//	(false, true,  nil)  — VIES was unreachable or timed out; degrade gracefully
func ValidateVATNumberLive(ctx context.Context, number, country string) (valid, unavailable bool, err error) {
	if !IsEU(country) {
		// Non-EU country — VIES does not cover it; pass through immediately.
		return true, false, nil
	}

	// Strip the country prefix from the VAT number if present; VIES encodes
	// the member state separately in the URL path.
	vatNumber := strings.TrimPrefix(strings.ToUpper(number), strings.ToUpper(country))

	url := fmt.Sprintf("%s/%s/vat/%s", viesBaseURL, country, vatNumber)

	// Apply a tight timeout independent of the parent context so a VIES outage
	// cannot delay the booking path beyond viesTimeout even if the parent
	// context has a much longer deadline.
	timeoutCtx, cancel := context.WithTimeout(ctx, viesTimeout)
	defer cancel()

	req, reqErr := http.NewRequestWithContext(timeoutCtx, http.MethodGet, url, nil)
	if reqErr != nil {
		return false, true, nil
	}
	req.Header.Set("Accept", "application/json")

	resp, doErr := http.DefaultClient.Do(req)
	if doErr != nil {
		// Network error, context cancelled, or timeout — degrade gracefully.
		return false, true, nil
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		// VIES returned an unexpected status — degrade gracefully.
		return false, true, nil
	}

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return false, true, nil
	}

	var result viesResponse
	if jsonErr := json.Unmarshal(body, &result); jsonErr != nil {
		return false, true, nil
	}

	if !result.IsValid {
		return false, false, fmt.Errorf(
			"VAT number %q is not registered as active in VIES for country %s",
			number, country,
		)
	}

	return true, false, nil
}
