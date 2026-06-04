// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/dao.go.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// DAOAdapter implements the CarrierAdapter interface for DAO.
type DAOAdapter struct {
	CustomerID string
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewDAOAdapter creates a new DAOAdapter instance.
// A private http.Client with a 10-second transport timeout is used by default;
// callers may inject their own client via the HTTPClient field for testing or
// custom timeout budgets.
func NewDAOAdapter(customerID, apiKey string, log *zap.Logger) *DAOAdapter {
	return &DAOAdapter{
		CustomerID: customerID,
		APIKey:     apiKey,
		BaseURL:    "https://api.dao.as",
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log,
	}
}

// BookShipment books a shipment with DAO.
func (a *DAOAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	params := url.Values{}
	params.Set("kundeid", a.CustomerID)
	params.Set("kode", a.APIKey)
	params.Set("postnr", request.Shipment.Receiver.PostalCode)
	params.Set("adresse", request.Shipment.Receiver.Street)
	params.Set("navn", request.Shipment.Receiver.Name)
	params.Set("mobil", request.Shipment.Receiver.Phone)
	params.Set("email", request.Shipment.Receiver.Email)
	params.Set("dato", "2026-06-01")                                              // Default to tomorrow
	params.Set("vaegt", strconv.Itoa(int(request.Shipment.Colli[0].Weight*1000))) // Convert kg to grams
	params.Set("l", strconv.Itoa(int(request.Shipment.Colli[0].Dimensions.Length)))
	params.Set("h", strconv.Itoa(int(request.Shipment.Colli[0].Dimensions.Height)))
	params.Set("b", strconv.Itoa(int(request.Shipment.Colli[0].Dimensions.Width)))
	params.Set("faktura", request.Shipment.Colli[0].ID)
	params.Set("format", "json")

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		a.BaseURL+"/DAODirekte/leveringsordre.php?"+params.Encode(),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var daoResponse struct {
		Status    string `json:"status"`
		ErrorCode string `json:"fejlkode"`
		ErrorText string `json:"fejltekst"`
		Result    struct {
			Barcode     string `json:"stregkode"`
			LabelText1  string `json:"labelTekst1"`
			LabelText2  string `json:"labelTekst2"`
			LabelText3  string `json:"labelTekst3"`
			SortingCode string `json:"udsorting"`
			ETA         string `json:"ETA"`
		} `json:"resultat"`
	}
	if err := json.Unmarshal(body, &daoResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if daoResponse.Status != "OK" {
		return nil, fmt.Errorf("DAO API error: %s (%s)", daoResponse.ErrorText, daoResponse.ErrorCode)
	}

	return &BookingResponse{
		TrackingNumber: daoResponse.Result.Barcode,
		LabelURL:       "", // DAO does not return a label URL directly; labels are generated separately
		Carrier:        "dao",
	}, nil
}

// TrackShipment tracks a shipment with DAO.
func (a *DAOAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	params := url.Values{}
	params.Set("kundeid", a.CustomerID)
	params.Set("kode", a.APIKey)
	params.Set("stregkode", trackingNumber)
	params.Set("format", "json")

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		a.BaseURL+"/TrackNTrace_v2.php?"+params.Encode(),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var daoTrackingResponse struct {
		Status    string `json:"status"`
		ErrorCode string `json:"fejlkode"`
		ErrorText string `json:"fejltekst"`
		Result    struct {
			TrackingNumber string `json:"stregkode"`
			ParcelType     string `json:"pakketype"`
			ETA            string `json:"eta"`
			Sender         string `json:"afsender"`
			Receiver       struct {
				Name    string `json:"navn"`
				Address string `json:"adresse"`
				Postal  string `json:"post"`
				Country string `json:"land"`
			} `json:"modtager"`
			ExternalTracking string `json:"ekstern_tracking"`
			Events           []struct {
				Timestamp   string `json:"tidspunkt"`
				Event       string `json:"haendelse"`
				Description string `json:"beskrivelse"`
				ParcelType  string `json:"pakketype"`
				Location    string `json:"sted"`
				ShopID      string `json:"shopid"`
			} `json:"haendelser"`
		} `json:"resultat"`
	}
	if err := json.Unmarshal(body, &daoTrackingResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if daoTrackingResponse.Status != "OK" {
		return nil, fmt.Errorf("DAO API error: %s (%s)", daoTrackingResponse.ErrorText, daoTrackingResponse.ErrorCode)
	}

	var events []TrackingEvent
	for _, event := range daoTrackingResponse.Result.Events {
		events = append(events, TrackingEvent{
			Timestamp: event.Timestamp,
			Status:    event.Description,
			Location:  event.Location,
		})
	}

	return &TrackingResponse{
		TrackingNumber: daoTrackingResponse.Result.TrackingNumber,
		Status:         daoTrackingResponse.Result.ParcelType,
		Events:         events,
	}, nil
}

// GetServicePoints retrieves parcel shops for DAO.
func (a *DAOAdapter) GetServicePoints(ctx context.Context, location Location) ([]ServicePoint, error) {
	params := url.Values{}
	params.Set("kundeid", a.CustomerID)
	params.Set("kode", a.APIKey)
	params.Set("postnr", location.PostalCode)
	params.Set("adresse", location.Street)
	params.Set("antal", "10") // Return up to 10 parcel shops
	params.Set("format", "json")

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		a.BaseURL+"/DAOPakkeshop/FindPakkeshop.php?"+params.Encode(),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var daoServicePoints struct {
		Status    string `json:"status"`
		ErrorCode string `json:"fejlkode"`
		ErrorText string `json:"fejltekst"`
		Result    struct {
			ServicePoints []struct {
				ShopID       string `json:"shopId"`
				Type         string `json:"type"`
				Name         string `json:"navn"`
				Address      string `json:"adresse"`
				PostalCode   string `json:"postnr"`
				City         string `json:"bynavn"`
				SortingCode  string `json:"udsorting"`
				Latitude     string `json:"latitude"`
				Longitude    string `json:"longitude"`
				Distance     string `json:"afstand"`
				OpeningHours struct {
					Monday    string `json:"man"`
					Tuesday   string `json:"tir"`
					Wednesday string `json:"ons"`
					Thursday  string `json:"tor"`
					Friday    string `json:"fre"`
					Saturday  string `json:"lor"`
					Sunday    string `json:"son"`
				} `json:"aabningstider"`
				DistanceDirect  string `json:"afstand_direkte"`
				DistanceMinutes string `json:"afstand_minutter"`
			} `json:"pakkeshops"`
			StartingPoint struct {
				Latitude           string `json:"latitude"`
				Longitude          string `json:"longitude"`
				PositionFromPostal bool   `json:"position_from_postal"`
			} `json:"udgangspunkt"`
			Count string `json:"antal"`
		} `json:"resultat"`
	}
	if err := json.Unmarshal(body, &daoServicePoints); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if daoServicePoints.Status != "OK" {
		return nil, fmt.Errorf("DAO API error: %s (%s)", daoServicePoints.ErrorText, daoServicePoints.ErrorCode)
	}

	var servicePoints []ServicePoint
	for _, sp := range daoServicePoints.Result.ServicePoints {
		servicePoints = append(servicePoints, ServicePoint{
			ID:   sp.ShopID,
			Name: sp.Name,
			Address: Address{
				Street:     sp.Address,
				PostalCode: sp.PostalCode,
				City:       sp.City,
				Country:    "DK", // DAO is primarily for Denmark
			},
		})
	}

	return servicePoints, nil
}
