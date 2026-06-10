// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/restricted.go.
package validation

import (
	"fmt"
	"strings"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
)

// restrictedItemSeverity classifies how a restricted item match is handled.
type restrictedItemSeverity int

const (
	// severityBlock causes the booking to be rejected with a hard error.
	severityBlock restrictedItemSeverity = iota
	// severityWarn surfaces a warning in the booking response but does not
	// block the booking.
	severityWarn
)

// restrictedItem describes a prohibited or restricted goods entry.
type restrictedItem struct {
	// keyword is the case-insensitive substring matched against item descriptions.
	keyword  string
	severity restrictedItemSeverity
	// reason is included in the error or warning message.
	reason string
}

// carrierRestrictedItems maps carrier keys to their known restricted goods.
// Keywords are matched case-insensitively against Item.Description substrings.
//
// severityBlock — booking is rejected outright.
// severityWarn  — booking proceeds; warning added to BookingResponse.CustomsWarnings.
//
// TODO: Extend per carrier as documentation becomes available. Current entries
// are based on carrier terms of service and known postal regulations.
var carrierRestrictedItems = map[string][]restrictedItem{
	"postnord": {
		{keyword: "explosive", severity: severityBlock, reason: "explosives are prohibited by PostNord"},
		{keyword: "ammunition", severity: severityBlock, reason: "ammunition is prohibited by PostNord"},
		{keyword: "flammable liquid", severity: severityBlock, reason: "flammable liquids are prohibited by PostNord"},
		{keyword: "aerosol", severity: severityWarn, reason: "aerosols may require hazmat declaration — verify with PostNord before shipping"},
		{keyword: "lithium battery", severity: severityWarn, reason: "lithium batteries require UN3480/UN3481 compliance labelling"},
		{keyword: "lithium ion", severity: severityWarn, reason: "lithium-ion batteries require UN3480/UN3481 compliance labelling"},
		{keyword: "perfume", severity: severityWarn, reason: "perfumes may be classified as flammable liquids — verify with PostNord"},
	},
	"bring": {
		{keyword: "explosive", severity: severityBlock, reason: "explosives are prohibited by Bring"},
		{keyword: "ammunition", severity: severityBlock, reason: "ammunition is prohibited by Bring"},
		{keyword: "weapon", severity: severityBlock, reason: "weapons are prohibited by Bring"},
		{keyword: "firearm", severity: severityBlock, reason: "firearms are prohibited by Bring"},
		{keyword: "flammable liquid", severity: severityBlock, reason: "flammable liquids are prohibited by Bring"},
		{keyword: "aerosol", severity: severityWarn, reason: "aerosols may require hazmat declaration — verify with Bring before shipping"},
		{keyword: "lithium battery", severity: severityWarn, reason: "lithium batteries require UN3480/UN3481 compliance labelling"},
		{keyword: "lithium ion", severity: severityWarn, reason: "lithium-ion batteries require UN3480/UN3481 compliance labelling"},
		{keyword: "dry ice", severity: severityWarn, reason: "dry ice shipments require special UN1845 labelling"},
		{keyword: "perishable", severity: severityWarn, reason: "perishables require cold-chain packaging — verify with Bring before shipping"},
	},
	"gls": {
		{keyword: "explosive", severity: severityBlock, reason: "explosives are prohibited by GLS"},
		{keyword: "ammunition", severity: severityBlock, reason: "ammunition is prohibited by GLS"},
		{keyword: "weapon", severity: severityBlock, reason: "weapons are prohibited by GLS"},
		{keyword: "firearm", severity: severityBlock, reason: "firearms are prohibited by GLS"},
		{keyword: "dangerous good", severity: severityBlock, reason: "dangerous goods (ADR) are not accepted by GLS without prior agreement"},
		{keyword: "lithium battery", severity: severityWarn, reason: "lithium batteries require GLS DangerousGoods pre-approval and UN3480/UN3481 labelling"},
		{keyword: "lithium ion", severity: severityWarn, reason: "lithium-ion batteries require GLS DangerousGoods pre-approval"},
	},
	"dao": {
		{keyword: "explosive", severity: severityBlock, reason: "explosives are prohibited by DAO"},
		{keyword: "ammunition", severity: severityBlock, reason: "ammunition is prohibited by DAO"},
		{keyword: "weapon", severity: severityBlock, reason: "weapons are prohibited by DAO"},
		{keyword: "flammable", severity: severityBlock, reason: "flammable goods are prohibited by DAO"},
		{keyword: "lithium battery", severity: severityWarn, reason: "lithium batteries require DAO pre-approval"},
		{keyword: "lithium ion", severity: severityWarn, reason: "lithium-ion batteries require DAO pre-approval"},
		{keyword: "perishable", severity: severityWarn, reason: "perishables require cold-chain packaging — verify with DAO"},
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol shipments within Denmark require age verification compliance"},
	},
	"dhl": {
		{keyword: "explosive", severity: severityBlock, reason: "explosives are prohibited by DHL"},
		{keyword: "ammunition", severity: severityBlock, reason: "ammunition is prohibited by DHL"},
		{keyword: "weapon", severity: severityBlock, reason: "weapons are prohibited by DHL"},
		{keyword: "firearm", severity: severityBlock, reason: "firearms are prohibited by DHL"},
		{keyword: "aerosol", severity: severityBlock, reason: "aerosols are prohibited by DHL eCommerce Europe"},
		{keyword: "flammable liquid", severity: severityBlock, reason: "flammable liquids are prohibited by DHL eCommerce Europe"},
		{keyword: "lithium battery", severity: severityWarn, reason: "lithium batteries require DHL Lithium Battery pre-approval and UN3480/UN3481 labelling"},
		{keyword: "lithium-ion", severity: severityWarn, reason: "lithium-ion batteries require DHL Lithium Battery pre-approval"},
		{keyword: "lithium ion", severity: severityWarn, reason: "lithium-ion batteries require DHL Lithium Battery pre-approval"},
		{keyword: "dry ice", severity: severityWarn, reason: "dry ice requires DHL special handling agreement and UN1845 labelling"},
		{keyword: "perishable", severity: severityWarn, reason: "perishables require DHL Express cold-chain — verify product eligibility"},
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol requires DHL import/export licence verification per destination country"},
		{keyword: "wine", severity: severityWarn, reason: "alcohol requires DHL import/export licence verification per destination country"},
		{keyword: "beer", severity: severityWarn, reason: "alcohol requires DHL import/export licence verification per destination country"},
		{keyword: "spirits", severity: severityWarn, reason: "alcohol requires DHL import/export licence verification per destination country"},
	},
}

