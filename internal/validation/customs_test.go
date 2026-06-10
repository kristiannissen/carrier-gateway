// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/customs_test.go.
package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

func validNonEUCustoms() adapter.Customs {
	return adapter.Customs{
		Incoterms:         "DDP",
		HSCode:            "61091000",
		CustomsValue:      500.0,
		CustomsCurrency:   "DKK",
		ImporterOfRecord:  "NO123456789",
		ExporterVATNumber: "12345678",
		ShipmentType:      "B2B",
	}
}

func validEUB2BCustoms(destination string) adapter.Customs {
	vatNumbers := map[string]string{
		"SE": "SE1234567890",
		"FI": "12345678",
		"DE": "DE123456789",
	}
	return adapter.Customs{
		CustomsValue:      200.0,
		CustomsCurrency:   "EUR",
		HSCode:            "61091000",
		ImporterVATNumber: vatNumbers[destination],
		ShipmentType:      "B2B",
	}
}

// =========================================================================
// 1. Denmark → Norway (non-EU, customs required)
// =========================================================================

func TestValidateCustoms_DK_to_NO(t *testing.T) {
	t.Parallel()

	t.Run("valid B2B shipment", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateCustoms(validNonEUCustoms(), "DK", "NO", "B2B"))
	})

	t.Run("missing incoterms", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.Incoterms = ""
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incoterms is required for shipments to NO")
	})

	t.Run("invalid incoterms value", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.Incoterms = "XYZ"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid incoterms")
	})

	t.Run("missing HS code", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.HSCode = ""
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HS code is required")
	})

	t.Run("HS code too short", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.HSCode = "123"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HS code must be 6-10 digits")
	})

	t.Run("HS code too long", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.HSCode = "12345678901"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HS code must be 6-10 digits")
	})

	t.Run("missing importer of record", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.ImporterOfRecord = ""
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "importer of record is required")
	})

	t.Run("missing exporter VAT number", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.ExporterVATNumber = ""
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exporter VAT number is required")
	})

	t.Run("invalid DK exporter VAT number — too short", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.ExporterVATNumber = "123"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Danish VAT number")
	})

	t.Run("invalid DK exporter VAT number — non-numeric", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.ExporterVATNumber = "1234567X"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Danish VAT number")
	})

	t.Run("zero customs value", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.CustomsValue = 0
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "customs value must be greater than 0")
	})

	t.Run("missing customs currency", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.CustomsCurrency = ""
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "customs currency is required")
	})

	t.Run("invalid customs currency", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.CustomsCurrency = "XYZ"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid customs currency")
	})

	t.Run("prohibited HS code — alcohol chapter 22", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.HSCode = "220410"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "special import permit for Norway")
	})

	t.Run("prohibited HS code — tobacco chapter 24", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.HSCode = "240120"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "special import permit for Norway")
	})

	t.Run("B2C below NOK de minimis", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 300.0, CustomsCurrency: "NOK", ShipmentType: "B2C"}
		assert.NoError(t, ValidateCustoms(c, "DK", "NO", "B2C"))
	})

	t.Run("B2C at NOK de minimis boundary", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 350.0, CustomsCurrency: "NOK", ShipmentType: "B2C"}
		assert.NoError(t, ValidateCustoms(c, "DK", "NO", "B2C"))
	})

	t.Run("B2C above NOK de minimis — full customs required", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 351.0, CustomsCurrency: "NOK", ShipmentType: "B2C"}
		err := ValidateCustoms(c, "DK", "NO", "B2C")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incoterms is required")
	})

	t.Run("B2C non-NOK currency flagged for review", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 100.0, CustomsCurrency: "EUR", ShipmentType: "B2C"}
		err := ValidateCustoms(c, "DK", "NO", "B2C")
		require.Error(t, err)
		assert.True(t, IsReviewRequired(err))
	})
}

// =========================================================================
// 2. Denmark → Sweden (EU, VAT rules apply)
// =========================================================================

