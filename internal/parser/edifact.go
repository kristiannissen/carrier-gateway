// Package parser provides format-specific parsers for inbound booking requests.
// This file is located at /internal/parser/edifact.go.
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
)

// EDIFACTParser parses a UN/EDIFACT IFTMIN (booking) message body.
//
// Supported segments:
//
//	TSR — carrier service code (mapped to Carrier)
//	NAD+CZ — consignor / sender address
//	NAD+CN — consignee / receiver address
//	GID — goods item description (one per colli)
//	MEA+WT — weight per goods item
//	DIM — dimensions per goods item
//
// Segment separator: '
// Element separator: +
// Component separator: : //nolint:godot
type EDIFACTParser struct{}

// Parse deserialises a UN/EDIFACT IFTMIN message into a BookingRequest.
func (p *EDIFACTParser) Parse(body []byte) (*adapter.BookingRequest, error) {
	segments := splitSegments(string(body))

	req := &adapter.BookingRequest{}
	var currentColli *adapter.Colli
	var totalWeight float64

	for _, seg := range segments {
		elements := strings.Split(seg, "+")
		if len(elements) == 0 {
			continue
		}

		switch elements[0] {

		case "TSR": // Transport service requirement — carrier code
			if len(elements) > 1 {
				req.Carrier = strings.ToLower(strings.Split(elements[1], ":")[0])
			}

		case "NAD": // Name and address
			if len(elements) < 3 {
				continue
			}
			qualifier := elements[1]
			addr := parseNADAddress(elements)
			switch qualifier {
			case "CZ": // Consignor = sender
				req.Shipment.Sender = addr
			case "CN": // Consignee = receiver
				req.Shipment.Receiver = addr
			}

		case "GID": // Goods item description — start of a new colli
			if currentColli != nil {
				req.Shipment.Colli = append(req.Shipment.Colli, *currentColli)
			}
			id := fmt.Sprintf("%d", len(req.Shipment.Colli)+1)
			currentColli = &adapter.Colli{ID: id}

		case "MEA": // Measurements
			if len(elements) < 4 || currentColli == nil {
				continue
			}
			if elements[1] == "WT" { // Weight
				components := strings.Split(elements[3], ":")
				if len(components) > 1 {
					w, err := strconv.ParseFloat(components[1], 64)
					if err == nil {
						currentColli.Weight = w
						totalWeight += w
					}
				}
			}

		case "DIM": // Dimensions
			if len(elements) < 3 || currentColli == nil {
				continue
			}
			components := strings.Split(elements[2], ":")
			// DIM+1+CMT:length:width:height
			if len(components) >= 4 {
				length, _ := strconv.ParseFloat(components[1], 64)
				width, _ := strconv.ParseFloat(components[2], 64)
				height, _ := strconv.ParseFloat(components[3], 64)
				currentColli.Dimensions = adapter.Dimensions{
					Length: length,
					Width:  width,
					Height: height,
				}
			}
		}
	}

	// Append the last colli
	if currentColli != nil {
		req.Shipment.Colli = append(req.Shipment.Colli, *currentColli)
	}

	req.Shipment.TotalWeight = totalWeight

	if req.Carrier == "" {
		return nil, fmt.Errorf("EDIFACT message missing TSR segment (carrier)")
	}
	if len(req.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("EDIFACT message contains no GID segments (colli)")
	}

	return req, nil
}

// splitSegments splits an EDIFACT message on the segment terminator "'".
func splitSegments(msg string) []string {
	raw := strings.Split(msg, "'")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// parseNADAddress extracts an adapter.Address from a NAD segment's elements.
// NAD+qualifier+partyID::codelist+name+street+city+state+postalCode+country
// HouseNumber and Supplement are not available in standard IFTMIN NAD segments
// and are left empty; senders requiring them should use JSON or XML format.
func parseNADAddress(elements []string) adapter.Address {
	get := func(i int) string {
		if i < len(elements) {
			return strings.ReplaceAll(elements[i], ":", "")
		}
		return ""
	}
	return adapter.Address{
		Name:       get(3),
		Street:     get(4),
		City:       get(5),
		State:      get(6),
		PostalCode: get(7),
		Country:    get(8),
	}
}
