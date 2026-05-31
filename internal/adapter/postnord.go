// Package adapter provides the PostNord implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/postnord.go.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"net/http"

	"github.com/go-resty/resty/v2"
)

// PostNordAdapter implements CarrierAdapter for PostNord.
type PostNordAdapter struct {
	apiKey     string
	baseURL    string
	httpClient *resty.Client
}

// NewPostNordAdapter creates a new PostNord adapter.
func NewPostNordAdapter(apiKey string) *PostNordAdapter {
	return &PostNordAdapter{
		apiKey:     apiKey,
		baseURL:    "https://api.postnord.com",
		httpClient: resty.New(),
	}
}

// BookShipment books a shipment with PostNord.
func (a *PostNordAdapter) BookShipment(request BookingRequest) (*BookingResponse, error) {
	// Map generic request to PostNord-specific format
	postNordRequest, err := a.mapToPostNord(request)
	if err != nil {
		return nil, fmt.Errorf("failed to map request: %v", err)
	}

	// Add callback URL if provided (PostNord supports webhooks)
	if request.CallbackURL != "" {
		postNordRequest.NotificationURL = request.CallbackURL
	}

	// Log warning if Idempotency-Key is provided (PostNord does not support it)
	if request.IdempotencyKey != "" {
		slog.Warn(
			"PostNord does not support idempotency. Ignoring Idempotency-Key.",
			"idempotencyKey", request.IdempotencyKey,
		)
	}

	// Call PostNord API
	resp, err := a.httpClient.R().
		SetContext(context.Background()).
		SetHeader("Authorization", "Bearer "+a.apiKey).
		SetHeader("Content-Type", "application/json").
		SetBody(postNordRequest).
		Post(a.baseURL + "/shipments")

	if err != nil {
		return nil, fmt.Errorf("PostNord API call failed: %v", err)
	}

	// Parse response
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("PostNord API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var postNordResp PostNordBookingResponse
	if err := json.Unmarshal(resp.Body(), &postNordResp); err != nil {
		return nil, fmt.Errorf("failed to parse PostNord response: %v", err)
	}

	// Map PostNord response to generic response
	return a.mapToGenericBookingResponse(postNordResp), nil
}

// TrackShipment retrieves the tracking status for a PostNord shipment.
func (a *PostNordAdapter) TrackShipment(trackingNumber string) (*TrackingResponse, error) {
	resp, err := a.httpClient.R().
		SetContext(context.Background()).
		SetHeader("Authorization", "Bearer "+a.apiKey).
		Get(fmt.Sprintf("%s/tracking/%s", a.baseURL, trackingNumber))

	if err != nil {
		return nil, fmt.Errorf("PostNord tracking API call failed: %v", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("PostNord tracking API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var postNordTrackingResp PostNordTrackingResponse
	if err := json.Unmarshal(resp.Body(), &postNordTrackingResp); err != nil {
		return nil, fmt.Errorf("failed to parse PostNord tracking response: %v", err)
	}

	return a.mapToGenericTrackingResponse(postNordTrackingResp), nil
}

// GetServicePoints retrieves PostNord service points for a location.
func (a *PostNordAdapter) GetServicePoints(location Location) ([]ServicePoint, error) {
	// PostNord API endpoint for service points
	url := fmt.Sprintf("%s/servicepoints?city=%s&postalCode=%s&country=%s",
		a.baseURL, location.City, location.PostalCode, location.Country)

	resp, err := a.httpClient.R().
		SetContext(context.Background()).
		SetHeader("Authorization", "Bearer "+a.apiKey).
		Get(url)

	if err != nil {
		return nil, fmt.Errorf("PostNord service points API call failed: %v", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("PostNord service points API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var postNordServicePoints []PostNordServicePoint
	if err := json.Unmarshal(resp.Body(), &postNordServicePoints); err != nil {
		return nil, fmt.Errorf("failed to parse PostNord service points response: %v", err)
	}

	return a.mapToGenericServicePoints(postNordServicePoints), nil
}

// --- PostNord-specific types ---

// PostNordBookingRequest represents a PostNord-specific booking request.
type PostNordBookingRequest struct {
	Sender          PostNordAddress `json:"sender"`
	Receiver        PostNordAddress `json:"receiver"`
	Colli           []PostNordColli `json:"colli"`
	Weight          float64         `json:"weight"`
	NotificationURL string          `json:"notificationUrl,omitempty"`
	ServiceLevel    string          `json:"serviceLevel,omitempty"`
	Incoterms       string          `json:"incoterms,omitempty"`
	HSCode          string          `json:"hsCode,omitempty"`
}

// PostNordAddress represents a PostNord-specific address.
type PostNordAddress struct {
	Name       string `json:"name"`
	Street     string `json:"street"`
	City       string `json:"city"`
	PostalCode string `json:"postalCode"`
	Country    string `json:"country"`
	Phone      string `json:"phone,omitempty"`
	Email      string `json:"email,omitempty"`
}

// PostNordColli represents a PostNord-specific colli.
type PostNordColli struct {
	ID         string             `json:"id"`
	Reference  string             `json:"reference,omitempty"`
	Weight     float64            `json:"weight"`
	Dimensions PostNordDimensions `json:"dimensions"`
	Items      []PostNordItem     `json:"items"`
}

// PostNordDimensions represents dimensions in PostNord's format.
type PostNordDimensions struct {
	Length float64 `json:"length"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// PostNordItem represents a PostNord-specific item.
type PostNordItem struct {
	Description string  `json:"description"`
	Weight      float64 `json:"weight"`
	Quantity    int     `json:"quantity"`
	Value       float64 `json:"value,omitempty"`
	SKU         string  `json:"sku,omitempty"`
}

// PostNordBookingResponse represents a PostNord-specific booking response.
type PostNordBookingResponse struct {
	ShipmentID     string                  `json:"shipmentId,omitempty"`
	TrackingNumber string                  `json:"trackingNumber"`
	LabelURL       string                  `json:"labelUrl,omitempty"`
	Cost           float64                 `json:"cost,omitempty"`
	Currency       string                  `json:"currency,omitempty"`
	ServiceLevel   string                  `json:"serviceLevel,omitempty"`
	Status         string                  `json:"status,omitempty"`
	Colli          []PostNordColliResponse `json:"colli,omitempty"`
}

// PostNordColliResponse represents the response for an individual colli in PostNord.
type PostNordColliResponse struct {
	ID             string `json:"id"`
	Reference      string `json:"reference,omitempty"`
	TrackingNumber string `json:"trackingNumber,omitempty"`
	LabelURL       string `json:"labelUrl,omitempty"`
	Status         string `json:"status,omitempty"`
}

// PostNordTrackingResponse represents a PostNord-specific tracking response.
type PostNordTrackingResponse struct {
	ShipmentID        string                  `json:"shipmentId,omitempty"`
	TrackingNumber    string                  `json:"trackingNumber"`
	Status            string                  `json:"status"`
	Events            []PostNordEvent         `json:"events"`
	EstimatedDelivery string                  `json:"estimatedDelivery,omitempty"`
	Colli             []PostNordColliTracking `json:"colli,omitempty"`
}

// PostNordColliTracking represents tracking for an individual colli in PostNord.
type PostNordColliTracking struct {
	ID             string          `json:"id"`
	Reference      string          `json:"reference,omitempty"`
	TrackingNumber string          `json:"trackingNumber,omitempty"`
	Status         string          `json:"status"`
	Events         []PostNordEvent `json:"events"`
}

// PostNordEvent represents a PostNord-specific tracking event.
type PostNordEvent struct {
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"`
	Location  string `json:"location,omitempty"`
	Details   string `json:"details,omitempty"`
}

// PostNordServicePoint represents a PostNord-specific service point.
type PostNordServicePoint struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Address      PostNordAddress `json:"address"`
	OpeningHours string          `json:"openingHours,omitempty"`
	Services     []string        `json:"services,omitempty"`
}

// --- Mapping functions ---

// mapToPostNord converts a generic BookingRequest to PostNord's format.
func (a *PostNordAdapter) mapToPostNord(request BookingRequest) (PostNordBookingRequest, error) {
	postNordRequest := PostNordBookingRequest{
		Sender:       mapToPostNordAddress(request.Shipment.Sender),
		Receiver:     mapToPostNordAddress(request.Shipment.Receiver),
		Colli:        mapToPostNordColli(request.Shipment.Colli),
		Weight:       request.Shipment.TotalWeight,
		ServiceLevel: "Standard", // Default service level
		Incoterms:    request.Shipment.Incoterms,
		HSCode:       request.Shipment.HSCode,
	}

	return postNordRequest, nil
}

// mapToGenericBookingResponse converts a PostNord response to a generic BookingResponse.
func (a *PostNordAdapter) mapToGenericBookingResponse(resp PostNordBookingResponse) *BookingResponse {
	bookingResponse := &BookingResponse{
		ShipmentID:     resp.ShipmentID,
		TrackingNumber: resp.TrackingNumber,
		LabelURL:       resp.LabelURL,
		Carrier:        "postnord",
		Cost:           resp.Cost,
		Currency:       resp.Currency,
		ServiceLevel:   resp.ServiceLevel,
		Status:         resp.Status,
	}

	// Map colli responses
	if len(resp.Colli) > 0 {
		bookingResponse.Colli = make([]ColliResponse, len(resp.Colli))
		for i, colli := range resp.Colli {
			bookingResponse.Colli[i] = ColliResponse{
				ID:             colli.ID,
				Reference:      colli.Reference,
				TrackingNumber: colli.TrackingNumber,
				LabelURL:       colli.LabelURL,
				Status:         colli.Status,
			}
		}
	}

	return bookingResponse
}

// mapToGenericTrackingResponse converts a PostNord tracking response to a generic TrackingResponse.
func (a *PostNordAdapter) mapToGenericTrackingResponse(resp PostNordTrackingResponse) *TrackingResponse {
	trackingResponse := &TrackingResponse{
		ShipmentID:        resp.ShipmentID,
		TrackingNumber:    resp.TrackingNumber,
		Carrier:           "postnord",
		Status:            resp.Status,
		EstimatedDelivery: resp.EstimatedDelivery,
	}

	// Map parent events
	if len(resp.Events) > 0 {
		trackingResponse.Events = make([]TrackingEvent, len(resp.Events))
		for i, event := range resp.Events {
			trackingResponse.Events[i] = TrackingEvent{
				Timestamp: event.Timestamp,
				Status:    event.Status,
				Location:  event.Location,
				Details:   event.Details,
			}
		}
	}

	// Map colli tracking
	if len(resp.Colli) > 0 {
		trackingResponse.Colli = make([]ColliTracking, len(resp.Colli))
		for i, colli := range resp.Colli {
			trackingResponse.Colli[i] = ColliTracking{
				ID:             colli.ID,
				Reference:      colli.Reference,
				TrackingNumber: colli.TrackingNumber,
				Status:         colli.Status,
			}
			// Map colli events
			if len(colli.Events) > 0 {
				trackingResponse.Colli[i].Events = make([]TrackingEvent, len(colli.Events))
				for j, event := range colli.Events {
					trackingResponse.Colli[i].Events[j] = TrackingEvent{
						Timestamp: event.Timestamp,
						Status:    event.Status,
						Location:  event.Location,
						Details:   event.Details,
					}
				}
			}
		}
	}

	return trackingResponse
}

// mapToGenericServicePoints converts PostNord service points to generic ServicePoint.
func (a *PostNordAdapter) mapToGenericServicePoints(points []PostNordServicePoint) []ServicePoint {
	servicePoints := make([]ServicePoint, len(points))
	for i, p := range points {
		servicePoints[i] = ServicePoint{
			ID:           p.ID,
			Name:         p.Name,
			Address:      mapToGenericAddress(p.Address),
			OpeningHours: p.OpeningHours,
			Services:     p.Services,
		}
	}
	return servicePoints
}

// --- Helper functions ---

func mapToPostNordAddress(addr Address) PostNordAddress {
	return PostNordAddress{
		Name:       addr.Name,
		Street:     addr.Street,
		City:       addr.City,
		PostalCode: addr.PostalCode,
		Country:    addr.Country,
		Phone:      addr.Phone,
		Email:      addr.Email,
	}
}

func mapToPostNordColli(colli []Colli) []PostNordColli {
	postNordColli := make([]PostNordColli, len(colli))
	for i, c := range colli {
		postNordColli[i] = PostNordColli{
			ID:        c.ID,
			Reference: c.Reference,
			Weight:    c.Weight,
			Dimensions: PostNordDimensions{
				Length: c.Dimensions.Length,
				Width:  c.Dimensions.Width,
				Height: c.Dimensions.Height,
			},
			Items: mapToPostNordItems(c.Items),
		}
	}
	return postNordColli
}

func mapToPostNordItems(items []Item) []PostNordItem {
	postNordItems := make([]PostNordItem, len(items))
	for i, item := range items {
		postNordItems[i] = PostNordItem{
			Description: item.Description,
			Weight:      item.Weight,
			Quantity:    item.Quantity,
			Value:       item.Value,
			SKU:         item.SKU,
		}
	}
	return postNordItems
}

func mapToGenericAddress(addr PostNordAddress) Address {
	return Address{
		Name:       addr.Name,
		Street:     addr.Street,
		City:       addr.City,
		PostalCode: addr.PostalCode,
		Country:    addr.Country,
		Phone:      addr.Phone,
		Email:      addr.Email,
	}
}

// NewPostNordAdapterFromEnv creates a new PostNord adapter from environment variables.
// Falls back to mock mode if POSTNORD_API_KEY is not set.
func NewPostNordAdapterFromEnv() *PostNordAdapter {
	apiKey := os.Getenv("POSTNORD_API_KEY")
	if apiKey == "" {
		slog.Warn("POSTNORD_API_KEY not set. Falling back to mock mode.")
		return nil
	}
	return NewPostNordAdapter(apiKey)
}