func TestValidateCustoms_DK_to_SE(t *testing.T) {
	t.Parallel()

	t.Run("valid B2B shipment", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateCustoms(validEUB2BCustoms("SE"), "DK", "SE", "B2B"))
	})

	t.Run("B2B missing importer VAT number", func(t *testing.T) {
		t.Parallel()
		c := validEUB2BCustoms("SE")
		c.ImporterVATNumber = ""
		err := ValidateCustoms(c, "DK", "SE", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "importer VAT number is required for B2B")
	})

	t.Run("B2B invalid SE VAT number — missing prefix", func(t *testing.T) {
		t.Parallel()
		c := validEUB2BCustoms("SE")
		c.ImporterVATNumber = "1234567890"
		err := ValidateCustoms(c, "DK", "SE", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Swedish VAT number")
	})

	t.Run("B2B valid SE VAT number", func(t *testing.T) {
		t.Parallel()
		c := validEUB2BCustoms("SE")
		c.ImporterVATNumber = "SE1234567890"
		assert.NoError(t, ValidateCustoms(c, "DK", "SE", "B2B"))
	})

	t.Run("B2C below EUR de minimis", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 100.0, CustomsCurrency: "EUR", ShipmentType: "B2C"}
		assert.NoError(t, ValidateCustoms(c, "DK", "SE", "B2C"))
	})

	t.Run("B2C above EUR de minimis requires HS code", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 200.0, CustomsCurrency: "EUR", ShipmentType: "B2C"}
		err := ValidateCustoms(c, "DK", "SE", "B2C")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HS code is required for EU shipments")
	})

	t.Run("B2C above EUR de minimis with valid HS code passes", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 200.0, CustomsCurrency: "EUR", HSCode: "61091000", ShipmentType: "B2C"}
		assert.NoError(t, ValidateCustoms(c, "DK", "SE", "B2C"))
	})

	t.Run("B2C non-EUR currency flagged for review", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 100.0, CustomsCurrency: "DKK", ShipmentType: "B2C"}
		err := ValidateCustoms(c, "DK", "SE", "B2C")
		require.Error(t, err)
		assert.True(t, IsReviewRequired(err))
	})

	t.Run("missing incoterms is not an error for EU", func(t *testing.T) {
		t.Parallel()
		c := validEUB2BCustoms("SE")
		c.Incoterms = ""
		assert.NoError(t, ValidateCustoms(c, "DK", "SE", "B2B"))
	})
}

// =========================================================================
// 3. Denmark → Finland (EU + Åland Islands)
// =========================================================================

func TestValidateCustoms_DK_to_FI(t *testing.T) {
	t.Parallel()

	t.Run("valid B2B shipment", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateCustoms(validEUB2BCustoms("FI"), "DK", "FI", "B2B"))
	})

	t.Run("B2B missing FI VAT number", func(t *testing.T) {
		t.Parallel()
		c := validEUB2BCustoms("FI")
		c.ImporterVATNumber = ""
		err := ValidateCustoms(c, "DK", "FI", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "importer VAT number is required for B2B")
	})

	t.Run("B2B invalid FI VAT number — too short", func(t *testing.T) {
		t.Parallel()
		c := validEUB2BCustoms("FI")
		c.ImporterVATNumber = "1234567"
		err := ValidateCustoms(c, "DK", "FI", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Finnish VAT number")
	})

	t.Run("Åland Islands hard error", func(t *testing.T) {
		t.Parallel()
		err := ValidateCustoms(validEUB2BCustoms("FI"), "DK", "AX", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "åland Islands")
	})

	t.Run("B2C de minimis applies same as SE", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 100.0, CustomsCurrency: "EUR", ShipmentType: "B2C"}
		assert.NoError(t, ValidateCustoms(c, "DK", "FI", "B2C"))
	})
}

// =========================================================================
// 4. Sweden → Norway
// =========================================================================

