// Package adapter provides interfaces and implementations for carrier integrations.
// This file is located at /internal/adapter/dao.go.
package adapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// DAOAdapter implements the CarrierAdapter interface for DAO.
type DAOAdapter struct {
	CustomerID string
	APIKey     string
	BaseURL    string
	TestMode   bool // when true, adds test=1 to all requests — mimics production without real bookings
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewDAOAdapter creates a new DAOAdapter instance.
// Set testMode to true to add test=1 on every request, which prevents real bookings
// while returning production-equivalent responses.
func NewDAOAdapter(customerID, apiKey string, testMode bool, log *zap.Logger) *DAOAdapter {
	return &DAOAdapter{
		CustomerID: customerID,
		APIKey:     apiKey,
		BaseURL:    "https://api.dao.as",
		TestMode:   testMode,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log,
	}
}

// daoBaseParams builds the common authentication parameters required on every DAO request.
func (a *DAOAdapter) daoBaseParams() url.Values {
	params := url.Values{}
	params.Set("kundeid", a.CustomerID)
	params.Set("kode", a.APIKey)
	params.Set("format", "json")
	return params
}

// daoBookingParams returns base params with test=1 appended when TestMode is enabled.
// Only booking endpoints (leveringsordre, returordre, OpdaterVaegt) document support
// for test=1 — do not use this for tracking, label, cancel, or contact-update calls.
func (a *DAOAdapter) daoBookingParams() url.Values {
	params := a.daoBaseParams()
	if a.TestMode {
		params.Set("test", "1")
	}
	return params
}

// daoParcelParams adds parcel weight, dimensions and invoice reference to a params set.
func daoParcelParams(params url.Values, colli Colli) {
	params.Set("vaegt", strconv.Itoa(int(math.Round(colli.Weight*1000)))) // kg → grams
	params.Set("l", strconv.Itoa(int(colli.Dimensions.Length)))
	params.Set("h", strconv.Itoa(int(colli.Dimensions.Height)))
	params.Set("b", strconv.Itoa(int(colli.Dimensions.Width)))
	params.Set("faktura", colli.ID)
}

// daoSenderParams adds optional sender fields to a params set.
func daoSenderParams(params url.Values, sender Address) {
	if sender.Name != "" {
		params.Set("afsender_navn", sender.Name)
	}
	street := sender.Street
	if sender.HouseNumber != "" {
		street = sender.Street + " " + sender.HouseNumber
	}
	if street != "" {
		params.Set("afsender_adresse", street)
	}
	if sender.PostalCode != "" {
		params.Set("afsender_postnr", sender.PostalCode)
	}
	if sender.Email != "" {
		params.Set("afsender_email", sender.Email)
	}
	if sender.Phone != "" {
		params.Set("afsender_mobil", sender.Phone)
	}
}

// daoParseBookingResponse unmarshals a standard DAO booking response body.
func daoParseBookingResponse(body []byte) (barcode, labelText1, labelText2, labelText3, labellessCode string, err error) {
	var resp struct {
		Status    string `json:"status"`
		ErrorCode string `json:"fejlkode"`
		ErrorText string `json:"fejltekst"`
		Result    struct {
			Barcode       string `json:"stregkode"`
			LabellessCode string `json:"labellesskode"`
			LabelText1    string `json:"labelTekst1"`
			LabelText2    string `json:"labelTekst2"`
			LabelText3    string `json:"labelTekst3"`
			SortingCode   string `json:"udsorting"`
			ETA           string `json:"ETA"`
		} `json:"resultat"`
	}
	if err = json.Unmarshal(body, &resp); err != nil {
		return
	}
	if resp.Status != "OK" {
		err = fmt.Errorf("DAO API error: %s (%s)", resp.ErrorText, resp.ErrorCode)
		return
	}
	barcode = resp.Result.Barcode
	labelText1 = resp.Result.LabelText1
	labelText2 = resp.Result.LabelText2
	labelText3 = resp.Result.LabelText3
	labellessCode = resp.Result.LabellessCode
	return
}

// BookShipment books a shipment with DAO.
//
// Routing:
//   - DeliveryType "return" → /DAOPakkeshop/returordre.php
//   - ServicePointID set → /DAOPakkeshop/leveringsordre.php (shop delivery)
//   - Default → /DAODirekte/leveringsordre.php (home delivery)
//
// DAO does not support flex delivery. Weight is in grams on the wire.
// Sender fields are forwarded as optional parameters.
// SMS/email add-ons are applied via a separate post-booking call to
// OpdaterKontaktOplysning.php before the parcel reaches the first terminal.
func (a *DAOAdapter) BookShipment(ctx context.Context, request BookingRequest) (*BookingResponse, error) {
	if len(request.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("shipment must contain at least one colli")
	}
	if hasAddOn(request.Shipment.AddOns, AddOnFlexDelivery) {
		return nil, fmt.Errorf("DAO does not support flex delivery")
	}
	if hasAddOn(request.Shipment.AddOns, AddOnSignatureRequired) {
		return nil, fmt.Errorf("DAO does not support signature required")
	}
	if hasAddOn(request.Shipment.AddOns, AddOnCashOnDelivery) {
		return nil, fmt.Errorf("DAO does not support cash on delivery")
	}
	if hasAddOn(request.Shipment.AddOns, AddOnInsurance) {
		return nil, fmt.Errorf("DAO does not support insurance")
	}

	isReturn := strings.EqualFold(request.Shipment.DeliveryType, "return")
	isShop := request.Shipment.Receiver.ServicePointID != "" &&
		!strings.EqualFold(request.Shipment.DeliveryType, "return")

	var (
		endpoint      string
		params        = a.daoBookingParams()
		barcode       string
		labellessCode string
	)

	switch {
	case isReturn:
		// Return order — customer drops parcel at a daoSHOP.
		endpoint = a.BaseURL + "/DAOPakkeshop/returordre.php"

		// type: "labelless" or "withlabel" — map from ReturnFunctionality.
		returnType := "labelless"
		if strings.EqualFold(request.Shipment.ReturnFunctionality, "withlabel") {
			returnType = "withlabel"
		}
		params.Set("type", returnType)
		params.Set("navn", request.Shipment.Receiver.Name)
		params.Set("postnr", request.Shipment.Receiver.PostalCode)
		params.Set("adresse", request.Shipment.Receiver.Street)
		params.Set("afsender", request.Shipment.Sender.Name)
		if request.Shipment.Sender.Phone != "" {
			params.Set("afs_mobil", request.Shipment.Sender.Phone)
		}
		if request.Shipment.Sender.Email != "" {
			params.Set("afs_email", request.Shipment.Sender.Email)
		}
		if request.IdempotencyKey != "" {
			params.Set("faktura", request.IdempotencyKey)
		}

	case isShop:
		// Shop delivery — parcel delivered to a specific daoSHOP.
		endpoint = a.BaseURL + "/DAOPakkeshop/leveringsordre.php"
		params.Set("shopid", request.Shipment.Receiver.ServicePointID)
		params.Set("navn", request.Shipment.Receiver.Name)
		params.Set("mobil", request.Shipment.Receiver.Phone)
		params.Set("email", request.Shipment.Receiver.Email)
		params.Set("dato", time.Now().AddDate(0, 0, 1).Format("2006-01-02"))
		params.Set("postnr", request.Shipment.Receiver.PostalCode)
		params.Set("adresse", request.Shipment.Receiver.Street)
		daoParcelParams(params, request.Shipment.Colli[0])
		daoSenderParams(params, request.Shipment.Sender)

	default:
		// Home delivery — parcel delivered directly to recipient address.
		endpoint = a.BaseURL + "/DAODirekte/leveringsordre.php"
		params.Set("navn", request.Shipment.Receiver.Name)
		params.Set("mobil", request.Shipment.Receiver.Phone)
		params.Set("email", request.Shipment.Receiver.Email)
		params.Set("dato", time.Now().AddDate(0, 0, 1).Format("2006-01-02"))
		params.Set("postnr", request.Shipment.Receiver.PostalCode)
		params.Set("adresse", request.Shipment.Receiver.Street)
		daoParcelParams(params, request.Shipment.Colli[0])
		daoSenderParams(params, request.Shipment.Sender)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DAO request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DAO request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read DAO response: %w", err)
	}

	barcode, _, _, _, labellessCode, err = daoParseBookingResponse(body)
	if err != nil {
		return nil, fmt.Errorf("dao: parse booking response: %w", err)
	}

	result := &BookingResponse{
		TrackingNumber: barcode,
		Carrier:        "dao",
	}

	// For labelless returns, surface the code the customer writes on the parcel.
	if labellessCode != "" {
		result.Colli = []ColliResponse{
			{ID: barcode, TrackingNumber: barcode, LabelURL: labellessCode, Status: "booked"},
		}
	}

	// For non-return bookings apply SMS/email add-ons via separate endpoint.
	// DAO requires a post-booking call to OpdaterKontaktOplysning.php before
	// the parcel is scanned at the first terminal.
	// If the call fails the shipment is booked but notifications are not active.
	// The failure is surfaced in AddOnWarnings so the caller can retry via
	// PATCH /api/bookings/{trackingNumber}?carrier=dao.
	if !isReturn && barcode != "" &&
		(hasAddOn(request.Shipment.AddOns, AddOnSMSNotification) ||
			hasAddOn(request.Shipment.AddOns, AddOnEmailNotification)) {
		if updateErr := a.updateContactInfo(ctx, barcode,
			request.Shipment.Receiver.Phone,
			request.Shipment.Receiver.Email); updateErr != nil {
			if a.log != nil {
				a.log.Warn("DAO contact update failed after successful booking",
					zap.String("barcode", barcode),
					zap.Error(updateErr),
				)
			}
			result.AddOnWarnings = append(result.AddOnWarnings,
				fmt.Sprintf("sms_notification/email_notification: contact update failed — %s. "+
					"Retry via PATCH /api/bookings/%s?carrier=dao with phone and email.",
					updateErr.Error(), barcode),
			)
		}
	}

	return result, nil
}

// updateContactInfo calls OpdaterKontaktOplysning.php to add SMS/email
// notification to an already-booked shipment before it reaches a DAO terminal.
func (a *DAOAdapter) updateContactInfo(ctx context.Context, barcode, phone, email string) error {
	params := a.daoBaseParams()
	params.Set("stregkode", barcode)
	if phone != "" {
		params.Set("mobil", phone)
	}
	if email != "" {
		params.Set("email", email)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.BaseURL+"/DAOPakkeshop/OpdaterKontaktOplysning.php?"+params.Encode(), nil)
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

// CancelShipment cancels a DAO shipment via AnnullerePakke.php.
// The parcel must not yet have been scanned at a DAO terminal.
func (a *DAOAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	params := a.daoBaseParams()
	params.Set("stregkode", trackingNumber)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.BaseURL+"/AnnullerePakke.php?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DAO cancel request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DAO cancel request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read DAO cancel response: %w", err)
	}

	var daoResp struct {
		Status    string `json:"status"`
		ErrorCode string `json:"fejlkode"`
		ErrorText string `json:"fejltekst"`
	}
	if err := json.Unmarshal(body, &daoResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DAO cancel response: %w", err)
	}
	if daoResp.Status != "OK" {
		return nil, fmt.Errorf("DAO cancel failed: %s (%s)", daoResp.ErrorText, daoResp.ErrorCode)
	}

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "dao",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment applies partial updates to a DAO shipment.
// Supported fields: ReceiverPhone, ReceiverEmail (via OpdaterKontaktOplysning.php),
// Weight (via OpdaterVaegt.php), ServicePointID (via OpdaterShopid.php).
// All updates must happen before the parcel is scanned at a DAO terminal.
func (a *DAOAdapter) UpdateShipment(ctx context.Context, req UpdateRequest) (*UpdateResponse, error) {
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}
	if req.ReceiverPhone == "" && req.ReceiverEmail == "" && req.Weight == 0 && req.ServicePointID == "" {
		return nil, fmt.Errorf("at least one field must be specified for update")
	}

	var updatedFields []string

	// Contact update — phone and/or email.
	if req.ReceiverPhone != "" || req.ReceiverEmail != "" {
		if err := a.updateContactInfo(ctx, req.TrackingNumber, req.ReceiverPhone, req.ReceiverEmail); err != nil {
			return nil, fmt.Errorf("DAO contact update failed: %w", err)
		}
		if req.ReceiverPhone != "" {
			updatedFields = append(updatedFields, "phone")
		}
		if req.ReceiverEmail != "" {
			updatedFields = append(updatedFields, "email")
		}
	}

	// Weight update.
	if req.Weight > 0 {
		if err := a.updateWeight(ctx, req.TrackingNumber, req.Weight); err != nil {
			return nil, fmt.Errorf("DAO weight update failed: %w", err)
		}
		updatedFields = append(updatedFields, "weight")
	}

	// Service point redirect.
	if req.ServicePointID != "" {
		if err := a.updateShopID(ctx, req.TrackingNumber, req.ServicePointID); err != nil {
			return nil, fmt.Errorf("DAO service point update failed: %w", err)
		}
		updatedFields = append(updatedFields, "servicePointId")
	}

	return &UpdateResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dao",
		Status:         "updated",
		UpdatedFields:  updatedFields,
	}, nil
}

