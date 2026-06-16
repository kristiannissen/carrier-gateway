// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/restricted.go.
package validation

import (
	"fmt"
	"strings"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
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
	// Econt: prohibited and restricted goods per Econt General Terms of Service.
	// Source: https://www.econt.com/services/forbidden-items/
	"econt": {
		{keyword: "explosive", severity: severityBlock, reason: "explosives are prohibited by Econt"},
		{keyword: "ammunition", severity: severityBlock, reason: "ammunition is prohibited by Econt"},
		{keyword: "weapon", severity: severityBlock, reason: "weapons are prohibited by Econt"},
		{keyword: "firearm", severity: severityBlock, reason: "firearms are prohibited by Econt"},
		{keyword: "flammable liquid", severity: severityBlock, reason: "flammable liquids are prohibited by Econt"},
		{keyword: "narcotic", severity: severityBlock, reason: "narcotics are prohibited by Econt"},
		{keyword: "lithium battery", severity: severityWarn, reason: "lithium batteries require UN3480/UN3481 compliance labelling — verify with Econt before shipping"},
		{keyword: "lithium ion", severity: severityWarn, reason: "lithium-ion batteries require UN3480/UN3481 compliance labelling — verify with Econt before shipping"},
		{keyword: "perishable", severity: severityWarn, reason: "perishables must use Econt refrigerated pack service (REF) — add AddOnInsurance or contact Econt"},
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

// destinationProhibited maps destination country codes to a list of keywords
// that are always blocked for import into that country. These supplement the
// carrier-level checks in carrierRestrictedItems and are checked regardless of
// the carrier used.
//
// The universal "all countries" prohibitions (explosives, drugs, weapons, etc.)
// are encoded under the special key "*".
var destinationProhibited = map[string][]restrictedItem{
	// Universal — applies to every destination.
	"*": {
		{keyword: "explosive", severity: severityBlock, reason: "explosives are prohibited universally"},
		{keyword: "ammunition", severity: severityBlock, reason: "ammunition is prohibited universally"},
		{keyword: "weapon", severity: severityBlock, reason: "weapons are prohibited universally"},
		{keyword: "firearm", severity: severityBlock, reason: "firearms are prohibited universally"},
		{keyword: "replica firearm", severity: severityBlock, reason: "replica firearms are prohibited universally"},
		{keyword: "narcotic", severity: severityBlock, reason: "narcotics are prohibited universally"},
		{keyword: "cannabis", severity: severityBlock, reason: "cannabis-derived products are prohibited universally"},
		{keyword: "cbd", severity: severityBlock, reason: "CBD/cannabis-derived products are prohibited universally"},
		{keyword: "counterfeit", severity: severityBlock, reason: "counterfeit goods are prohibited universally"},
		{keyword: "radioactive", severity: severityBlock, reason: "radioactive materials are prohibited universally"},
		{keyword: "infectious substance", severity: severityBlock, reason: "infectious substances are prohibited universally"},
		{keyword: "human remains", severity: severityBlock, reason: "human remains are prohibited universally (use specialised carrier)"},
		{keyword: "animal ashes", severity: severityBlock, reason: "animal ashes are prohibited universally (use specialised carrier)"},
		{keyword: "pornography", severity: severityBlock, reason: "obscene publications are prohibited universally"},
	},
	// Middle East / Gulf — alcohol prohibited outright.
	"AE": {
		{keyword: "alcohol", severity: severityBlock, reason: "alcohol is prohibited for import into the UAE"},
		{keyword: "wine", severity: severityBlock, reason: "alcohol is prohibited for import into the UAE"},
		{keyword: "beer", severity: severityBlock, reason: "alcohol is prohibited for import into the UAE"},
		{keyword: "spirits", severity: severityBlock, reason: "alcohol is prohibited for import into the UAE"},
		{keyword: "drone", severity: severityBlock, reason: "drones require prior approval for import into the UAE"},
	},
	"BH": {
		{keyword: "alcohol", severity: severityBlock, reason: "alcohol is prohibited for import into Bahrain"},
	},
	"KW": {
		{keyword: "alcohol", severity: severityBlock, reason: "alcohol is prohibited for import into Kuwait"},
		{keyword: "military good", severity: severityBlock, reason: "military goods require authorisation for import into Kuwait"},
	},
	"QA": {
		{keyword: "alcohol", severity: severityBlock, reason: "alcohol is prohibited for import into Qatar"},
	},
	"SA": {
		{keyword: "alcohol", severity: severityBlock, reason: "alcohol is prohibited for import into Saudi Arabia"},
	},
	// Non-EU European — alcohol/tobacco trigger duty warning; restricted items follow EU/national rules.
	"NO": {
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol is subject to Norwegian import duties and quantity limits"},
		{keyword: "wine", severity: severityWarn, reason: "alcohol is subject to Norwegian import duties and quantity limits"},
		{keyword: "beer", severity: severityWarn, reason: "alcohol is subject to Norwegian import duties and quantity limits"},
		{keyword: "spirits", severity: severityWarn, reason: "alcohol is subject to Norwegian import duties and quantity limits"},
		{keyword: "tobacco", severity: severityWarn, reason: "tobacco is subject to Norwegian import duties and quantity limits"},
	},
	"CH": {
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol is subject to Swiss customs duties and quantity limits"},
		{keyword: "tobacco", severity: severityWarn, reason: "tobacco is subject to Swiss customs duties and quantity limits"},
	},
	"GB": {
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol is subject to UK import duty and excise"},
		{keyword: "tobacco", severity: severityWarn, reason: "tobacco is subject to UK import duty and excise"},
		{keyword: "human remains", severity: severityBlock, reason: "human remains require prior permission from UK Border Force"},
	},
	"IS": {
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol is subject to Icelandic import duties and quantity limits"},
		{keyword: "tobacco", severity: severityWarn, reason: "tobacco is subject to Icelandic import duties and quantity limits"},
	},
	"TR": {
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol is subject to Turkish import duties and quantity limits"},
		{keyword: "tobacco", severity: severityWarn, reason: "tobacco is subject to Turkish import duties and quantity limits"},
	},
	"UA": {
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol is subject to Ukrainian import duties and quantity limits"},
		{keyword: "tobacco", severity: severityWarn, reason: "tobacco is subject to Ukrainian import duties and quantity limits"},
	},
	"RS": {
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol is subject to Serbian import duties and quantity limits"},
		{keyword: "tobacco", severity: severityWarn, reason: "tobacco is subject to Serbian import duties and quantity limits"},
	},
	"ME": {
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol is subject to Montenegrin import duties and quantity limits"},
		{keyword: "tobacco", severity: severityWarn, reason: "tobacco is subject to Montenegrin import duties and quantity limits"},
	},
	"MK": {
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol is subject to North Macedonian import duties and quantity limits"},
		{keyword: "tobacco", severity: severityWarn, reason: "tobacco is subject to North Macedonian import duties and quantity limits"},
	},
	"XK": {
		{keyword: "alcohol", severity: severityWarn, reason: "alcohol is subject to Kosovo import duties and quantity limits"},
		{keyword: "tobacco", severity: severityWarn, reason: "tobacco is subject to Kosovo import duties and quantity limits"},
	},
}

// CheckDestinationProhibited checks all item descriptions in the shipment
// against the destination country's prohibited and restricted goods lists.
//
// It checks both the universal "*" rules and any destination-specific rules.
// Results follow the same semantics as ValidateRestrictedItems:
//   - blocked: must prevent the booking from proceeding.
//   - warned: restricted but do not block; surface in BookingResponse.CustomsWarnings.
func CheckDestinationProhibited(destination string, shipment adapter.Shipment) (blocked, warned []RestrictedItemResult) {
	keys := []string{"*", destination}

	for _, key := range keys {
		rules, ok := destinationProhibited[key]
		if !ok {
			continue
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
	}

	return blocked, warned
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