func TestValidateCustoms_SE_to_NO(t *testing.T) {
	t.Parallel()

	validSEtoNO := func() adapter.Customs {
		c := validNonEUCustoms()
		c.ExporterVATNumber = "SE1234567890"
		return c
	}

	t.Run("valid B2B shipment", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateCustoms(validSEtoNO(), "SE", "NO", "B2B"))
	})

	t.Run("missing incoterms", func(t *testing.T) {
		t.Parallel()
		c := validSEtoNO()
		c.Incoterms = ""
		err := ValidateCustoms(c, "SE", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incoterms is required for shipments to NO")
	})

	t.Run("invalid SE VAT number — missing SE prefix", func(t *testing.T) {
		t.Parallel()
		c := validSEtoNO()
		c.ExporterVATNumber = "1234567890"
		err := ValidateCustoms(c, "SE", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Swedish VAT number")
	})

	t.Run("prohibited HS code — alcohol", func(t *testing.T) {
		t.Parallel()
		c := validSEtoNO()
		c.HSCode = "220421"
		err := ValidateCustoms(c, "SE", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "special import permit for Norway")
	})
}

// =========================================================================
// 5. Finland → Norway
// =========================================================================

func TestValidateCustoms_FI_to_NO(t *testing.T) {
	t.Parallel()

	validFItoNO := func() adapter.Customs {
		c := validNonEUCustoms()
		c.ExporterVATNumber = "12345678"
		return c
	}

	t.Run("valid B2B shipment", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateCustoms(validFItoNO(), "FI", "NO", "B2B"))
	})

	t.Run("missing incoterms", func(t *testing.T) {
		t.Parallel()
		c := validFItoNO()
		c.Incoterms = ""
		err := ValidateCustoms(c, "FI", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incoterms is required for shipments to NO")
	})

	t.Run("invalid FI VAT number — non-numeric", func(t *testing.T) {
		t.Parallel()
		c := validFItoNO()
		c.ExporterVATNumber = "1234567X"
		err := ValidateCustoms(c, "FI", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid Finnish VAT number")
	})

	t.Run("missing importer of record", func(t *testing.T) {
		t.Parallel()
		c := validFItoNO()
		c.ImporterOfRecord = ""
		err := ValidateCustoms(c, "FI", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "importer of record is required")
	})
}

// =========================================================================
// 6. Denmark → Germany (intra-EU)
// =========================================================================

func TestValidateCustoms_DK_to_DE(t *testing.T) {
	t.Parallel()

	t.Run("valid B2B shipment", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateCustoms(validEUB2BCustoms("DE"), "DK", "DE", "B2B"))
	})

	t.Run("B2B missing DE VAT number", func(t *testing.T) {
		t.Parallel()
		c := validEUB2BCustoms("DE")
		c.ImporterVATNumber = ""
		err := ValidateCustoms(c, "DK", "DE", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "importer VAT number is required for B2B")
	})

	t.Run("B2B invalid DE VAT number — missing DE prefix", func(t *testing.T) {
		t.Parallel()
		c := validEUB2BCustoms("DE")
		c.ImporterVATNumber = "123456789"
		err := ValidateCustoms(c, "DK", "DE", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid German VAT number")
	})

	t.Run("B2B valid DE VAT number", func(t *testing.T) {
		t.Parallel()
		c := validEUB2BCustoms("DE")
		c.ImporterVATNumber = "DE123456789"
		assert.NoError(t, ValidateCustoms(c, "DK", "DE", "B2B"))
	})

	t.Run("B2C below EUR de minimis", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 100.0, CustomsCurrency: "EUR", ShipmentType: "B2C"}
		assert.NoError(t, ValidateCustoms(c, "DK", "DE", "B2C"))
	})

	t.Run("B2C above EUR de minimis requires HS code", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 200.0, CustomsCurrency: "EUR", ShipmentType: "B2C"}
		err := ValidateCustoms(c, "DK", "DE", "B2C")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "HS code is required for EU shipments")
	})

	t.Run("unknown destination — no rules enforced", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateCustoms(adapter.Customs{}, "DK", "XX", "B2C"))
	})
}

