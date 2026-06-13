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
		"ATTEMPTED_DELIVERY":     StatusFailed,
		"COLLECTED":              StatusPickedUp,
		"CUSTOMS":                StatusInTransit,
		"DELIVERED":              StatusDelivered,
		"DELIVERED_SENDER":       StatusReturned,
		"DELIVERY_CANCELLED":     StatusFailed,
		"DELIVERY_CHANGED":       StatusInTransit,
		"DELIVERY_ORDERED":       StatusBooked,
		"DEVIATION":              StatusFailed,
		"HANDED_IN":              StatusPickedUp,
		"INTERNATIONAL":          StatusInTransit,
		"IN_TRANSIT":             StatusInTransit,
		"NOTIFICATION_SENT": StatusBooked,
		// PRE_NOTIFIED means the carrier has sent the recipient a delivery
		// notification — the shipment is already moving toward the address.
		// Previously mapped to StatusBooked (incorrect: booked = just registered).
		"PRE_NOTIFIED":           StatusInTransit,
		"READY_FOR_PICKUP":       StatusOutForDelivery,
		"RETURN":                 StatusReturned,
		"TERMINAL":               StatusInTransit,
		"TRANSPORT_TO_RECIPIENT": StatusOutForDelivery,
		"UNKNOWN":                StatusUnknown,
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

	"dhl_express": {
		// Event type codes from MyDHL API v3.3.0 tracking response.
		// TODO: Obtain the full event code reference from DHL Express to extend
		// this map. Codes below are sourced from the API spec examples and descriptions.
		"SS": StatusBooked,         // Shipment information sent to DHL
		"PU": StatusPickedUp,       // Picked up from shipper
		"PL": StatusInTransit,      // Processed at facility
		"AF": StatusInTransit,      // Arrived at DHL facility
		"DF": StatusInTransit,      // Departed DHL facility
		"BR": StatusInTransit,      // Broker notified
		"RR": StatusInTransit,      // Customs update
		"CR": StatusInTransit,      // Customs clearance complete
		"AR": StatusInTransit,      // Arrived at delivery facility
		"WC": StatusOutForDelivery, // With courier, out for delivery
		"OK": StatusDelivered,      // Delivered
		"DM": StatusFailed,         // Damage reported
		"OH": StatusFailed,         // On hold
		"MS": StatusFailed,         // Shipment missed (not picked up)
		"TP": StatusFailed,         // Transfer to post office
	},

	// hermes: 2x2 event codes from hermes_Germany_Eventcodes.csv.
	// Codes are the 4-character second column (2x2 Code) returned in ShipmentStatus.code.
	// TODO: Confirm the exact wire format of code from a live tracking response —
	// the OpenAPI spec shows example "0-0-0" which may indicate a different format.
	"hermes": {
		"0000": StatusBooked,         // Shipment notified to Hermes electronically
		"0600": StatusDelayed,        // Not arrived at depot (1st notice)
		"0700": StatusDelayed,        // Not arrived at depot (2nd notice)
		"0800": StatusDelayed,        // Not arrived at depot (3rd notice)
		"0900": StatusDelayed,        // Not arrived at depot (4th notice)
		"1000": StatusPickedUp,       // Left client warehouse
		"1510": StatusInTransit,      // Sorted at Logistics Center
		"1520": StatusReturned,       // Return sorted, on way back to client
		"1610": StatusInTransit,      // International transit
		"1710": StatusInTransit,      // Customs export created
		"1720": StatusInTransit,      // Customs clearance completed
		"1730": StatusFailed,         // Held by customs
		"1751": StatusFailed,         // Rejected by customs
		"1810": StatusInTransit,      // Handed to partner carrier
		"1820": StatusInTransit,      // Handed to partner carrier (variant)
		"1900": StatusPickedUp,       // Accepted after collection
		"1901": StatusInTransit,      // Arrived at branch
		"1910": StatusPickedUp,       // Collected from client
		"2000": StatusInTransit,      // Arrived at depot
		"2100": StatusInTransit,      // Arrived without advice data
		"2300": StatusInTransit,      // Arrived (automatic)
		"2400": StatusInTransit,      // Handed in at ParcelShop
		"3000": StatusOutForDelivery, // Gone out on delivery route
		"3010": StatusOutForDelivery, // Left distribution centre
		"3300": StatusInTransit,      // Sorted at depot
		"3410": StatusInTransit,      // Available at ParcelShop for collection
		"3430": StatusInTransit,      // Handed to island carrier
		"3500": StatusDelivered,      // Delivered
		"3510": StatusDelivered,      // Delivered (variant)
		"3511": StatusDelivered,      // Delivered to letterbox
		"3520": StatusDelivered,      // Delivered (variant)
		"3530": StatusDelivered,      // Collected by recipient from ParcelShop
		"3710": StatusFailed,         // Not accepted, on way back
		"3715": StatusFailed,         // COD delivery failed (no cash)
		"3720": StatusFailed,         // Address not found
		"3731": StatusFailed,         // Recipient not present (1st attempt)
		"3732": StatusFailed,         // Recipient not present (2nd attempt)
		"3733": StatusFailed,         // Recipient not present (3rd attempt)
		"3734": StatusFailed,         // Recipient not present (4th attempt), return prep
		"3740": StatusFailed,         // Stopped due to possible damage
		"3745": StatusFailed,         // Forwarded to nearby parcel shop
		"3750": StatusFailed,         // Route cancellation
		"3751": StatusFailed,         // Missing/incorrect TAN (1st attempt)
		"3752": StatusFailed,         // Missing/incorrect TAN (2nd attempt)
		"3753": StatusFailed,         // Missing/incorrect TAN (3rd attempt)
		"3754": StatusFailed,         // Missing/incorrect TAN (4th attempt), return prep
		"3780": StatusFailed,         // Wrong route, redirected
		"3782": StatusFailed,         // ID photo mismatch
		"3783": StatusFailed,         // ID name mismatch
		"3784": StatusFailed,         // ID date of birth mismatch
		"3785": StatusFailed,         // ID number mismatch
		"3786": StatusFailed,         // PIN code mismatch
		"3787": StatusFailed,         // Age mismatch
		"3795": StatusFailed,         // Delivery stopped
		"4010": StatusReturned,       // On way back to depot
		"4015": StatusReturned,       // Back at depot
		"4020": StatusReturned,       // Back, address not found
		"4024": StatusReturned,       // Back, too large for ParcelShop
		"4025": StatusReturned,       // Sorted, forwarded to depot
		"4031": StatusReturned,       // Back at depot
		"4032": StatusReturned,       // Back at depot
		"4033": StatusReturned,       // Back, contact customer
		"4034": StatusReturned,       // Back, prepared for return to client
		"4035": StatusReturned,       // Not collected from ParcelShop within 7 days
		"4040": StatusReturned,       // Received with potential damage
		"4050": StatusReturned,       // Back at depot
		"4051": StatusReturned,       // Back at depot
		"4052": StatusReturned,       // Back at depot
		"4053": StatusReturned,       // Back, contact customer
		"4054": StatusReturned,       // Back, prepared for return to client
		"4060": StatusReturned,       // Return arrived at depot
		"4061": StatusReturned,       // Return arrived at depot
		"4080": StatusReturned,       // Redirected, delivery delayed
		"4081": StatusReturned,       // Back at depot
		"4082": StatusReturned,       // Back at depot
		"6080": StatusReturned,       // Processed, being returned
		"6081": StatusReturned,       // On way back to client
		"6082": StatusReturned,       // On way back (label not legible)
		"6083": StatusReturned,       // On way back to client
		"6084": StatusReturned,       // On way back (recipient repeatedly absent)
		"6085": StatusReturned,       // On way back (possible damage)
		"6086": StatusFailed,         // Delivered to wrong location, redirected
		"6087": StatusFailed,         // Delivered to wrong location
		"6088": StatusInTransit,      // Redirected at recipient request
		"6089": StatusInTransit,      // Sorted
		"6090": StatusInTransit,      // Sorted
		"6092": StatusReturned,       // On way back to client
		"6093": StatusReturned,       // On way back (not collected from ParcelShop)
		"6094": StatusReturned,       // Being returned to client
		"6096": StatusReturned,       // On way back (too big for ParcelShop)
		"6098": StatusReturned,       // On way back (missing/incorrect TAN)
		"6099": StatusReturned,       // On way back (delivery stopped)
		"7500": StatusDelivered,      // Return arrived at client
		"9340": StatusFailed,         // Shipment cancelled
		"9341": StatusFailed,         // Shipment cancelled by client
	},

	// dpd: DPD Baltic API v1 status normalization is handled by normalizeDPDStatus
	// in dpd.go. It requires statusCode + serviceCode + prevStatusCode together
	// (§6.1.4 of the API docs), so a single-key lookup here is insufficient.
	// DPD is intentionally absent from this map.

	"fedex": {
		// Sourced from FedEx Track API v1 spec (fedex_track.json).
		// Keys are derivedStatusCode / eventType values from ScanEvent.
		// FedEx does not publish a complete public enum; these codes are
		// confirmed in the spec examples and field descriptions.
		"OC": StatusBooked,         // Shipment information sent to FedEx
		"PU": StatusPickedUp,       // Picked up
		"AR": StatusInTransit,      // Arrived at FedEx location
		"DP": StatusInTransit,      // Departed FedEx location
		"IT": StatusInTransit,      // In transit
		"OD": StatusOutForDelivery, // On FedEx vehicle for delivery
		"DE": StatusDelivered,      // Delivered
		"DL": StatusFailed,         // Delivery exception
		"SE": StatusFailed,         // Shipment exception
		"CA": StatusFailed,         // Cancelled
		"RS": StatusReturned,       // Return to sender/shipper
		"HL": StatusInTransit,      // At FedEx facility (hold)
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
