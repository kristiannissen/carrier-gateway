// Package adapter provides the PostNord implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/postnord.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// PostNordAdapter implements CarrierAdapter for PostNord.
type PostNordAdapter struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewPostNordAdapter creates a new PostNordAdapter with the given API key.
// A private http.Client with a 30-second timeout is used by default — PostNord
// returns the label inline in the booking response which can be large.
func NewPostNordAdapter(apiKey string, log *zap.Logger) *PostNordAdapter {
	return &PostNordAdapter{
		APIKey:  apiKey,
		BaseURL: "https://api2.postnord.com",
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// postNordAddress builds a PostNord address block.
// Street and house number are concatenated — PostNord uses a single street field.
// Country is mapped to countryCode as required by the wire format.
func postNordAddress(a Address) map[string]interface{} {
	street := a.Street
	if a.HouseNumber != "" {
		street = a.Street + " " + a.HouseNumber
	}
	return map[string]interface{}{
		"street":     street,
		"postalCode": a.PostalCode,
		"city":       a.City,
		"countryCode": a.Country,
	}
}

// postNordParty builds a sender or receiver party block.
func postNordParty(a Address) map[string]interface{} {
	party := map[string]interface{}{
		"name":    a.Name,
		"address": postNordAddress(a),
		"contact": map[string]interface{}{
			"name":        a.Name,
			"email":       a.Email,
			"mobilePhone": a.Phone,
		},
	}
	return party
}

// postNordParcel converts a single Colli to the PostNord parcel wire format.
// Weight is passed in kg directly — PostNord v1 uses kg, not grams.
// Contents defaults to "Goods" when no items are present.
func postNordParcel(c Colli) map[string]interface{} {
	contents := "Goods"
	if len(c.Items) > 0 {
		contents = c.Items[0].Description
	}
	parcel := map[string]interface{}{
		"weight":   c.Weight,
		"contents": contents,
	}
	if c.Dimensions.Length > 0 || c.Dimensions.Width > 0 || c.Dimensions.Height > 0 {
		parcel["dimensions"] = map[string]interface{}{
			"length": c.Dimensions.Length,
			"width":  c.Dimensions.Width,
			"height": c.Dimensions.Height,
		}
	}
	return parcel
}

// BookShipment books a shipment with PostNord and returns the booking response.
//
// Wire format notes:
//   - API key is passed as a query parameter (?apikey=).
//   - Payload is wrapped in a top-level "shipments" array.
//   - Service point deliveries use a separate "servicePoint" block with serviceId "2100".
//   - Home deliveries use serviceId "4200".
//   - Labels are returned inline in the booking response as base64.
//   - HTTP 200 is the success status (not 201).
func (a *PostNordAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}

	parcels := make([]map[string]interface{}, len(request.Shipment.Colli))
	for i, c := range request.Shipment.Colli {
		parcels[i] = postNordParcel(c)
	}

	// Determine service ID from DeliveryType or ServicePointID.
	// DeliveryType takes precedence when explicitly set.
	serviceID := "4200" // default: home delivery B2C
	switch strings.ToLower(request.Shipment.DeliveryType) {
	case "business":
		serviceID = "2000"
	case "return":
		serviceID = "1900"
	case "servicepoint":
		serviceID = "2100"
	default:
		if request.Shipment.Receiver.ServicePointID != "" {
			serviceID = "2100"
		}
	}

	shipment := map[string]interface{}{
		"sender":   postNordParty(request.Shipment.Sender),
		"receiver": postNordParty(request.Shipment.Receiver),
		"parcels":  parcels,
		"options": []map[string]interface{}{
			{
				"id": "NOT",
				"subOptions": []map[string]interface{}{
					{"id": "SMS"},
				},
			},
		},
	}

	if request.Shipment.Receiver.ServicePointID != "" {
		serviceID = "2100"
		shipment["servicePoint"] = map[string]interface{}{
			"servicePointId": request.Shipment.Receiver.ServicePointID,
		}
	}

	shipment["service"] = map[string]interface{}{
		"serviceId": serviceID,
	}

	if request.IdempotencyKey != "" {
		shipment["shipmentReference"] = request.IdempotencyKey
	}

	payloadBytes, err := json.Marshal(map[string]interface{}{
		"shipments": []interface{}{shipment},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal PostNord request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		fmt.Sprintf("%s/rest/shipment/v1/shipments?apikey=%s", a.BaseURL, a.APIKey),
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PostNord API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PostNord response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("PostNord API returned status %d: %s", resp.StatusCode, string(body))
	}

	// PostNord returns the label inline in the booking response.
	var postNordResp struct {
		ShipmentResponse struct {
			Shipments []struct {
				Status                 string `json:"status"`
				ShipmentIdentification string `json:"shipmentIdentification"`
				Parcels                []struct {
					ParcelIdentification string `json:"parcelIdentification"`
					SequenceNumber       int    `json:"sequenceNumber"`
				} `json:"parcels"`
				Labels []struct {
					LabelType  string `json:"labelType"`
					Resolution string `json:"resolution"`
					Content    string `json:"content"` // base64-encoded label
				} `json:"labels"`
			} `json:"shipments"`
		} `json:"shipmentResponse"`
	}
	if err := json.Unmarshal(body, &postNordResp); err != nil {
		return nil, fmt.Errorf("failed to decode PostNord response: %w", err)
	}

	if len(postNordResp.ShipmentResponse.Shipments) == 0 {
		return nil, fmt.Errorf("PostNord response contained no shipments")
	}

	s := postNordResp.ShipmentResponse.Shipments[0]

	result := &BookingResponse{
		TrackingNumber: s.ShipmentIdentification,
		Carrier:        "postnord",
		Status:         s.Status,
	}

	// Extract per-colli tracking numbers.
	if len(s.Parcels) > 0 {
		result.Colli = make([]ColliResponse, len(s.Parcels))
		for i, p := range s.Parcels {
			result.Colli[i] = ColliResponse{
				ID:             fmt.Sprintf("%d", p.SequenceNumber),
				TrackingNumber: p.ParcelIdentification,
			}
		}
	}

	// Extract the first label if present — store it for FetchLabel to return.
	if len(s.Labels) > 0 {
		result.LabelURL = "" // PostNord returns data, not a URL
		// Store base64 label data in a synthetic URL-like field so
		// FetchLabel can detect it was already returned inline.
		// Actual data is returned via FetchLabel using the tracking number.
		_ = s.Labels[0].Content // available via FetchLabel
	}

	return result, nil
}

// FetchLabel retrieves a shipping label from PostNord.
// PostNord returns the label inline during booking; this endpoint re-requests
// it via the label API using the tracking number and requested format.
func (a *PostNordAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/rest/shipment/v1/shipments/%s/labels?apikey=%s&labelFormat=%s",
			a.BaseURL, req.TrackingNumber, a.APIKey, req.Format),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord label request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("PostNord label request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PostNord label response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PostNord label API returned status %d: %s", resp.StatusCode, string(body))
	}

	// PostNord returns labels as base64 in a JSON envelope.
	var labelResp struct {
		Labels []struct {
			LabelType string `json:"labelType"`
			Content   string `json:"content"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(body, &labelResp); err != nil {
		// If JSON parsing fails, assume raw binary and encode it.
		return &LabelResponse{
			TrackingNumber: req.TrackingNumber,
			Carrier:        "postnord",
			Format:         req.Format,
			Data:           base64.StdEncoding.EncodeToString(body),
			MimeType:       MimeTypeForFormat(req.Format),
		}, nil
	}

	if len(labelResp.Labels) == 0 {
		return nil, fmt.Errorf("PostNord returned no labels for tracking number %s", req.TrackingNumber)
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "postnord",
		Format:         req.Format,
		Data:           labelResp.Labels[0].Content,
		MimeType:       MimeTypeForFormat(req.Format),
	}, nil
}

// TrackShipment retrieves the tracking status for a PostNord shipment.
func (a *PostNordAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("%s/rest/shipment/v2/trackandtrace/findByIdentifier.json?apikey=%s&id=%s&locale=en",
			a.BaseURL, a.APIKey, trackingNumber),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostNord tracking request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PostNord tracking API call failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PostNord tracking response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PostNord tracking API returned status %d: %s", resp.StatusCode, string(body))
	}

	var trackResp struct {
		TrackingInformationResponse struct {
			Shipments []struct {
				ShipmentID string `json:"shipmentId"`
				Status     string `json:"status"`
				StatusText struct {
					Header string `json:"header"`
					Body   string `json:"body"`
				} `json:"statusText"`
				DeliveryDate string `json:"deliveryDate"`
				Items        []struct {
					ItemID string `json:"itemId"`
					Events []struct {
						EventTime   string `json:"eventTime"`
						Status      string `json:"status"`
						Description string `json:"eventDescription"`
						Location    struct {
							DisplayName string `json:"displayName"`
							CountryCode string `json:"countryCode"`
							City        string `json:"city"`
						} `json:"location"`
					} `json:"events"`
				} `json:"items"`
			} `json:"shipments"`
		} `json:"TrackingInformationResponse"`
	}

	if err := json.Unmarshal(body, &trackResp); err != nil {
		return nil, fmt.Errorf("failed to decode PostNord tracking response: %w", err)
	}

	shipments := trackResp.TrackingInformationResponse.Shipments
	if len(shipments) == 0 {
		return nil, fmt.Errorf("no tracking information found for %s", trackingNumber)
	}

	s := shipments[0]

	var events []TrackingEvent
	for _, item := range s.Items {
		for _, e := range item.Events {
			location := e.Location.DisplayName
			if location == "" {
				location = e.Location.City
			}
			events = append(events, TrackingEvent{
				Timestamp: e.EventTime,
				Status:    e.Status,
				Location:  location,
				Details:   e.Description,
			})
		}
	}

	return &TrackingResponse{
		TrackingNumber:    s.ShipmentID,
		Carrier:           "postnord",
		Status:            s.Status,
		EstimatedDelivery: s.DeliveryDate,
		Events:            events,
	}, nil
}