// =========================================================================
// Transport mode + Incoterms compatibility
// =========================================================================

func TestValidateCustoms_TransportMode(t *testing.T) {
	t.Parallel()

	t.Run("sea-only incoterms accepted with sea mode", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.Incoterms = "FOB"
		c.TransportMode = "sea"
		assert.NoError(t, ValidateCustoms(c, "DK", "NO", "B2B"))
	})

	t.Run("FOB rejected for air transport", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.Incoterms = "FOB"
		c.TransportMode = "air"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only valid for sea transport")
		assert.Contains(t, err.Error(), "FOB")
	})

	t.Run("FOB rejected for road transport", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.Incoterms = "FOB"
		c.TransportMode = "road"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only valid for sea transport")
	})

	t.Run("FAS rejected for air transport", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.Incoterms = "FAS"
		c.TransportMode = "air"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only valid for sea transport")
	})

	t.Run("DDP accepted for any transport mode", func(t *testing.T) {
		t.Parallel()
		for _, mode := range []string{"sea", "air", "road", "rail"} {
			mode := mode
			t.Run(mode, func(t *testing.T) {
				t.Parallel()
				c := validNonEUCustoms()
				c.TransportMode = mode
				assert.NoError(t, ValidateCustoms(c, "DK", "NO", "B2B"))
			})
		}
	})

	t.Run("invalid transport mode rejected", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.TransportMode = "truck"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid transport mode")
	})

	t.Run("no transport mode set — no error", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.TransportMode = ""
		assert.NoError(t, ValidateCustoms(c, "DK", "NO", "B2B"))
	})

	t.Run("sea-only incoterms with no mode set — accepted", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.Incoterms = "FOB"
		c.TransportMode = ""
		assert.NoError(t, ValidateCustoms(c, "DK", "NO", "B2B"))
	})
}

// =========================================================================
// De minimis — non-Nordic/EU destinations
// =========================================================================

func TestValidateCustoms_DeMinimis_Global(t *testing.T) {
	t.Parallel()

	validUSCustoms := func() adapter.Customs {
		return adapter.Customs{
			Incoterms:         "DDP",
			HSCode:            "61091000",
			CustomsValue:      900.0,
			CustomsCurrency:   "USD",
			ImporterOfRecord:  "US-EIN-12-3456789",
			ExporterVATNumber: "12345678",
			ShipmentType:      "B2B",
		}
	}

	t.Run("US B2C below $800 de minimis", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 799.0, CustomsCurrency: "USD", ShipmentType: "B2C"}
		assert.NoError(t, ValidateCustoms(c, "DK", "US", "B2C"))
	})

	t.Run("US B2C at $800 de minimis boundary", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 800.0, CustomsCurrency: "USD", ShipmentType: "B2C"}
		assert.NoError(t, ValidateCustoms(c, "DK", "US", "B2C"))
	})

	t.Run("US B2C above $800 de minimis — full customs required", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 801.0, CustomsCurrency: "USD", ShipmentType: "B2C"}
		err := ValidateCustoms(c, "DK", "US", "B2C")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incoterms is required")
	})

	t.Run("US B2B full customs required", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateCustoms(validUSCustoms(), "DK", "US", "B2B"))
	})

	t.Run("US B2C non-USD currency flagged for review", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 500.0, CustomsCurrency: "EUR", ShipmentType: "B2C"}
		err := ValidateCustoms(c, "DK", "US", "B2C")
		require.Error(t, err)
		assert.True(t, IsReviewRequired(err))
	})

	t.Run("GB B2C below £135 de minimis", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 130.0, CustomsCurrency: "GBP", ShipmentType: "B2C"}
		assert.NoError(t, ValidateCustoms(c, "DK", "GB", "B2C"))
	})

	t.Run("GB B2C above £135 de minimis — full customs required", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 136.0, CustomsCurrency: "GBP", ShipmentType: "B2C"}
		err := ValidateCustoms(c, "DK", "GB", "B2C")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incoterms is required")
	})
}

