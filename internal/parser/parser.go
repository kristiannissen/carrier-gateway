// Package parser provides format-specific parsers that normalise inbound
// booking requests into the unified adapter.BookingRequest.
// This file is located at /internal/parser/parser.go.
package parser

import (
	"fmt"
	"mime"
	"net/http"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// Parser converts a raw request body into a unified BookingRequest.
type Parser interface {
	// Parse reads the body and returns a normalised BookingRequest.
	Parse(body []byte) (*adapter.BookingRequest, error)
}

// ForRequest returns the appropriate Parser for the request's Content-Type.
// Defaults to JSON when the header is absent or empty.
// Uses mime.ParseMediaType so that charset parameters (e.g.
// "application/json; charset=utf-8") are handled correctly.
func ForRequest(r *http.Request) (Parser, error) {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return &JSONParser{}, nil
	}

	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		// Unparseable Content-Type — fall back to JSON rather than rejecting.
		return &JSONParser{}, nil
	}

	switch mediaType {
	case "application/json":
		return &JSONParser{}, nil
	case "application/xml", "text/xml":
		return &XMLParser{}, nil
	case "application/edifact", "text/plain":
		return &EDIFACTParser{}, nil
	default:
		return nil, fmt.Errorf("unsupported Content-Type: %s", mediaType)
	}
}
