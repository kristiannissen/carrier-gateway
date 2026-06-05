// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/address_test.go.
package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
)

func TestValidateAddress_PostalCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		country     string
		postalCode  string
		wantErr     bool
		errContains string
	}{
		// DK — 4 digits
		{name: "DK valid", country: "DK", postalCode: "2300"},
		{name: "DK leading zero valid", country: "DK", postalCode: "0900"},
		{name: "DK too short", country: "DK", postalCode: "230", wantErr: true, errContains: "invalid Danish postal code: 230"},
		{name: "DK too long", country: "DK", postalCode: "23000", wantErr: true, errContains: "invalid Danish postal code: 23000"},
		{name: "DK non-numeric", country: "DK", postalCode: "23AB", wantErr: true, errContains: "invalid Danish postal code"},
		{name: "DK empty", country: "DK", postalCode: "", wantErr: true, errContains: "postal code is required"},

		// NO — 4 digits
		{name: "NO valid", country: "NO", postalCode: "0158"},
		{name: "NO leading zero valid", country: "NO", postalCode: "0001"},
		{name: "NO too short", country: "NO", postalCode: "158", wantErr: true, errContains: "invalid Norwegian postal code: 158"},
		{name: "NO too long", country: "NO", postalCode: "01580", wantErr: true, errContains: "invalid Norwegian postal code"},

		// SE — 5 digits
		{name: "SE valid", country: "SE", postalCode: "11122"},
		{name: "SE too short", country: "SE", postalCode: "1112", wantErr: true, errContains: "invalid Swedish postal code: 1112"},
		{name: "SE too long", country: "SE", postalCode: "111222", wantErr: true, errContains: "invalid Swedish postal code"},

		// FI — 5 digits
		{name: "FI valid", country: "FI", postalCode: "00100"},
		{name: "FI too short", country: "FI", postalCode: "0010", wantErr: true, errContains: "invalid Finnish postal code: 0010"},
		{name: "FI too long", country: "FI", postalCode: "001000", wantErr: true, errContains: "invalid Finnish postal code"},

		// PL — NN-NNN
		{name: "PL valid", country: "PL", postalCode: "00-001"},
		{name: "PL no dash", country: "PL", postalCode: "00001", wantErr: true, errContains: "invalid Polish postal code: 00001"},
		{name: "PL wrong format", country: "PL", postalCode: "000-01", wantErr: true, errContains: "invalid Polish postal code"},

		// DE — 5 digits
		{name: "DE valid", country: "DE", postalCode: "10115"},
		{name: "DE too short", country: "DE", postalCode: "1011", wantErr: true, errContains: "invalid German postal code"},

		// FR — 5 digits
		{name: "FR valid", country: "FR", postalCode: "75001"},
		{name: "FR too short", country: "FR", postalCode: "7500", wantErr: true, errContains: "invalid French postal code"},

		// Unknown country — non-standard code flagged for review
		{name: "unknown country standard code", country: "XX", postalCode: "12345"},
		{name: "unknown country non-standard", country: "XX", postalCode: "!@#$%", wantErr: true, errContains: "manual review"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			addr := adapter.Address{
				Name:       "Test",
				Street:     "Test Street",
				HouseNumber: "1",
				City:       "Test City",
				PostalCode: tc.postalCode,
				Country:    tc.country,
			}
			err := ValidateAddress(addr, "postnord", tc.country)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateAddress_StreetRequired(t *testing.T) {
	t.Parallel()

	nordicCountries := []string{"DK", "NO", "SE", "FI"}
	for _, country := range nordicCountries {
		country := country
		t.Run(country, func(t *testing.T) {
			t.Parallel()
			addr := adapter.Address{
				Name:       "Test",
				Street:     "",
				City:       "Test City",
				PostalCode: postalCodeFor(country),
				Country:    country,
			}
			err := ValidateAddress(addr, "postnord", country)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "street name is required")
		})
	}
}

func TestValidateAddress_MunicipalityRequiredForFinland(t *testing.T) {
	t.Parallel()

	addr := adapter.Address{
		Name:       "Test",
		Street:     "Mannerheimintie",
		HouseNumber: "1",
		City:       "",
		PostalCode: "00100",
		Country:    "FI",
	}
	err := ValidateAddress(addr, "posti", "FI")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "municipality is required for Finnish addresses")
}