// =========================================================================
// CountryOfOrigin validation (new)
// =========================================================================

func TestValidateCustoms_CountryOfOrigin(t *testing.T) {
	t.Parallel()

	t.Run("uppercase two-letter accepted", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.CountryOfOrigin = "CN"
		assert.NoError(t, ValidateCustoms(c, "DK", "NO", "B2B"))
	})

	t.Run("absent — no error", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.CountryOfOrigin = ""
		assert.NoError(t, ValidateCustoms(c, "DK", "NO", "B2B"))
	})

	t.Run("lowercase two-letter accepted — normalised internally", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.CountryOfOrigin = "cn"
		assert.NoError(t, ValidateCustoms(c, "DK", "NO", "B2B"))
	})

	t.Run("three letters rejected", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.CountryOfOrigin = "CHN"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ISO 3166-1 alpha-2")
	})

	t.Run("single letter rejected", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.CountryOfOrigin = "C"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ISO 3166-1 alpha-2")
	})

	t.Run("numeric rejected", func(t *testing.T) {
		t.Parallel()
		c := validNonEUCustoms()
		c.CountryOfOrigin = "12"
		err := ValidateCustoms(c, "DK", "NO", "B2B")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ISO 3166-1 alpha-2")
	})
}

// =========================================================================
// ShipmentType enum validation (new)
// =========================================================================

func TestValidateCustoms_ShipmentType(t *testing.T) {
	t.Parallel()

	t.Run("B2B accepted", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateCustoms(validNonEUCustoms(), "DK", "NO", "B2B"))
	})

	t.Run("B2C accepted", func(t *testing.T) {
		t.Parallel()
		c := adapter.Customs{CustomsValue: 300.0, CustomsCurrency: "NOK", ShipmentType: "B2C"}
		assert.NoError(t, ValidateCustoms(c, "DK", "NO", "B2C"))
	})

	t.Run("lowercase b2b accepted", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateCustoms(validNonEUCustoms(), "DK", "NO", "b2b"))
	})

	t.Run("invalid type rejected", func(t *testing.T) {
		t.Parallel()
		err := ValidateCustoms(validNonEUCustoms(), "DK", "NO", "B2G")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shipmentType must be B2B or B2C")
	})

	t.Run("empty accepted — optional field", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateCustoms(validNonEUCustoms(), "DK", "NO", ""))
	})
}

// =========================================================================
// HS code validation
// =========================================================================

func TestValidateHSCode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		code    string
		wantErr bool
	}{
		{"610910", false},
		{"6109100000", false},
		{"61091000", false},
		{"12345", true},
		{"12345678901", true},
		{"61091X", true},
		{"", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			err := validateHSCode(tc.code)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =========================================================================
// VAT number format validation
// =========================================================================

func TestValidateVATNumber(t *testing.T) {
	t.Parallel()

	cases := []struct {
		number  string
		country string
		wantErr bool
	}{
		{"12345678", "DK", false},
		{"1234567", "DK", true},
		{"123456789", "DK", true},
		{"1234567X", "DK", true},
		{"SE1234567890", "SE", false},
		{"1234567890", "SE", true},
		{"SE123456789", "SE", true},
		{"12345678", "FI", false},
		{"1234567", "FI", true},
		{"123456789", "NO", false},
		{"12345678", "NO", true},
		{"1234567890", "NO", true},
		{"DE123456789", "DE", false},
		{"123456789", "DE", true},
		{"DE12345678", "DE", true},
		{"anything", "XX", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.country+"_"+tc.number, func(t *testing.T) {
			t.Parallel()
			err := validateVATNumber(tc.number, tc.country)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
