// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/restricted_test.go.
package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

func shipmentWithItems(items ...string) adapter.Shipment {
	colli := make([]adapter.Colli, len(items))
	for i, desc := range items {
		colli[i] = adapter.Colli{
			ID:     "c" + string(rune('1'+i)),
			Weight: 1.0,
			Items:  []adapter.Item{{Description: desc, Weight: 1.0, Quantity: 1}},
		}
	}
	return adapter.Shipment{
		TotalWeight: float64(len(items)),
		Colli:       colli,
	}
}

func TestValidateRestrictedItems_NoRulesForCarrier(t *testing.T) {
	t.Parallel()
	blocked, warned := ValidateRestrictedItems("fedex", shipmentWithItems("explosives", "lithium battery"))
	assert.Empty(t, blocked)
	assert.Empty(t, warned)
}

func TestValidateRestrictedItems_NoItems(t *testing.T) {
	t.Parallel()
	blocked, warned := ValidateRestrictedItems("dhl", adapter.Shipment{})
	assert.Empty(t, blocked)
	assert.Empty(t, warned)
}

func TestValidateRestrictedItems_DHL_Blocked(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc    string
		keyword string
	}{
		{"Explosive device", "explosive"},
		{"12-gauge ammunition box", "ammunition"},
		{"Hunting weapon", "weapon"},
		{"Semi-automatic firearm", "firearm"},
		{"Aerosol paint can", "aerosol"},
		{"Flammable liquid solvent", "flammable liquid"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.keyword, func(t *testing.T) {
			t.Parallel()
			blocked, warned := ValidateRestrictedItems("dhl", shipmentWithItems(tc.desc))
			require.Len(t, blocked, 1)
			assert.True(t, blocked[0].Block)
			assert.Equal(t, tc.keyword, blocked[0].Keyword)
			assert.Empty(t, warned)
		})
	}
}

func TestValidateRestrictedItems_DHL_Warned(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc    string
		keyword string
	}{
		{"Lithium battery pack 18650", "lithium battery"},
		{"Lithium-ion cell assembly", "lithium-ion"},
		{"Dry ice cooler for biologics", "dry ice"},
		{"Perishable food item", "perishable"},
		{"Red wine case 12x75cl", "wine"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.keyword, func(t *testing.T) {
			t.Parallel()
			blocked, warned := ValidateRestrictedItems("dhl", shipmentWithItems(tc.desc))
			assert.Empty(t, blocked)
			require.Len(t, warned, 1)
			assert.False(t, warned[0].Block)
			assert.Equal(t, tc.keyword, warned[0].Keyword)
		})
	}
}

func TestValidateRestrictedItems_CaseInsensitive(t *testing.T) {
	t.Parallel()
	blocked, _ := ValidateRestrictedItems("dhl", shipmentWithItems("EXPLOSIVE DEVICE"))
	require.Len(t, blocked, 1)
	assert.Equal(t, "explosive", blocked[0].Keyword)
}

func TestValidateRestrictedItems_MultipleMatches(t *testing.T) {
	t.Parallel()
	// Two separate colli, each with a blocked item.
	shipment := adapter.Shipment{
		TotalWeight: 2.0,
		Colli: []adapter.Colli{
			{ID: "c1", Weight: 1.0, Items: []adapter.Item{{Description: "explosive charge", Weight: 1.0, Quantity: 1}}},
			{ID: "c2", Weight: 1.0, Items: []adapter.Item{{Description: "firearm parts", Weight: 1.0, Quantity: 1}}},
		},
	}
	blocked, _ := ValidateRestrictedItems("dhl", shipment)
	assert.Len(t, blocked, 2)
}

func TestValidateRestrictedItems_MixedSeverity(t *testing.T) {
	t.Parallel()
	// One colli with both a blocked item and a warned item.
	shipment := adapter.Shipment{
		TotalWeight: 1.0,
		Colli: []adapter.Colli{
			{
				ID:     "c1",
				Weight: 1.0,
				Items: []adapter.Item{
					{Description: "explosive detonator", Weight: 0.5, Quantity: 1},
					{Description: "lithium battery backup", Weight: 0.5, Quantity: 1},
				},
			},
		},
	}
	blocked, warned := ValidateRestrictedItems("dhl", shipment)
	assert.Len(t, blocked, 1)
	assert.Len(t, warned, 1)
}

func TestRestrictedItemsError_Empty(t *testing.T) {
	t.Parallel()
	assert.NoError(t, RestrictedItemsError(nil))
	assert.NoError(t, RestrictedItemsError([]RestrictedItemResult{}))
}

