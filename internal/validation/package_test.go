// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/package_test.go.
package validation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// shipmentWith returns a minimal valid Shipment containing one colli built
// from the provided Colli value. Use it as a base for package validation tests.
func shipmentWith(colli adapter.Colli) adapter.Shipment {
	return adapter.Shipment{
		Sender:      adapter.Address{Name: "S", Street: "S St", City: "Copenhagen", PostalCode: "2300", Country: "DK"},
		Receiver:    adapter.Address{Name: "R", Street: "R St", City: "Stockholm", PostalCode: "11122", Country: "SE"},
		TotalWeight: colli.Weight,
		Colli:       []adapter.Colli{colli},
	}
}

func colliOf(weightKg, length, width, height float64) adapter.Colli {
	return adapter.Colli{
		ID:     "c1",
		Weight: weightKg,
		Dimensions: adapter.Dimensions{
			Length: length,
			Width:  width,
			Height: height,
		},
		Items: []adapter.Item{{Description: "item", Weight: weightKg, Quantity: 1}},
	}
}

// =========================================================================
// Weight limits
// =========================================================================

func TestValidateShipment_WeightLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		carrier     string
		weightKg    float64
		wantErr     bool
		errContains string
	}{
		// PostNord — 30 kg
		{carrier: "postnord", weightKg: 30.0},
		{carrier: "postnord", weightKg: 30.1, wantErr: true, errContains: "exceeds PostNord limit of 30 kg"},

		// Bring — 30 kg
		{carrier: "bring", weightKg: 30.0},
		{carrier: "bring", weightKg: 30.1, wantErr: true, errContains: "exceeds Bring limit of 30 kg"},

		// GLS — 40 kg
		{carrier: "gls", weightKg: 40.0},
		{carrier: "gls", weightKg: 40.1, wantErr: true, errContains: "exceeds GLS limit of 40 kg"},

		// DAO — 35 kg
		{carrier: "dao", weightKg: 35.0},
		{carrier: "dao", weightKg: 35.1, wantErr: true, errContains: "exceeds DAO limit of 35 kg"},

		// InPost — no limit
		{carrier: "inpost", weightKg: 999.0},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.carrier+"_"+fmt.Sprintf("%.1fkg", tc.weightKg), func(t *testing.T) {
			t.Parallel()
			err := ValidateShipment(tc.carrier, shipmentWith(colliOf(tc.weightKg, 10, 10, 10)))
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
// Individual dimension limits
// =========================================================================

func TestValidateShipment_DimensionLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		carrier     string
		l, w, h     float64
		wantErr     bool
		errContains string
	}{
		// Bring
		{name: "bring length ok", carrier: "bring", l: 250, w: 100, h: 80},
		{name: "bring length exceeded", carrier: "bring", l: 251, w: 100, h: 80, wantErr: true, errContains: "length 251 cm exceeds Bring limit"},
		{name: "bring width exceeded", carrier: "bring", l: 100, w: 121, h: 80, wantErr: true, errContains: "width 121 cm exceeds Bring limit"},
		{name: "bring height exceeded", carrier: "bring", l: 100, w: 100, h: 101, wantErr: true, errContains: "height 101 cm exceeds Bring limit"},

		// GLS
		{name: "gls length ok", carrier: "gls", l: 100, w: 80, h: 70},
		{name: "gls length exceeded", carrier: "gls", l: 271, w: 30, h: 30, wantErr: true, errContains: "length 271 cm exceeds GLS limit"},
		{name: "gls width exceeded", carrier: "gls", l: 100, w: 121, h: 30, wantErr: true, errContains: "width 121 cm exceeds GLS limit"},
		{name: "gls height exceeded", carrier: "gls", l: 100, w: 30, h: 121, wantErr: true, errContains: "height 121 cm exceeds GLS limit"},

		// DAO
		{name: "dao length ok", carrier: "dao", l: 250, w: 100, h: 100},
		{name: "dao length exceeded", carrier: "dao", l: 251, w: 100, h: 100, wantErr: true, errContains: "length 251 cm exceeds DAO limit"},
		{name: "dao width exceeded", carrier: "dao", l: 100, w: 121, h: 100, wantErr: true, errContains: "width 121 cm exceeds DAO limit"},
		{name: "dao height exceeded", carrier: "dao", l: 100, w: 100, h: 121, wantErr: true, errContains: "height 121 cm exceeds DAO limit"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateShipment(tc.carrier, shipmentWith(colliOf(1.0, tc.l, tc.w, tc.h)))
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
// Combined dimension sum (PostNord: L+W+H <= 300)
// =========================================================================

func TestValidateShipment_PostNord_DimensionSum(t *testing.T) {
	t.Parallel()

	t.Run("at limit", func(t *testing.T) {
		t.Parallel()
		// L=180, W=30, H=30: sum=240 (under 300), girth=2*(30+30)+180=300 (exactly at limit).
		assert.NoError(t, ValidateShipment("postnord", shipmentWith(colliOf(1.0, 180, 30, 30))))
	})

	t.Run("girth tighter than sum for long thin parcels", func(t *testing.T) {
		t.Parallel()
		// PostNord enforces both L+W+H<=300 and 2*(W+H)+L<=300.
		// For any parcel with W,H > 0, girth is tighter than sum:
		// L=298, W=1, H=1: sum=300 (at limit), girth=2*(1+1)+298=302 (exceeds limit).
		err := ValidateShipment("postnord", shipmentWith(colliOf(1.0, 298, 1, 1)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "girth")
	})
}

// =========================================================================
// Girth limits (2*(W+H)+L)
// =========================================================================

func TestValidateShipment_GirthLimits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		carrier     string
		l, w, h     float64
		wantErr     bool
		errContains string
	}{
		// PostNord girth limit = 300: 2*(50+50)+100 = 300 — ok
		{name: "postnord at girth limit", carrier: "postnord", l: 100, w: 50, h: 50},
		// 2*(51+50)+100 = 302
		{name: "postnord girth exceeded", carrier: "postnord", l: 100, w: 51, h: 50, wantErr: true, errContains: "girth 302 cm (2×(W+H)+L) exceeds PostNord limit"},

		// GLS girth limit = 400: 2*(100+100)+100 = 500 — exceeded
		{name: "gls girth exceeded", carrier: "gls", l: 100, w: 100, h: 100, wantErr: true, errContains: "girth 500 cm (2×(W+H)+L) exceeds GLS limit"},
		// 2*(80+70)+100 = 400 — at limit
		{name: "gls at girth limit", carrier: "gls", l: 100, w: 80, h: 70},

		// Bring has no girth limit
		{name: "bring no girth limit", carrier: "bring", l: 100, w: 100, h: 100},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateShipment(tc.carrier, shipmentWith(colliOf(1.0, tc.l, tc.w, tc.h)))
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
// Colli count limit (PostNord: max 5)
// =========================================================================

func TestValidateShipment_PostNord_ColliCountLimit(t *testing.T) {
	t.Parallel()

	makeShipment := func(n int) adapter.Shipment {
		colli := make([]adapter.Colli, n)
		total := 0.0
		for i := range colli {
			colli[i] = colliOf(1.0, 10, 10, 10)
			colli[i].ID = fmt.Sprintf("c%d", i+1)
			total += 1.0
		}
		return adapter.Shipment{
			Sender:      adapter.Address{Name: "S", Street: "St", City: "CPH", PostalCode: "2300", Country: "DK"},
			Receiver:    adapter.Address{Name: "R", Street: "St", City: "STO", PostalCode: "11122", Country: "SE"},
			TotalWeight: total,
			Colli:       colli,
		}
	}

	t.Run("exactly 5 colli", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateShipment("postnord", makeShipment(5)))
	})

	t.Run("6 colli exceeds limit", func(t *testing.T) {
		t.Parallel()
		err := ValidateShipment("postnord", makeShipment(6))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "PostNord supports a maximum of 5 colli per shipment")
	})

	t.Run("no colli limit for bring", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, ValidateShipment("bring", makeShipment(20)))
	})
}

// =========================================================================
// Unknown carrier — no limits enforced
// =========================================================================

func TestValidateShipment_UnknownCarrier(t *testing.T) {
	t.Parallel()
	// Unknown carrier should not return an error — limits are simply not enforced.
	err := ValidateShipment("fedex", shipmentWith(colliOf(999, 999, 999, 999)))
	assert.NoError(t, err)
}
