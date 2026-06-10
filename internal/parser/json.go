// Package parser provides format-specific parsers for inbound booking requests.
// This file is located at /internal/parser/json.go.
package parser

import (
	"encoding/json"
	"fmt"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// JSONParser parses an application/json booking request body.
type JSONParser struct{}

// Parse deserialises a JSON body into a BookingRequest.
func (p *JSONParser) Parse(body []byte) (*adapter.BookingRequest, error) {
	var req adapter.BookingRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return &req, nil
}