// updateWeight calls OpdaterVaegt.php to change the parcel weight before first terminal scan.
func (a *DAOAdapter) updateWeight(ctx context.Context, barcode string, weightKg float64) error {
	params := a.daoBookingParams() // OpdaterVaegt.php supports test=1
	params.Set("stregkode", barcode)
	params.Set("vaegt", strconv.Itoa(int(math.Round(weightKg*1000)))) // kg → grams

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.BaseURL+"/OpdaterVaegt.php?"+params.Encode(), nil)
	if err != nil {
		return fmt.Errorf("failed to create DAO weight update request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("DAO weight update request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read DAO weight update response: %w", err)
	}

	var daoResp struct {
		Status    string `json:"status"`
		ErrorCode string `json:"fejlkode"`
		ErrorText string `json:"fejltekst"`
	}
	if err := json.Unmarshal(body, &daoResp); err != nil {
		return fmt.Errorf("failed to unmarshal DAO weight update response: %w", err)
	}
	if daoResp.Status != "OK" {
		return fmt.Errorf("DAO weight update failed: %s (%s)", daoResp.ErrorText, daoResp.ErrorCode)
	}
	return nil
}

// updateShopID calls OpdaterShopid.php to redirect a parcel to a different daoSHOP.
func (a *DAOAdapter) updateShopID(ctx context.Context, barcode, shopID string) error {
	params := a.daoBaseParams()
	params.Set("stregkode", barcode)
	params.Set("shopid", shopID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.BaseURL+"/DAOPakkeshop/OpdaterShopid.php?"+params.Encode(), nil)
	if err != nil {
		return fmt.Errorf("failed to create DAO shop update request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("DAO shop update request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read DAO shop update response: %w", err)
	}

	var daoResp struct {
		Status    string `json:"status"`
		ErrorCode string `json:"fejlkode"`
		ErrorText string `json:"fejltekst"`
	}
	if err := json.Unmarshal(body, &daoResp); err != nil {
		return fmt.Errorf("failed to unmarshal DAO shop update response: %w", err)
	}
	if daoResp.Status != "OK" {
		return fmt.Errorf("DAO shop update failed: %s (%s)", daoResp.ErrorText, daoResp.ErrorCode)
	}
	return nil
}

// FetchLabel retrieves a PDF label for a DAO shipment using HentLabel.php.
// Only PDF format is supported — DAO does not offer ZPL or other formats.
// The response is raw PDF bytes which are base64-encoded before returning.
func (a *DAOAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != LabelFormatPDF {
		return nil, unsupportedFormat("DAO", req.Format, LabelFormatPDF)
	}
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("tracking number must not be empty")
	}

	params := a.daoBaseParams()
	params.Set("stregkode", req.TrackingNumber)
	params.Set("papir", "100x150") // standard label size

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.BaseURL+"/HentLabel.php?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DAO label request: %w", err)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("DAO label request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read DAO label response: %w", err)
	}

	// DAO returns a raw PDF on success (content-type: application/pdf).
	// On error it returns a JSON envelope — detect by checking the first byte.
	if resp.StatusCode != http.StatusOK || (len(body) > 0 && body[0] == '{') {
		var errResp struct {
			Status    string `json:"status"`
			ErrorCode string `json:"fejlkode"`
			ErrorText string `json:"fejltekst"`
		}
		if jsonErr := json.Unmarshal(body, &errResp); jsonErr == nil && errResp.Status == "FEJL" {
			return nil, fmt.Errorf("DAO label error: %s (%s)", errResp.ErrorText, errResp.ErrorCode)
		}
		return nil, fmt.Errorf("DAO label API returned status %d", resp.StatusCode)
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dao",
		Format:         LabelFormatPDF,
		Data:           base64.StdEncoding.EncodeToString(body),
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// TrackShipment tracks a shipment with DAO using TrackNTrace_v2.php.
func (a *DAOAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	params := a.daoBaseParams()
	params.Set("stregkode", trackingNumber)
	params.Set("sprog", "EN") // English descriptions

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		a.BaseURL+"/TrackNTrace_v2.php?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DAO tracking request: %w", err)
	}

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DAO tracking request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails after reading

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read DAO tracking response: %w", err)
	}

	var daoResp struct {
		Status    string `json:"status"`
		ErrorCode string `json:"fejlkode"`
		ErrorText string `json:"fejltekst"`
		Result    struct {
			TrackingNumber   string `json:"stregkode"`
			ParcelType       string `json:"pakketype"`
			ETA              string `json:"eta"`
			ExternalTracking string `json:"ekstern_tracking"`
			Receiver         struct {
				Name    string `json:"navn"`
				Address string `json:"adresse"`
				Postal  string `json:"post"`
				Country string `json:"land"`
			} `json:"modtager"`
			Events []struct {
				Timestamp   string `json:"tidspunkt"`
				EventCode   string `json:"haendelse"`
				Description string `json:"beskrivelse"`
				ParcelType  string `json:"pakketype"`
				Location    string `json:"sted"`
				ShopID      string `json:"shopid"`
			} `json:"haendelser"`
		} `json:"resultat"`
	}

	if err := json.Unmarshal(body, &daoResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal DAO tracking response: %w", err)
	}
	if daoResp.Status != "OK" {
		return nil, fmt.Errorf("DAO API error: %s (%s)", daoResp.ErrorText, daoResp.ErrorCode)
	}

	events := make([]TrackingEvent, len(daoResp.Result.Events))
	for i, e := range daoResp.Result.Events {
		details := e.Description
		location := e.Location
		events[i] = TrackingEvent{
			Timestamp:        e.Timestamp,
			Status:           e.EventCode,
			NormalizedStatus: normalizeStatus("dao", e.EventCode),
			Location:         location,
			Details:          details,
		}
	}

	// Use the most recent event description as the overall status.
	rawStatus := daoResp.Result.ParcelType
	if len(daoResp.Result.Events) > 0 {
		rawStatus = daoResp.Result.Events[0].EventCode
	}

	return &TrackingResponse{
		TrackingNumber:   daoResp.Result.TrackingNumber,
		Carrier:          "dao",
		Status:           rawStatus,
		NormalizedStatus: normalizeStatus("dao", rawStatus),
		OriginalStatus:   rawStatus,
		Events:           events,
	}, nil
}