func TestValidateAddress_HouseNumberRequired(t *testing.T) {
	t.Parallel()

	carriers := []string{"inpost", "gls", "dao"}
	for _, carrier := range carriers {
		carrier := carrier
		t.Run(carrier, func(t *testing.T) {
			t.Parallel()
			addr := adapter.Address{
				Name:        "Test",
				Street:      "Test Street",
				HouseNumber: "",
				City:        "Test City",
				PostalCode:  "2300",
				Country:     "DK",
			}
			err := ValidateAddress(addr, carrier, "DK")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "house number is required")
		})
	}
}

func TestValidateAddress_HouseNumberExemptForFrance(t *testing.T) {
	t.Parallel()

	// GLS, DAO, and InPost all exempt France from the house number requirement.
	carriers := []string{"inpost", "gls", "dao"}
	for _, carrier := range carriers {
		carrier := carrier
		t.Run(carrier, func(t *testing.T) {
			t.Parallel()
			addr := adapter.Address{
				Name:       "Marie Dupont",
				Street:     "Rue de Rivoli",
				City:       "Paris",
				PostalCode: "75001",
				Country:    "FR",
			}
			err := ValidateAddress(addr, carrier, "FR")
			assert.NoError(t, err, "house number should not be required for FR")
		})
	}
}

func TestValidateAddress_ReviewRequiredForRuralAddress(t *testing.T) {
	t.Parallel()

	addr := adapter.Address{
		Name:       "Rural Farm",
		Street:     "Some Rural Road",
		City:       "Nowhere",
		PostalCode: "!!!",
		Country:    "XX",
	}
	err := ValidateAddress(addr, "postnord", "XX")
	require.Error(t, err)
	assert.True(t, IsReviewRequired(err), "expected ReviewRequired sentinel")
}

func TestValidateAddress_ValidFullAddress(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		carrier string
		addr    adapter.Address
	}{
		{
			name:    "postnord DK",
			carrier: "postnord",
			addr: adapter.Address{
				Name: "Unisport", Street: "Industrivej", HouseNumber: "10",
				City: "Copenhagen", PostalCode: "2300", Country: "DK",
			},
		},
		{
			name:    "bring NO",
			carrier: "bring",
			addr: adapter.Address{
				Name: "Test", Street: "Karl Johans gate", HouseNumber: "1",
				City: "Oslo", PostalCode: "0154", Country: "NO",
			},
		},
		{
			name:    "gls DE with house number",
			carrier: "gls",
			addr: adapter.Address{
				Name: "Klaus", Street: "Hauptstraße", HouseNumber: "42",
				City: "Berlin", PostalCode: "10115", Country: "DE",
			},
		},
		{
			name:    "inpost PL with house number",
			carrier: "inpost",
			addr: adapter.Address{
				Name: "Jan", Street: "Marszałkowska", HouseNumber: "10",
				City: "Warsaw", PostalCode: "00-001", Country: "PL",
			},
		},
		{
			name:    "posti FI",
			carrier: "posti",
			addr: adapter.Address{
				Name: "Matti", Street: "Mannerheimintie", HouseNumber: "1",
				City: "Helsinki", PostalCode: "00100", Country: "FI",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, ValidateAddress(tc.addr, tc.carrier, tc.addr.Country))
		})
	}
}

// postalCodeFor returns a valid postal code for a given country for use in tests
// that are not testing postal code validation itself.
func postalCodeFor(country string) string {
	codes := map[string]string{
		"DK": "2300",
		"NO": "0158",
		"SE": "11122",
		"FI": "00100",
		"PL": "00-001",
		"DE": "10115",
		"FR": "75001",
	}
	if code, ok := codes[country]; ok {
		return code
	}
	return "12345"
}
