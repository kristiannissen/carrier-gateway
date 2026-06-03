// Package parser provides format-specific parsers that normalise inbound
// booking requests into the unified adapter.BookingRequest.
// This file is located at /internal/parser/parser.go.
package parser

import (
	"fmt"
	"net/http"

	"github.com/kristiannissen/logistics-gateway/internal/adapter"
)

// Parser converts a raw request body into a unified BookingRequest.
type Parser interface {
	// Parse reads the body and returns a normalised BookingRequest.
	Parse(body []byte) (*adapter.BookingRequest, error)
}

// ForRequest returns the appropriate Parser for the request's Content-Type.
// Defaults to JSON if the header is absent.
func ForRequest(r *http.Request) (Parser, error) {
	ct := r.Header.Get("Content-Type")
	switch {
	case ct == "" || ct == "application/json":
		return &JSONParser{}, nil
	case ct == "application/xml" || ct == "text/xml":
		return &XMLParser{}, nil
	case ct == "application/edifact" || ct == "text/plain":
		return &EDIFACTParser{}, nil
	default:
		return nil, fmt.Errorf("unsupported Content-Type: %s", ct)
	}
}
