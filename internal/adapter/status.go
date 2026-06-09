// Package adapter provides interfaces and shared types for carrier integrations.
// This file is located at /internal/adapter/status.go.
package adapter

// normalizedStatuses maps carrier keys to their known raw status → TrackingStatus mappings.
// Any raw status not present in the inner map falls back to StatusUnknown.
//
// Mapping notes per carrier:
//
// PostNord: status string from the v5 T&T API (shipments[0].status and items[0].events[0].status).
// Only INFORMED has been confirmed from a live production test.
// TODO: Obtain the complete PostNord v5 status enum from developer.postnord.com or
// by contacting kundeintegration@postnord.com — the portal returned 404 when checked.
//
// Bring: statusId string from consignmentSet[0].packageSet[0].statusId.
// Full enum sourced from the Bring Tracking API YAML specification.
//
// GLS: StatusCode string from UnitDetail.History[0].StatusCode.
// TODO: GLS does not publish its tracking status code enum publicly.
// Obtain the full list from GLS support or by observing live shipment events.
// All GLS statuses currently fall through to StatusUnknown.
//
// DAO: haendelse numeric string from resultat.haendelser[0].haendelse.
// TODO: Fetch the complete code list from GET /TrackNTraceKoder.php to verify
// and extend the mapping below.
//
// DHL: statusCode enum from the DHL Unified Tracking API spec (well-documented).
// Values: delivered, failure, pre-transit, transit, unknown.
var normalizedStatuses = map[string]map[string]TrackingStatus{
	"postnord": {
		// Confirmed from live production test (tracking 00073215400599388772).
		"INFORMED": StatusBooked,
		// Inferred from PostNord status progression documentation.
		// TODO: Confirm these against the full PostNord v5 status enum.
		"AVAILABLE_FOR_DELIVERY": StatusInTransit,
		"EXPECTED_DELIVERY":      StatusInTransit,
		"EN_ROUTE":               StatusInTransit,
		"IN_TRANSPORT":           StatusInTransit,
		"AT_DISTRIBUTION_CENTER": StatusInTransit,
		"OUT_FOR_DELIVERY":       StatusOutForDelivery,
		"DELIVERED":              StatusDelivered,
		"DELIVERY_IMPOSSIBLE":    StatusFailed,
		"FAILED_DELIVERY":        StatusFailed,
		"RETURN_TO_SENDER":       StatusReturned,
		"RETURNED":               StatusReturned,
		"DELAYED":                StatusDelayed,
	},

	"bring": {
		// Full enum from Bring Tracking API YAML specification.
		"ATTEMPTED_DELIVERY": StatusFailed,
		"COLLECTED":          StatusPickedUp,
		"CUSTOMS":            StatusInTransit,
		"DELIVERED":          StatusDelivered,
		"DELIVERED_SENDER":   StatusReturned,
		"DELIVERY_CANCELLED": StatusFailed,
		"DELIVERY_CHANGED":   StatusInTransit,
		"DELIVERY_ORDERED":   StatusBooked,
		"DEVIATION":          StatusFailed,
		"HANDED_IN":          StatusPickedUp,
		"INTERNATIONAL":      StatusInTransit,
		"IN_TRANSIT":         StatusInTransit,
		"NOTIFICATION_SENT":  StatusBooked,
		"PRE_NOTIFIED":       StatusBooked,
		"READY_FOR_PICKUP":   StatusOutForDelivery,
		"RETURN":             StatusReturned,
		"TERMINAL":           StatusInTransit,
		"TRANSPORT_TO_RECIPIENT": StatusOutForDelivery,
		"UNKNOWN":            StatusUnknown,
	},

	"gls": {
		// TODO: GLS does not publish its tracking StatusCode enum publicly.
		// Contact GLS support or observe live shipment events to populate this map.
		// All GLS statuses currently resolve to StatusUnknown via the fallback.
	},

	"dao": {
		// Numeric event codes from DAO TrackNTrace_v2.php haendelse field.
		// TODO: Fetch the authoritative list from GET /TrackNTraceKoder.php
		// and cross-check the codes below.
		"10": StatusInTransit,      // Pakke modtaget på fordelingscenter
		"20": StatusInTransit,      // Pakke er ankommet til terminal
		"30": StatusDelivered,      // Pakke er afleveret
		"40": StatusFailed,         // Pakke er ikke afleveret
		"50": StatusReturned,       // Pakke returneret til afsender
		"60": StatusOutForDelivery, // Pakke er på vej til modtager
		"70": StatusBooked,         // Pakke er registreret
	},

	"dhl": {
		// Full enum from DHL Unified Tracking API v1.5.8 specification.
		"delivered":   StatusDelivered,
		"failure":     StatusFailed,
		"pre-transit": StatusBooked,
		"transit":     StatusInTransit,
		"unknown":     StatusUnknown,
	},
}

// normalizeStatus maps a carrier-specific raw status string to a TrackingStatus.
// Returns StatusUnknown for any carrier or raw status not in the mapping table.
func normalizeStatus(carrier, rawStatus string) TrackingStatus {
	carrierMap, ok := normalizedStatuses[carrier]
	if !ok {
		return StatusUnknown
	}
	if normalized, ok := carrierMap[rawStatus]; ok {
		return normalized
	}
	return StatusUnknown
}
