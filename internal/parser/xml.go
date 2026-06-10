// Package parser provides format-specific parsers for inbound booking requests.
// This file is located at /internal/parser/xml.go.
package parser

import (
	"encoding/xml"
	"fmt"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

// XMLParser parses an application/xml booking request body.
type XMLParser struct{}

// xmlBookingRequest is the XML envelope for an inbound booking request.
// Element names follow a generic logistics XML convention; adjust to match
// the spec of the system sending XML to this gateway.
type xmlBookingRequest struct {
	XMLName  xml.Name    `xml:"BookingRequest"`
	Carrier  string      `xml:"Carrier"`
	Shipment xmlShipment `xml:"Shipment"`
}

type xmlShipment struct {
	Sender      xmlAddress `xml:"Sender"`
	Receiver    xmlAddress `xml:"Receiver"`
	TotalWeight float64    `xml:"TotalWeight"`
	Colli       []xmlColli `xml:"Colli>Item"`
}

type xmlAddress struct {
	Name        string `xml:"Name"`
	Street      string `xml:"Street"`
	HouseNumber string `xml:"HouseNumber,omitempty"`
	Supplement  string `xml:"Supplement,omitempty"`
	City        string `xml:"City"`
	PostalCode  string `xml:"PostalCode"`
	Country     string `xml:"Country"`
	State       string `xml:"State,omitempty"`
	Phone       string `xml:"Phone,omitempty"`
	Email       string `xml:"Email,omitempty"`
}

type xmlColli struct {
	ID     string  `xml:"ID"`
	Weight float64 `xml:"Weight"`
	Length float64 `xml:"Dimensions>Length"`
	Width  float64 `xml:"Dimensions>Width"`
	Height float64 `xml:"Dimensions>Height"`
}

// Parse deserialises an XML body into a BookingRequest.
func (p *XMLParser) Parse(body []byte) (*adapter.BookingRequest, error) {
	var x xmlBookingRequest
	if err := xml.Unmarshal(body, &x); err != nil {
		return nil, fmt.Errorf("invalid XML: %w", err)
	}

	colli := make([]adapter.Colli, len(x.Shipment.Colli))
	for i, c := range x.Shipment.Colli {
		colli[i] = adapter.Colli{
			ID:     c.ID,
			Weight: c.Weight,
			Dimensions: adapter.Dimensions{
				Length: c.Length,
				Width:  c.Width,
				Height: c.Height,
			},
		}
	}

	return &adapter.BookingRequest{
		Carrier: x.Carrier,
		Shipment: adapter.Shipment{
			Sender: adapter.Address{
				Name:        x.Shipment.Sender.Name,
				Street:      x.Shipment.Sender.Street,
				HouseNumber: x.Shipment.Sender.HouseNumber,
				Supplement:  x.Shipment.Sender.Supplement,
				City:        x.Shipment.Sender.City,
				PostalCode:  x.Shipment.Sender.PostalCode,
				Country:     x.Shipment.Sender.Country,
				State:       x.Shipment.Sender.State,
				Phone:       x.Shipment.Sender.Phone,
				Email:       x.Shipment.Sender.Email,
			},
			Receiver: adapter.Address{
				Name:        x.Shipment.Receiver.Name,
				Street:      x.Shipment.Receiver.Street,
				HouseNumber: x.Shipment.Receiver.HouseNumber,
				Supplement:  x.Shipment.Receiver.Supplement,
				City:        x.Shipment.Receiver.City,
				PostalCode:  x.Shipment.Receiver.PostalCode,
				Country:     x.Shipment.Receiver.Country,
				State:       x.Shipment.Receiver.State,
				Phone:       x.Shipment.Receiver.Phone,
				Email:       x.Shipment.Receiver.Email,
			},
			TotalWeight: x.Shipment.TotalWeight,
			Colli:       colli,
		},
	}, nil
}
