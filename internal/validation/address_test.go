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

// =========================================================================
// Service point address validation
// =========================================================================

func TestValidateAddress_ServicePoint(t *testing.T) {
	t.Parallel()

	t.Run("valid service point — no street city postalCode required", func(t *testing.T) {
		t.Parallel()
		addr := adapter.Address{
			Name:           "Anna Svensson",
			Country:        "SE",
			Phone:          "+46701234567",
			ServicePointID: "sp_123",
		}
		assert.NoError(t, ValidateAddress(addr, "postnord", "SE"))
	})

	t.Run("service point missing name returns error", func(t *testing.T) {
		t.Parallel()
		addr := adapter.Address{
			Country:        "SE",
			ServicePointID: "sp_123",
		}
		err := ValidateAddress(addr, "postnord", "SE")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("service point missing country returns error", func(t *testing.T) {
		t.Parallel()
		addr := adapter.Address{
			Name:           "Anna Svensson",
			ServicePointID: "sp_123",
		}
		err := ValidateAddress(addr, "postnord", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "country is required")
	})

	t.Run("service point works for all carriers", func(t *testing.T) {
		t.Parallel()
		for _, carrier := range []string{"postnord", "bring", "posti", "gls", "dao", "inpost"} {
			carrier := carrier
			t.Run(carrier, func(t *testing.T) {
				t.Parallel()
				addr := adapter.Address{
					Name:           "Recipient",
					Country:        "DK",
					ServicePointID: "sp_001",
				}
				assert.NoError(t, ValidateAddress(addr, carrier, "DK"))
			})
		}
	})
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
		"US": "90210",
		"CA": "M5V3L9",
		"GB": "SW1A1AA",
		"JP": "123-4567",
		"CN": "100000",
		"BR": "01310-100",
		"AU": "2000",
	}
	if code, ok := codes[country]; ok {
		return code
	}
	return "12345"
}

// =========================================================================
// Extended postal code tests — Americas, Asia-Pacific, British Isles
// =========================================================================

func TestValidateAddress_PostalCodes_Extended(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		country     string
		postalCode  string
		state       string
		wantErr     bool
		errContains string
	}{
		// US — 5-digit ZIP
		{name: "US 5-digit valid", country: "US", postalCode: "90210", state: "CA"},
		{name: "US 5+4 ZIP valid", country: "US", postalCode: "90210-1234", state: "CA"},
		{name: "US APO valid", country: "US", postalCode: "APO AE 09001", state: "AE"},
		{name: "US FPO valid", country: "US", postalCode: "FPO AP 96601", state: "AP"},
		{name: "US 4-digit invalid", country: "US", postalCode: "9021", state: "CA", wantErr: true, errContains: "invalid US postal code"},
		{name: "US 6-digit invalid", country: "US", postalCode: "902101", state: "CA", wantErr: true, errContains: "invalid US postal code"},
		{name: "US letters invalid", country: "US", postalCode: "9021X", state: "CA", wantErr: true, errContains: "invalid US postal code"},

		// Canada
		{name: "CA valid no space", country: "CA", postalCode: "M5V3L9", state: "ON"},
		{name: "CA valid with space", country: "CA", postalCode: "M5V 3L9", state: "ON"},
		{name: "CA all digits invalid", country: "CA", postalCode: "123456", state: "ON", wantErr: true, errContains: "invalid Canadian postal code"},
		{name: "CA too short", country: "CA", postalCode: "M5V", state: "ON", wantErr: true, errContains: "invalid Canadian postal code"},

		// GB
		{name: "GB standard valid", country: "GB", postalCode: "SW1A 1AA"},
		{name: "GB no space valid", country: "GB", postalCode: "SW1A1AA"},
		{name: "GB short outward valid", country: "GB", postalCode: "W1A 1AA"},
		{name: "GB all digits invalid", country: "GB", postalCode: "12345", wantErr: true, errContains: "invalid British postal code"},

		// Japan
		{name: "JP with hyphen valid", country: "JP", postalCode: "123-4567"},
		{name: "JP without hyphen valid", country: "JP", postalCode: "1234567"},
		{name: "JP too short", country: "JP", postalCode: "123-456", wantErr: true, errContains: "invalid Japanese postal code"},
		{name: "JP too long", country: "JP", postalCode: "1234-5678", wantErr: true, errContains: "invalid Japanese postal code"},

		// China
		{name: "CN 6-digit valid", country: "CN", postalCode: "100000"},
		{name: "CN 5-digit invalid", country: "CN", postalCode: "10000", wantErr: true, errContains: "invalid Chinese postal code"},
		{name: "CN 7-digit invalid", country: "CN", postalCode: "1000000", wantErr: true, errContains: "invalid Chinese postal code"},

		// Brazil
		{name: "BR with hyphen valid", country: "BR", postalCode: "01310-100", state: "SP"},
		{name: "BR without hyphen valid", country: "BR", postalCode: "01310100", state: "SP"},
		{name: "BR too short", country: "BR", postalCode: "01310", state: "SP", wantErr: true, errContains: "invalid Brazilian postal code"},

		// Australia
		{name: "AU 4-digit valid", country: "AU", postalCode: "2000", state: "NSW"},
		{name: "AU 3-digit invalid", country: "AU", postalCode: "200", state: "NSW", wantErr: true, errContains: "invalid Australian postal code"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			addr := adapter.Address{
				Name:       "Test",
				Street:     "Test Street",
				City:       "Test City",
				PostalCode: tc.postalCode,
				Country:    tc.country,
				State:      tc.state,
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

// =========================================================================
// State / province validation
// =========================================================================

func TestValidateState_US(t *testing.T) {
	t.Parallel()

	cases := []struct {
		state   string
		wantErr bool
	}{
		// Valid states
		{state: "CA"},
		{state: "NY"},
		{state: "TX"},
		{state: "FL"},
		// DC and territories
		{state: "DC"},
		{state: "PR"},
		{state: "GU"},
		// Military
		{state: "AE"},
		{state: "AP"},
		{state: "AA"},
		// Lower-case normalised
		{state: "ca"},
		// Invalid
		{state: "", wantErr: true},
		{state: "XX", wantErr: true},
		{state: "ZZ", wantErr: true},
		{state: "California", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run("US_"+tc.state, func(t *testing.T) {
			t.Parallel()
			err := ValidateState(tc.state, "US")
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateState_Canada(t *testing.T) {
	t.Parallel()

	cases := []struct {
		state   string
		wantErr bool
	}{
		{state: "ON"},
		{state: "QC"},
		{state: "BC"},
		{state: "AB"},
		{state: "YT"}, // territory
		{state: "NU"}, // territory
		{state: "on"}, // lower-case normalised
		{state: "", wantErr: true},
		{state: "XX", wantErr: true},
		{state: "Ontario", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run("CA_"+tc.state, func(t *testing.T) {
			t.Parallel()
			err := ValidateState(tc.state, "CA")
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateState_Germany_Optional(t *testing.T) {
	t.Parallel()

	// DE state is not required but validated when present.
	t.Run("valid Bundesland code accepted", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateState("BE", "DE"))
		assert.NoError(t, ValidateState("BY", "DE"))
		assert.NoError(t, ValidateState("NW", "DE"))
	})

	t.Run("absent is accepted for DE", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateState("", "DE"))
	})

	t.Run("invalid Bundesland code rejected when provided", func(t *testing.T) {
		t.Parallel()
		err := ValidateState("XX", "DE")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid German state code")
	})
}

func TestValidateState_Brazil(t *testing.T) {
	t.Parallel()

	cases := []struct {
		state   string
		wantErr bool
	}{
		{state: "SP"},
		{state: "RJ"},
		{state: "MG"},
		{state: "sp"}, // lower-case normalised
		{state: "", wantErr: true},
		{state: "XX", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run("BR_"+tc.state, func(t *testing.T) {
			t.Parallel()
			err := ValidateState(tc.state, "BR")
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateState_NoRequirement(t *testing.T) {
	t.Parallel()

	// Countries with no state requirement and no known state set.
	for _, country := range []string{"DK", "NO", "SE", "FI", "FR", "JP", "CN"} {
		country := country
		t.Run(country+"_state_absent", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, ValidateState("", country))
		})
		t.Run(country+"_state_present_no_rule", func(t *testing.T) {
			t.Parallel()
			// No known set for these countries — any value accepted.
			assert.NoError(t, ValidateState("SomeRegion", country))
		})
	}
}

func TestValidateAddress_StateIntegration(t *testing.T) {
	t.Parallel()

	t.Run("US address with valid state passes", func(t *testing.T) {
		t.Parallel()
		addr := adapter.Address{
			Name: "Test", Street: "123 Main St",
			City: "Beverly Hills", PostalCode: "90210",
			Country: "US", State: "CA",
		}
		assert.NoError(t, ValidateAddress(addr, "postnord", "US"))
	})

	t.Run("US address missing state fails", func(t *testing.T) {
		t.Parallel()
		addr := adapter.Address{
			Name: "Test", Street: "123 Main St",
			City: "Beverly Hills", PostalCode: "90210",
			Country: "US",
		}
		err := ValidateAddress(addr, "postnord", "US")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "state is required for US")
	})

	t.Run("US address invalid state fails", func(t *testing.T) {
		t.Parallel()
		addr := adapter.Address{
			Name: "Test", Street: "123 Main St",
			City: "Beverly Hills", PostalCode: "90210",
			Country: "US", State: "ZZ",
		}
		err := ValidateAddress(addr, "postnord", "US")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid US state code")
	})

	t.Run("CA address with province passes", func(t *testing.T) {
		t.Parallel()
		addr := adapter.Address{
			Name: "Test", Street: "100 King St",
			City: "Toronto", PostalCode: "M5V3L9",
			Country: "CA", State: "ON",
		}
		assert.NoError(t, ValidateAddress(addr, "postnord", "CA"))
	})

	t.Run("DK address without state passes", func(t *testing.T) {
		t.Parallel()
		addr := adapter.Address{
			Name: "Test", Street: "Strøget", HouseNumber: "1",
			City: "Copenhagen", PostalCode: "1000",
			Country: "DK",
		}
		assert.NoError(t, ValidateAddress(addr, "postnord", "DK"))
	})

	t.Run("DE address with valid Bundesland passes", func(t *testing.T) {
		t.Parallel()
		addr := adapter.Address{
			Name: "Test", Street: "Hauptstraße", HouseNumber: "1",
			City: "Berlin", PostalCode: "10115",
			Country: "DE", State: "BE",
		}
		assert.NoError(t, ValidateAddress(addr, "gls", "DE"))
	})

	t.Run("DE address without state passes", func(t *testing.T) {
		t.Parallel()
		addr := adapter.Address{
			Name: "Test", Street: "Hauptstraße", HouseNumber: "1",
			City: "Berlin", PostalCode: "10115",
			Country: "DE",
		}
		assert.NoError(t, ValidateAddress(addr, "gls", "DE"))
	})
}
