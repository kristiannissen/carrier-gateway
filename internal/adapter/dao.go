// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/dao.go.
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
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
	params.Set("navn", request.Shipment.Receiver.Name)
	params.Set("mobil", request.Shipment.Receiver.Phone)
	params.Set("email", request.Shipment.Receiver.Email)
	params.Set("dato", time.Now().AddDate(0, 0, 1).Format("2006-01-02"))
	params.Set("vaegt", strconv.Itoa(int(math.Round(request.Shipment.Colli[0].Weight*1000))))
	params.Set("l", strconv.Itoa(int(request.Shipment.Colli[0].Dimensions.Length)))
	params.Set("h", strconv.Itoa(int(request.Shipment.Colli[0].Dimensions.Height)))
	params.Set("b", strconv.Itoa(int(request.Shipment.Colli[0].Dimensions.Width)))
	params.Set("faktura", request.Shipment.Colli[0].ID)
	params.Set("format", "json")

	if request.Shipment.Receiver.ServicePointID != "" {
		params.Set("lockerId", request.Shipment.Receiver.ServicePointID)
	} else {
		params.Set("postnr", request.Shipment.Receiver.PostalCode)
		params.Set("adresse", request.Shipment.Receiver.Street)
	}

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
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

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

	barcode := daoResponse.Result.Barcode

	// If SMS or email add-ons are requested, call the separate contact update
	// endpoint after the booking is confirmed. DAO does not support add-ons
	// in the initial booking payload.
	if barcode != "" && (hasAddOn(request.Shipment.AddOns, AddOnSMSNotification) ||
		hasAddOn(request.Shipment.AddOns, AddOnEmailNotification)) {
		if err := a.updateContactInfo(ctx, barcode, request.Shipment.Receiver.Phone, request.Shipment.Receiver.Email); err != nil {
			// Log but do not fail the booking — the shipment is created.
			if a.log != nil {
				a.log.Warn("DAO contact update failed after successful booking",
					zap.String("barcode", barcode),
					zap.Error(err),
				)
			}
		}
	}

	if hasAddOn(request.Shipment.AddOns, AddOnFlexDelivery) {
		return nil, fmt.Errorf("DAO does not support flex delivery")
	}

	return &BookingResponse{
		TrackingNumber: barcode,
		LabelURL:       "",
		Carrier:        "dao",
	}, nil
}

// updateContactInfo calls the DAO OpdaterKontaktOplysning endpoint to add
// SMS and/or email notification to an already-booked shipment.
// This must be called before the parcel is scanned at a DAO terminal.
func (a *DAOAdapter) updateContactInfo(ctx context.Context, barcode, phone, email string) error {
	params := url.Values{}
	params.Set("kundeid", a.CustomerID)
	params.Set("kode", a.APIKey)
	params.Set("stregkode", barcode)
	params.Set("format", "json")
	if phone != "" {
		params.Set("mobil", phone)
	}
	if email != "" {
		params.Set("email", email)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		a.BaseURL+"/DAOPakkeshop/OpdaterKontaktOplysning.php?"+params.Encode(),
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create DAO contact update request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("DAO contact update request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read DAO contact update response: %w", err)
	}

	var updateResp struct {
		Status    string `json:"status"`
		ErrorCode string `json:"fejlkode"`
		ErrorText string `json:"fejltekst"`
	}
	if err := json.Unmarshal(body, &updateResp); err != nil {
		return fmt.Errorf("failed to unmarshal DAO contact update response: %w", err)
	}

	if updateResp.Status != "OK" {
		return fmt.Errorf("DAO contact update failed: %s (%s)", updateResp.ErrorText, updateResp.ErrorCode)
	}

	return nil
}

// FetchLabel is not yet available for DAO.
// DAO label support is under investigation; labels must currently be
// downloaded from the DAO portal directly.
func (a *DAOAdapter) FetchLabel(_ context.Context, _ LabelRequest) (*LabelResponse, error) {
	return nil, fmt.Errorf("DAO label support is under investigation and not yet available; download labels from the DAO portal")
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
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

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
