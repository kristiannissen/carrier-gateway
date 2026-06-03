// Package parser provides format-specific parsers for inbound booking requests.
// This file is located at /internal/parser/parser_test.go.
package parser

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForRequest_DefaultsToJSON(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	p, err := ForRequest(r)
	require.NoError(t, err)
	assert.IsType(t, &JSONParser{}, p)
}

func TestForRequest_XML(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Content-Type", "application/xml")
	p, err := ForRequest(r)
	require.NoError(t, err)
	assert.IsType(t, &XMLParser{}, p)
}

func TestForRequest_EDIFACT(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Content-Type", "application/edifact")
	p, err := ForRequest(r)
	require.NoError(t, err)
	assert.IsType(t, &EDIFACTParser{}, p)
}

func TestForRequest_UnsupportedContentType(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Content-Type", "text/csv")
	_, err := ForRequest(r)
	assert.Error(t, err)
}

func TestJSONParser(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"carrier": "postnord",
		"shipment": {
			"sender":   {"name":"Sender","street":"Street 1","city":"Copenhagen","postalCode":"2300","country":"DK"},
			"receiver": {"name":"Receiver","street":"Street 2","city":"Stockholm","postalCode":"111 22","country":"SE"},
			"totalWeight": 2.5,
			"colli": [{"id":"c1","weight":2.5,"dimensions":{"length":30,"width":20,"height":10}}]
		}
	}`)
	req, err := (&JSONParser{}).Parse(body)
	require.NoError(t, err)
	assert.Equal(t, "postnord", req.Carrier)
	assert.Equal(t, "Sender", req.Shipment.Sender.Name)
	assert.Len(t, req.Shipment.Colli, 1)
	assert.Equal(t, 2.5, req.Shipment.TotalWeight)
}

func TestXMLParser(t *testing.T) {
	t.Parallel()
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<BookingRequest>
  <Carrier>postnord</Carrier>
  <Shipment>
    <Sender>
      <Name>Sender Shop</Name>
      <Street>Industrivej 10</Street>
      <City>Copenhagen</City>
      <PostalCode>2300</PostalCode>
      <Country>DK</Country>
    </Sender>
    <Receiver>
      <Name>John Doe</Name>
      <Street>Storgatan 1</Street>
      <City>Stockholm</City>
      <PostalCode>111 22</PostalCode>
      <Country>SE</Country>
    </Receiver>
    <TotalWeight>2.5</TotalWeight>
    <Colli>
      <Item>
        <ID>c1</ID>
        <Weight>2.5</Weight>
        <Dimensions>
          <Length>30</Length>
          <Width>20</Width>
          <Height>10</Height>
        </Dimensions>
      </Item>
    </Colli>
  </Shipment>
</BookingRequest>`)

	req, err := (&XMLParser{}).Parse(body)
	require.NoError(t, err)
	assert.Equal(t, "postnord", req.Carrier)
	assert.Equal(t, "Sender Shop", req.Shipment.Sender.Name)
	assert.Equal(t, "DK", req.Shipment.Sender.Country)
	assert.Equal(t, "John Doe", req.Shipment.Receiver.Name)
	assert.Equal(t, float64(2.5), req.Shipment.TotalWeight)
	require.Len(t, req.Shipment.Colli, 1)
	assert.Equal(t, float64(30), req.Shipment.Colli[0].Dimensions.Length)
}

func TestEDIFACTParser(t *testing.T) {
	t.Parallel()
	// Minimal IFTMIN message
	body := []byte(
		"UNB+UNOA:1+SENDER+RECEIVER+260603:1000+1'" +
		"TSR+POSTNORD:1'" +
		"NAD+CZ+::91+Sender Shop+Industrivej 10+Copenhagen++2300+DK'" +
		"NAD+CN+::91+John Doe+Storgatan 1+Stockholm++111 22+SE'" +
		"GID+1'" +
		"MEA+WT+AAB+KGM:2.5'" +
		"DIM+1+CMT:30:20:10'" +
		"UNZ+1+1'",
	)

	req, err := (&EDIFACTParser{}).Parse(body)
	require.NoError(t, err)
	assert.Equal(t, "postnord", req.Carrier)
	assert.Equal(t, "Sender Shop", req.Shipment.Sender.Name)
	assert.Equal(t, "2300", req.Shipment.Sender.PostalCode)
	assert.Equal(t, "John Doe", req.Shipment.Receiver.Name)
	require.Len(t, req.Shipment.Colli, 1)
	assert.Equal(t, 2.5, req.Shipment.Colli[0].Weight)
	assert.Equal(t, float64(30), req.Shipment.Colli[0].Dimensions.Length)
	assert.Equal(t, 2.5, req.Shipment.TotalWeight)
}

func TestEDIFACTParser_MissingCarrier(t *testing.T) {
	t.Parallel()
	body := []byte("GID+1'MEA+WT+AAB+KGM:2.5'")
	_, err := (&EDIFACTParser{}).Parse(body)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TSR segment")
}

func TestEDIFACTParser_NoColli(t *testing.T) {
	t.Parallel()
	body := []byte("TSR+POSTNORD:1'NAD+CZ+++Sender'")
	_, err := (&EDIFACTParser{}).Parse(body)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "GID segment")
}
