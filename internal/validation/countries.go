// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/countries.go.
package validation

// euMemberStates is the canonical set of EU member state ISO 3166-1 alpha-2
// country codes as of 2026. It is the single authoritative source for EU
// membership within this package — all EU checks must delegate to IsEU.
var euMemberStates = map[string]bool{
	"AT": true, // Austria
	"BE": true, // Belgium
	"BG": true, // Bulgaria
	"CY": true, // Cyprus
	"CZ": true, // Czech Republic
	"DE": true, // Germany
	"DK": true, // Denmark
	"EE": true, // Estonia
	"ES": true, // Spain
	"FI": true, // Finland
	"FR": true, // France
	"GR": true, // Greece
	"HR": true, // Croatia
	"HU": true, // Hungary
	"IE": true, // Ireland
	"IT": true, // Italy
	"LT": true, // Lithuania
	"LU": true, // Luxembourg
	"LV": true, // Latvia
	"MT": true, // Malta
	"NL": true, // Netherlands
	"PL": true, // Poland
	"PT": true, // Portugal
	"RO": true, // Romania
	"SE": true, // Sweden
	"SI": true, // Slovenia
	"SK": true, // Slovakia
}

// nonEUEuropeanCountries is the set of European countries that are not EU
// members. Shipments to these destinations require full customs declarations,
// unlike intra-EU shipments which use simplified customs handling.
var nonEUEuropeanCountries = map[string]bool{
	"CH": true, // Switzerland
	"GB": true, // United Kingdom
	"IS": true, // Iceland
	"ME": true, // Montenegro
	"MK": true, // North Macedonia
	"NO": true, // Norway
	"RS": true, // Serbia
	"TR": true, // Turkey
	"UA": true, // Ukraine
	"XK": true, // Kosovo
}

// RouteType classifies the customs handling category for a shipment.
type RouteType int

const (
	// RouteIntraEU covers shipments where both origin and destination are EU
	// member states. Simplified customs apply; VAT validation for B2B.
	RouteIntraEU RouteType = iota
	// RouteEUToNonEU covers shipments from an EU member state to a non-EU
	// country. Full customs declarations are required.
	RouteEUToNonEU
	// RouteNonEUToEU covers shipments from a non-EU country into the EU.
	// Full customs declarations are required.
	RouteNonEUToEU
	// RouteNonEUToNonEU covers shipments between two non-EU countries.
	// Field-level validation applies; bilateral agreement logic is delegated
	// to the carrier API.
	RouteNonEUToNonEU
)

// ClassifyRoute returns the RouteType for a shipment from origin to dest.
// Both codes must be ISO 3166-1 alpha-2. Kosovo uses the provisional "XK" code.
func ClassifyRoute(origin, dest string) RouteType {
	switch {
	case IsEU(origin) && IsEU(dest):
		return RouteIntraEU
	case IsEU(origin) && !IsEU(dest):
		return RouteEUToNonEU
	case !IsEU(origin) && IsEU(dest):
		return RouteNonEUToEU
	default:
		return RouteNonEUToNonEU
	}
}

// IsEU reports whether code is an EU member state.
func IsEU(code string) bool {
	return euMemberStates[code]
}

// IsEurope reports whether code is a European country, EU member or not.
func IsEurope(code string) bool {
	return euMemberStates[code] || nonEUEuropeanCountries[code]
}