// RestrictedItemResult holds a single match from ValidateRestrictedItems.
type RestrictedItemResult struct {
	// ItemDescription is the item description that matched.
	ItemDescription string
	// Keyword is the restricted keyword that triggered the match.
	Keyword string
	// Reason explains why the item is restricted or prohibited.
	Reason string
	// Block is true when the item is outright prohibited (booking must be rejected).
	Block bool
}

// ValidateRestrictedItems checks all item descriptions in the shipment against
// the carrier's prohibited and restricted goods list.
//
// It returns two slices:
//   - blocked: items that must prevent the booking from proceeding.
//   - warned: items that are restricted but do not block the booking; surface
//     these in BookingResponse.CustomsWarnings.
//
// If the carrier has no restricted items list the function returns immediately
// with empty slices — no allocation, no iteration.
func ValidateRestrictedItems(carrier string, shipment adapter.Shipment) (blocked, warned []RestrictedItemResult) {
	rules, ok := carrierRestrictedItems[carrier]
	if !ok {
		return nil, nil
	}

	for _, colli := range shipment.Colli {
		for _, item := range colli.Items {
			lower := strings.ToLower(item.Description)
			for _, rule := range rules {
				if strings.Contains(lower, rule.keyword) {
					match := RestrictedItemResult{
						ItemDescription: item.Description,
						Keyword:         rule.keyword,
						Reason:          rule.reason,
						Block:           rule.severity == severityBlock,
					}
					if rule.severity == severityBlock {
						blocked = append(blocked, match)
					} else {
						warned = append(warned, match)
					}
				}
			}
		}
	}

	return blocked, warned
}

// RestrictedItemsError formats a hard-block result into an error message.
func RestrictedItemsError(blocked []RestrictedItemResult) error {
	if len(blocked) == 0 {
		return nil
	}
	reasons := make([]string, len(blocked))
	for i, r := range blocked {
		reasons[i] = fmt.Sprintf("%q: %s", r.ItemDescription, r.Reason)
	}
	return fmt.Errorf("shipment contains prohibited items: %s", strings.Join(reasons, "; "))
}
