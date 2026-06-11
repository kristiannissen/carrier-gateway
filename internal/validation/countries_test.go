// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/countries_test.go.
package validation

import "testing"

func TestIsEU(t *testing.T) {
	t.Parallel()

	// All 27 EU member states must return true.
	members := []string{
		"AT", "BE", "BG", "CY", "CZ", "DE", "DK", "EE", "ES", "FI",
		"FR", "GR", "HR", "HU", "IE", "IT", "LT", "LU", "LV", "MT",
		"NL", "PL", "PT", "RO", "SE", "SI", "SK",
	}
	for _, code := range members {
		if !IsEU(code) {
			t.Errorf("IsEU(%q) = false, want true", code)
		}
	}

	// Non-EU European countries must return false.
	nonEU := []string{"CH", "GB", "IS", "ME", "MK", "NO", "RS", "TR", "UA", "XK"}
	for _, code := range nonEU {
		if IsEU(code) {
			t.Errorf("IsEU(%q) = true, want false", code)
		}
	}

	// Non-European countries must return false.
	nonEuropean := []string{"US", "CA", "AU", "JP", "CN", "BR"}
	for _, code := range nonEuropean {
		if IsEU(code) {
			t.Errorf("IsEU(%q) = true, want false", code)
		}
	}

	// Empty and invalid codes must return false.
	for _, code := range []string{"", "XX", "ZZ"} {
		if IsEU(code) {
			t.Errorf("IsEU(%q) = true, want false", code)
		}
	}
}

func TestIsEurope(t *testing.T) {
	t.Parallel()

	// All EU members are in Europe.
	euMembers := []string{
		"AT", "BE", "BG", "CY", "CZ", "DE", "DK", "EE", "ES", "FI",
		"FR", "GR", "HR", "HU", "IE", "IT", "LT", "LU", "LV", "MT",
		"NL", "PL", "PT", "RO", "SE", "SI", "SK",
	}
	for _, code := range euMembers {
		if !IsEurope(code) {
			t.Errorf("IsEurope(%q) = false, want true", code)
		}
	}

	// Non-EU European countries are still in Europe.
	nonEU := []string{"CH", "GB", "IS", "ME", "MK", "NO", "RS", "TR", "UA", "XK"}
	for _, code := range nonEU {
		if !IsEurope(code) {
			t.Errorf("IsEurope(%q) = false, want true", code)
		}
	}

	// Non-European countries are not in Europe.
	nonEuropean := []string{"US", "CA", "AU", "JP", "CN", "BR"}
	for _, code := range nonEuropean {
		if IsEurope(code) {
			t.Errorf("IsEurope(%q) = true, want false", code)
		}
	}

	// Empty and invalid codes must return false.
	for _, code := range []string{"", "XX", "ZZ"} {
		if IsEurope(code) {
			t.Errorf("IsEurope(%q) = true, want false", code)
		}
	}
}

func TestEUMemberCount(t *testing.T) {
	t.Parallel()

	const want = 27
	if got := len(euMemberStates); got != want {
		t.Errorf("len(euMemberStates) = %d, want %d", got, want)
	}
}

func TestNonEUEuropeanCount(t *testing.T) {
	t.Parallel()

	const want = 10
	if got := len(nonEUEuropeanCountries); got != want {
		t.Errorf("len(nonEUEuropeanCountries) = %d, want %d", got, want)
	}
}

func TestEUAndNonEUAreDisjoint(t *testing.T) {
	t.Parallel()

	for code := range nonEUEuropeanCountries {
		if euMemberStates[code] {
			t.Errorf("country %q appears in both euMemberStates and nonEUEuropeanCountries", code)
		}
	}
}

func TestClassifyRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		origin string
		dest   string
		want   RouteType
	}{
		// IntraEU — both EU members
		{"DK", "DE", RouteIntraEU},
		{"FR", "SE", RouteIntraEU},
		{"PL", "IT", RouteIntraEU},
		// EU → Non-EU
		{"DK", "NO", RouteEUToNonEU},
		{"DE", "GB", RouteEUToNonEU},
		{"FR", "CH", RouteEUToNonEU},
		{"SE", "US", RouteEUToNonEU},
		{"NL", "TR", RouteEUToNonEU},
		// Non-EU → EU
		{"NO", "DK", RouteNonEUToEU},
		{"GB", "DE", RouteNonEUToEU},
		{"US", "FR", RouteNonEUToEU},
		{"CH", "IT", RouteNonEUToEU},
		// Non-EU → Non-EU
		{"NO", "CH", RouteNonEUToNonEU},
		{"GB", "US", RouteNonEUToNonEU},
		{"US", "CA", RouteNonEUToNonEU},
		{"TR", "UA", RouteNonEUToNonEU},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.origin+"→"+tc.dest, func(t *testing.T) {
			t.Parallel()
			got := ClassifyRoute(tc.origin, tc.dest)
			if got != tc.want {
				t.Errorf("ClassifyRoute(%q, %q) = %d, want %d", tc.origin, tc.dest, got, tc.want)
			}
		})
	}
}