func TestRestrictedItemsError_Single(t *testing.T) {
	t.Parallel()
	err := RestrictedItemsError([]RestrictedItemResult{
		{ItemDescription: "explosive charge", Keyword: "explosive", Reason: "explosives are prohibited by DHL", Block: true},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "explosive charge")
	assert.Contains(t, err.Error(), "prohibited")
}

func TestRestrictedItemsError_Multiple(t *testing.T) {
	t.Parallel()
	err := RestrictedItemsError([]RestrictedItemResult{
		{ItemDescription: "explosive A", Reason: "reason A", Block: true},
		{ItemDescription: "weapon B", Reason: "reason B", Block: true},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "explosive A")
	assert.Contains(t, err.Error(), "weapon B")
}

func TestValidateRestrictedItems_Postnord(t *testing.T) {
	t.Parallel()
	blocked, _ := ValidateRestrictedItems("postnord", shipmentWithItems("explosive fuse"))
	require.Len(t, blocked, 1)
}

func TestValidateRestrictedItems_Bring(t *testing.T) {
	t.Parallel()
	blocked, _ := ValidateRestrictedItems("bring", shipmentWithItems("flammable liquid solvent"))
	require.Len(t, blocked, 1)
}

func TestValidateRestrictedItems_GLS(t *testing.T) {
	t.Parallel()
	_, warned := ValidateRestrictedItems("gls", shipmentWithItems("lithium battery module"))
	require.Len(t, warned, 1)
}

func TestValidateRestrictedItems_DAO_Alcohol_Warned(t *testing.T) {
	t.Parallel()
	blocked, warned := ValidateRestrictedItems("dao", shipmentWithItems("bottle of alcohol 500ml"))
	assert.Empty(t, blocked)
	require.Len(t, warned, 1)
	assert.Equal(t, "alcohol", warned[0].Keyword)
}

func TestCheckDestinationProhibited_Universal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc string
		key  string
	}{
		{"military explosive device", "explosive"},
		{"12-gauge ammunition", "ammunition"},
		{"hunting weapon", "weapon"},
		{"counterfeit handbag", "counterfeit"},
		{"radioactive isotope sample", "radioactive"},
		{"human remains urn", "human remains"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			// Universal rules apply regardless of destination.
			blocked, warned := CheckDestinationProhibited("SE", shipmentWithItems(tc.desc))
			require.Len(t, blocked, 1, "expected block for %q", tc.desc)
			assert.Empty(t, warned)
			assert.Equal(t, tc.key, blocked[0].Keyword)
		})
	}
}

func TestCheckDestinationProhibited_AlcoholToUAE_Blocked(t *testing.T) {
	t.Parallel()
	blocked, warned := CheckDestinationProhibited("AE", shipmentWithItems("bottle of red wine"))
	require.Len(t, blocked, 1)
	assert.Equal(t, "wine", blocked[0].Keyword)
	assert.Empty(t, warned)
}

func TestCheckDestinationProhibited_AlcoholToNorway_Warned(t *testing.T) {
	t.Parallel()
	blocked, warned := CheckDestinationProhibited("NO", shipmentWithItems("bottle of red wine"))
	assert.Empty(t, blocked)
	require.Len(t, warned, 1)
	assert.Equal(t, "wine", warned[0].Keyword)
}

func TestCheckDestinationProhibited_AlcoholToSweden_NoRule(t *testing.T) {
	t.Parallel()
	// Sweden (EU, intra-EU) has no destination-specific alcohol rule.
	blocked, warned := CheckDestinationProhibited("SE", shipmentWithItems("bottle of wine"))
	assert.Empty(t, blocked)
	assert.Empty(t, warned)
}

func TestCheckDestinationProhibited_GBTobacco_Warned(t *testing.T) {
	t.Parallel()
	blocked, warned := CheckDestinationProhibited("GB", shipmentWithItems("tobacco cigarettes"))
	assert.Empty(t, blocked)
	require.Len(t, warned, 1)
	assert.Equal(t, "tobacco", warned[0].Keyword)
}

func TestCheckDestinationProhibited_NoItems(t *testing.T) {
	t.Parallel()
	blocked, warned := CheckDestinationProhibited("NO", adapter.Shipment{})
	assert.Empty(t, blocked)
	assert.Empty(t, warned)
}

func TestRequiresCustomsBlock(t *testing.T) {
	t.Parallel()

	cases := []struct {
		origin      string
		destination string
		want        bool
	}{
		{"DK", "NO", true},
		{"DK", "GB", true},
		{"DK", "US", true},
		{"DK", "CH", true},
		{"DK", "SE", false}, // EU destination
		{"DK", "DE", false}, // EU destination
		{"DK", "DK", false}, // domestic
		{"SE", "SE", false}, // domestic
		{"DK", "XX", false}, // unknown — no rule
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.origin+"_to_"+tc.destination, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, RequiresCustomsBlock(tc.origin, tc.destination))
		})
	}
}
