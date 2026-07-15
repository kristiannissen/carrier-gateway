// Package adapter provides the Omniva OMX implementation of CarrierAdapter and ManifestAdapter.
// This file is located at /internal/adapter/omniva.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/kristiannissen/carrier-gateway/internal/notification"
)

const (
	omnivaLiveBase = "https://omx.omniva.eu"
	omnivaTestBase = "https://test-omx.omniva.eu"

	// omnivaRateLimit is the maximum number of tracking requests per window.
	// Omniva enforces 5 requests per 5 minutes per account on the TRT endpoint.
	// omnivaEventPollSize is the number of events requested per poll cycle.
	omnivaEventPollSize = 100

	// omnivaMainServiceParcel is the default main service code for parcel shipments.
	omnivaMainServiceParcel = "PARCEL"
	omnivaMainServiceLetter = "LETTER"
	omnivaMainServicePallet = "PALLET"
)

// omnivaRoundTripper injects the Basic Auth and X-Integration-Agent-Id headers
// on every outgoing request so call sites do not need to repeat them.
type omnivaRoundTripper struct {
	inner    http.RoundTripper
	username string
	password string
	agentID  string
}

// RoundTrip implements http.RoundTripper.
func (rt *omnivaRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.SetBasicAuth(rt.username, rt.password)
	if rt.agentID != "" {
		r.Header.Set("X-Integration-Agent-Id", rt.agentID)
	}
	r.Header.Set("Content-Type", "application/json")
	return rt.inner.RoundTrip(r)
}

// OmnivaAdapter implements CarrierAdapter and ManifestAdapter for the Omniva OMX API.
//
// Authentication uses HTTP Basic Auth (username + password supplied by Omniva account manager).
// The X-Integration-Agent-Id header identifies this integration platform and is injected
// via omnivaRoundTripper on every request.
//
// Event polling: call StartEventPoller to launch a background goroutine that consumes
// Omniva's cursor-based event stream and dispatches tracking updates via the notification service.
type OmnivaAdapter struct {
	// CustomerCode is the Omniva partner code required on every API request.
	CustomerCode string
	// BaseURL is the API root. Defaults to omnivaLiveBase.
	BaseURL string

	client   *http.Client
	log      *zap.Logger
	notifSvc *notification.Service

	// pollerMu guards lastEventID so the poller and any concurrent reads are safe.
	pollerMu    sync.Mutex
	lastEventID int64
}

// NewOmnivaAdapter returns a production OmnivaAdapter.
// agentID should be the value provided by Omniva for the X-Integration-Agent-Id header
// in the format "Developer_XXXXXX_YYYYYY". Leave empty if not yet assigned.
func NewOmnivaAdapter(username, password, customerCode, agentID string, log *zap.Logger) *OmnivaAdapter {
	rt := &omnivaRoundTripper{
		inner:    http.DefaultTransport,
		username: username,
		password: password,
		agentID:  agentID,
	}
	return &OmnivaAdapter{
		CustomerCode: customerCode,
		BaseURL:      omnivaLiveBase,
		client:       &http.Client{Timeout: 30 * time.Second, Transport: rt},
		log:          log,
	}
}

// WithNotificationService attaches a notification service to the adapter so the
// event poller can dispatch tracking webhooks. Call before StartEventPoller.
func (a *OmnivaAdapter) WithNotificationService(svc *notification.Service) *OmnivaAdapter {
	a.notifSvc = svc
	return a
}

// ── wire format types ─────────────────────────────────────────────────────────

type omnivaAddress struct {
	Street          string `json:"street,omitempty"`
	HouseNo         string `json:"houseNo,omitempty"`
	ApartmentNo     string `json:"apartmentNo,omitempty"`
	Deliverypoint   string `json:"deliverypoint,omitempty"`
	Postcode        string `json:"postcode,omitempty"`
	Country         string `json:"country"`
	OffloadPostcode string `json:"offloadPostcode,omitempty"`
}

type omnivaAddressee struct {
	PersonName                string        `json:"personName,omitempty"`
	CompanyName               string        `json:"companyName,omitempty"`
	AltName                   string        `json:"altName,omitempty"`
	ContactPhone              string        `json:"contactPhone,omitempty"`
	ContactMobile             string        `json:"contactMobile,omitempty"`
	ContactEmail              string        `json:"contactEmail,omitempty"`
	Address                   omnivaAddress `json:"address"`
	UseCustomerCode           bool          `json:"useCustomerCode,omitempty"`
	UseSenderAddressForReturn bool          `json:"useSenderAddressForReturn,omitempty"`
}

type omnivaMeasurement struct {
	Weight float64 `json:"weight,omitempty"`
	Length float64 `json:"length,omitempty"`
	Width  float64 `json:"width,omitempty"`
	Height float64 `json:"height,omitempty"`
}

type omnivaServicePackage struct {
	Code                 string `json:"code,omitempty"`
	AllowedStoringPeriod *int   `json:"allowedStoringPeriod,omitempty"`
}

type omnivaAddServiceParam struct {
	CODReceiver    string  `json:"COD_RECEIVER,omitempty"`
	CODAmount      float64 `json:"COD_AMOUNT,omitempty"`
	CODBankAccount string  `json:"COD_BANK_ACCOUNT_NO,omitempty"`
	CODReferenceNo string  `json:"COD_REFERENCE_NO,omitempty"`
	InsuranceValue float64 `json:"INSURANCE_VALUE,omitempty"`
	PersonalCode   string  `json:"DELIVERY_TO_SPECIFIC_PERSON_PERSONAL_CODE,omitempty"`
}

type omnivaAddService struct {
	Code   string                 `json:"code"`
	Params *omnivaAddServiceParam `json:"params,omitempty"`
}

type omnivaCustomsItem struct {
	Description    string  `json:"description"`
	NumberOfPieces int     `json:"numberOfPieces"`
	Weight         float64 `json:"weight"`
	FinancialValue float64 `json:"financialValue"`
	TariffNumber   string  `json:"tariffNumber,omitempty"`
	OriginCountry  string  `json:"originCountry,omitempty"`
}

type omnivaCustoms struct {
	GoodsCategoryCode      string              `json:"goodsCategoryCode,omitempty"`
	CategoryExplanation    string              `json:"categoryExplanation,omitempty"`
	LicenceNumber          string              `json:"licenceNumber,omitempty"`
	CertificateNumber      string              `json:"certificateNumber,omitempty"`
	InvoiceNumber          string              `json:"invoiceNumber,omitempty"`
	SenderCustomsReference string              `json:"senderCustomsReference,omitempty"`
	ImportersReference     string              `json:"importersReference,omitempty"`
	ShipmentItems          []omnivaCustomsItem `json:"shipmentItems,omitempty"`
}

type omnivaShipment struct {
	Barcode            string                `json:"barcode,omitempty"`
	PartnerShipmentID  string                `json:"partnerShipmentId,omitempty"`
	MainService        string                `json:"mainService"`
	DeliveryChannel    string                `json:"deliveryChannel,omitempty"`
	ContentDescription string                `json:"contentDescription,omitempty"`
	ShipmentComment    string                `json:"shipmentComment,omitempty"`
	ReturnAllowed      *bool                 `json:"returnAllowed,omitempty"`
	CustomerReturn     bool                  `json:"customerReturn,omitempty"`
	PaidByReceiver     bool                  `json:"paidByReceiver,omitempty"`
	ServicePackage     *omnivaServicePackage `json:"servicePackage,omitempty"`
	AddServices        []omnivaAddService    `json:"addServices,omitempty"`
	Measurement        *omnivaMeasurement    `json:"measurement,omitempty"`
	SenderAddressee    omnivaAddressee       `json:"senderAddressee"`
	ReceiverAddressee  omnivaAddressee       `json:"receiverAddressee"`
	Customs            *omnivaCustoms        `json:"customs,omitempty"`
}

type omnivaShipmentRequest struct {
	CustomerCode string           `json:"customerCode"`
	Shipments    []omnivaShipment `json:"shipments"`
}

type omnivaSavedShipment struct {
	Barcode      string `json:"barcode"`
	ClientItemID string `json:"clientItemId"`
}

type omnivaFailedShipment struct {
	Barcode      string `json:"barcode"`
	ClientItemID string `json:"clientItemId"`
	MessageCode  string `json:"messageCode"`
	Message      string `json:"message"`
}

type omnivaShipmentResponse struct {
	ResultCode      string                 `json:"resultCode"`
	SavedShipments  []omnivaSavedShipment  `json:"savedShipments"`
	FailedShipments []omnivaFailedShipment `json:"failedShipments"`
}

type omnivaLabelRequest struct {
	CustomerCode      string   `json:"customerCode"`
	Barcodes          []string `json:"barcodes"`
	SendAddressCardTo string   `json:"sendAddressCardTo"`
}

type omnivaLabelEntry struct {
	Barcode     string `json:"barcode"`
	Filedata    string `json:"filedata"`
	MessageCode string `json:"messageCode"`
}

type omnivaLabelResponse struct {
	SuccessAddressCards []omnivaLabelEntry `json:"successAddressCards"`
	FailedAddressCards  []omnivaLabelEntry `json:"failedAddressCards"`
}

type omnivaCancelRequest struct {
	CustomerCode string   `json:"customerCode"`
	Barcodes     []string `json:"barcodes"`
}

type omnivaCancelResponse struct {
	ResultCode  string `json:"resultCode"`
	Barcode     string `json:"barcode"`
	MessageCode string `json:"messageCode"`
	Message     string `json:"message"`
}

type omnivaUpdateRequest struct {
	CustomerCode      string          `json:"customerCode"`
	Barcode           string          `json:"barcode"`
	NeedsRelabel      bool            `json:"needsRelabel"`
	DeliveryChannel   string          `json:"deliveryChannel,omitempty"`
	ReceiverAddressee omnivaAddressee `json:"receiverAddressee"`
}

type omnivaUpdateResponse struct {
	ResultCode  string `json:"resultCode"`
	Barcode     string `json:"barcode"`
	MessageCode string `json:"messageCode"`
	Message     string `json:"message"`
}

type omnivaTrackingEvent struct {
	EventID   int64  `json:"eventId"`
	Barcode   string `json:"barcode"`
	EventDate string `json:"eventDate"`
	EventCode string `json:"eventCode"`
	Comment   string `json:"comment"`
	Location  string `json:"location"`
}

type omnivaTrackingSlice struct {
	Content []omnivaTrackingEvent `json:"content"`
	Last    bool                  `json:"last"`
}

type omnivaReturnShipment struct {
	Barcode           string `json:"barcode"`
	PartnerShipmentID string `json:"partnerShipmentId,omitempty"`
	PaidByReceiver    bool   `json:"paidByReceiver,omitempty"`
}

type omnivaReturnRequest struct {
	CustomerCode    string                 `json:"customerCode"`
	ReturnShipments []omnivaReturnShipment `json:"returnShipments"`
}

type omnivaReturnSaved struct {
	Barcode                 string `json:"barcode"`
	ClientItemID            string `json:"clientItemId"`
	OriginalShipmentBarcode string `json:"originalShipmentBarcode"`
}

type omnivaReturnFailed struct {
	Barcode                 string `json:"barcode"`
	ClientItemID            string `json:"clientItemId"`
	OriginalShipmentBarcode string `json:"originalShipmentBarcode"`
	MessageCode             string `json:"messageCode"`
	Message                 string `json:"message"`
}

type omnivaReturnResponse struct {
	ResultCode      string               `json:"resultCode"`
	SavedShipments  []omnivaReturnSaved  `json:"savedShipments"`
	FailedShipments []omnivaReturnFailed `json:"failedShipments"`
}

// ReturnRequest is the input to BookReturn.
type OmnivaReturnRequest struct {
	// OriginalBarcode is the Omniva barcode of the already-delivered shipment to return.
	OriginalBarcode string `json:"originalBarcode"`
	// PartnerShipmentID is the caller's own reference (optional). Echoed in the response.
	PartnerShipmentID string `json:"partnerShipmentId,omitempty"`
	// PaidByReceiver bills the return freight to the receiver rather than the sender.
	// For international returns this must be true — only the Omniva business customer
	// (the receiver) can be billed.
	PaidByReceiver bool `json:"paidByReceiver,omitempty"`
}

// OmnivaReturnResult is returned by BookReturn.
type OmnivaReturnResult struct {
	// ReturnBarcode is the barcode assigned to the return shipment.
	ReturnBarcode string `json:"returnBarcode"`
	// OriginalBarcode is the barcode of the original shipment.
	OriginalBarcode string `json:"originalBarcode"`
	// PartnerShipmentID is the caller's reference if supplied in the request.
	PartnerShipmentID string `json:"partnerShipmentId,omitempty"`
}

type omnivaPickupAvailabilityRequest struct {
	CustomerCode  string              `json:"customerCode"`
	PickupAddress omnivaPickupAddress `json:"pickupAddress"`
}

type omnivaPickupAddress struct {
	Street        string `json:"street,omitempty"`
	House         string `json:"house,omitempty"`
	ApartmentNo   string `json:"apartmentNo,omitempty"`
	Deliverypoint string `json:"deliverypoint,omitempty"`
	Postcode      string `json:"postcode,omitempty"`
	Country       string `json:"country,omitempty"`
}

type omnivaAvailableTimeslot struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

type omnivaPickupAvailabilityResponse struct {
	AvailableTimeslots []omnivaAvailableTimeslot `json:"availableTimeslots"`
}

type omnivaPickupRequest struct {
	CustomerCode      string              `json:"customerCode"`
	ContactPersonName string              `json:"contactPersonName"`
	ContactPhone      string              `json:"contactPhone"`
	PickupAddress     omnivaPickupAddress `json:"pickupAddress"`
	PickupComment     string              `json:"pickupComment,omitempty"`
	StartTime         string              `json:"startTime"`
	EndTime           string              `json:"endTime"`
	IsHeavyPackage    bool                `json:"isHeavyPackage,omitempty"`
	PackageCount      int                 `json:"packageCount,omitempty"`
	PalletCount       int                 `json:"palletCount,omitempty"`
}

type omnivaPickupResponse struct {
	CourierOrderNumber string `json:"courierOrderNumber"`
	StartTime          string `json:"startTime"`
	EndTime            string `json:"endTime"`
}

type omnivaCancelPickupRequest struct {
	CustomerCode       string `json:"customerCode"`
	CourierOrderNumber string `json:"courierOrderNumber"`
}

type omnivaCancelPickupResponse struct {
	CourierOrderNumber string `json:"courierOrderNumber"`
	ResultCode         string `json:"resultCode"`
}

// ── helpers ───────────────────────────────────────────────────────────────────

// do executes an HTTP request and decodes the JSON response into dst.
// A non-2xx status is mapped to an error containing the raw body.
func (a *OmnivaAdapter) do(ctx context.Context, method, path string, body any, dst any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("omniva: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("omniva: build request: %w", err)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("omniva: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("omniva: read response body: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("omniva: rate limit exceeded (HTTP 429): %s", raw)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("omniva: HTTP %d: %s", resp.StatusCode, raw)
	}

	if dst != nil {
		if err := json.Unmarshal(raw, dst); err != nil {
			return fmt.Errorf("omniva: decode response: %w", err)
		}
	}
	return nil
}

// omnivaDeliveryChannel derives the Omniva deliveryChannel from the gateway shipment.
// COURIER is the default; PARCEL_MACHINE is used when ServicePointID is set on the receiver.
func omnivaDeliveryChannel(s Shipment) string {
	if s.Receiver.ServicePointID != "" {
		return "PARCEL_MACHINE"
	}
	switch s.DeliveryType {
	case "servicepoint":
		return "POST_OFFICE"
	default:
		return "COURIER"
	}
}

// omnivaServicePackageCode maps the gateway ServiceTier to an Omniva service package code.
// Empty ServiceTier defaults to ECONOMY for parcels.
func omnivaServicePackageCode(tier string) string {
	switch tier {
	case "standard":
		return "STANDARD"
	case "premium":
		return "PREMIUM"
	case "procedural_document":
		return "PROCEDURAL_DOCUMENT"
	case "registered_letter":
		return "REGISTERED_LETTER"
	case "registered_maxiletter":
		return "REGISTERED_MAXILETTER"
	default:
		return "ECONOMY"
	}
}

// omnivaAddresses converts a gateway Address to an Omniva addressee block.
func omnivaAddresseeFrom(addr Address) omnivaAddressee {
	a := omnivaAddressee{
		AltName:                   addr.AltName,
		ContactPhone:              addr.Phone,
		ContactMobile:             addr.Phone, // same value; Omniva prefers mobile for notifications
		ContactEmail:              addr.Email,
		UseSenderAddressForReturn: addr.UseAddressForReturn,
		Address: omnivaAddress{
			Street:          addr.Street,
			HouseNo:         addr.HouseNumber,
			ApartmentNo:     addr.Supplement,
			Deliverypoint:   addr.City,
			Postcode:        addr.PostalCode,
			Country:         addr.Country,
			OffloadPostcode: addr.ServicePointID,
		},
	}
	// Omniva requires either personName or companyName, not both.
	a.PersonName = addr.Name
	return a
}

// omnivaAddServices converts gateway AddOns to Omniva addServices entries.
func omnivaAddServices(addOns []AddOn) []omnivaAddService {
	var services []omnivaAddService
	for _, ao := range addOns {
		var code string
		var params *omnivaAddServiceParam
		switch ao.Type {
		case AddOnCashOnDelivery:
			code = "COD"
			params = &omnivaAddServiceParam{
				CODReceiver:    ao.CODReceiver,
				CODAmount:      ao.CODAmount,
				CODBankAccount: ao.CODAccountNumber,
				CODReferenceNo: ao.CODReferenceNo,
			}
		case AddOnInsurance:
			code = "INSURANCE"
			params = &omnivaAddServiceParam{InsuranceValue: ao.InsuranceValue}
		case AddOnSignatureRequired:
			code = "SIGNATURE"
		case AddOnDeliveryToSpecificPerson:
			code = "DELIVERY_TO_A_SPECIFIC_PERSON"
			if ao.PersonalCode != "" {
				params = &omnivaAddServiceParam{PersonalCode: ao.PersonalCode}
			}
		case AddOnDeliveryToPrivatePerson:
			code = "DELIVERY_TO_PRIVATE_PERSON"
		case AddOnFragile:
			code = "FRAGILE"
		case AddOnDocumentReturn:
			code = "DOCUMENT_RETURN"
		case AddOnMultiParcelTogether:
			code = "MULTIPLE_PARCELS_DELIVERY_TOGETHER"
		default:
			// Unsupported add-on for Omniva — skip silently.
			continue
		}
		svc := omnivaAddService{Code: code}
		if params != nil {
			svc.Params = params
		}
		services = append(services, svc)
	}
	return services
}

// omnivaCustomsFrom converts the gateway Customs block to Omniva's customs format.
// Returns nil when there are no customs items to declare.
func omnivaCustomsFrom(c Customs) *omnivaCustoms {
	if len(c.Items) == 0 && c.GoodsCategoryCode == "" {
		return nil
	}

	gc := c.GoodsCategoryCode
	if gc == "" && c.NatureOfCargo != "" {
		// Map the shared NatureOfCargo to Omniva's goodsCategoryCode where possible.
		switch c.NatureOfCargo {
		case "SALE_OF_GOODS":
			gc = "SALE_OF_GOODS"
		case "GIFT":
			gc = "GIFT"
		case "RETURNED_GOODS":
			gc = "RETURNED_GOODS"
		case "COMMERCIAL_SAMPLE":
			gc = "COMMERCIAL_SAMPLE"
		case "DOCUMENTS":
			gc = "DOCUMENTS"
		default:
			gc = "OTHER"
		}
	}

	oc := &omnivaCustoms{
		GoodsCategoryCode:      gc,
		CategoryExplanation:    c.CategoryExplanation,
		LicenceNumber:          c.LicenceNumber,
		CertificateNumber:      c.CertificateNumber,
		InvoiceNumber:          c.InvoiceNumber,
		SenderCustomsReference: c.SenderCustomsReference,
		ImportersReference:     c.ImportersReference,
	}
	for _, item := range c.Items {
		oc.ShipmentItems = append(oc.ShipmentItems, omnivaCustomsItem{
			Description:    item.Description,
			NumberOfPieces: item.Quantity,
			Weight:         item.NetWeight,
			FinancialValue: item.Value,
			TariffNumber:   item.HSCode,
			OriginCountry:  item.CountryOfOrigin,
		})
	}
	return oc
}

// omnivaShipmentFrom builds the Omniva shipment payload from a gateway BookingRequest.
func omnivaShipmentFrom(r BookingRequest) omnivaShipment {
	s := r.Shipment

	mainService := omnivaMainServiceParcel
	// Map gateway DeliveryType to Omniva main service where unambiguous.
	// Callers can override via Shipment.ServiceTier using letter-specific tier codes.
	switch s.ServiceTier {
	case "procedural_document", "registered_letter", "registered_maxiletter":
		mainService = omnivaMainServiceLetter
	}

	channel := omnivaDeliveryChannel(s)

	pkg := &omnivaServicePackage{Code: omnivaServicePackageCode(s.ServiceTier)}

	// Dimensions are in cm in the gateway; Omniva expects metres.
	var measurement *omnivaMeasurement
	if len(s.Colli) > 0 {
		c := s.Colli[0]
		m := &omnivaMeasurement{Weight: s.TotalWeight}
		d := c.Dimensions
		if d.Length > 0 {
			m.Length = d.Length / 100
		}
		if d.Width > 0 {
			m.Width = d.Width / 100
		}
		if d.Height > 0 {
			m.Height = d.Height / 100
		}
		measurement = m
	}

	// Build a content description from the first colli item for non-Baltic destinations.
	var contentDesc string
	if len(s.Colli) > 0 && len(s.Colli[0].Items) > 0 {
		contentDesc = s.Colli[0].Items[0].Description
	}

	sender := omnivaAddresseeFrom(s.Sender)
	receiver := omnivaAddresseeFrom(s.Receiver)

	return omnivaShipment{
		PartnerShipmentID:  s.Colli[0].ID,
		MainService:        mainService,
		DeliveryChannel:    channel,
		ContentDescription: contentDesc,
		ShipmentComment:    s.ShipmentComment,
		PaidByReceiver:     s.PaidByReceiver,
		ServicePackage:     pkg,
		AddServices:        omnivaAddServices(s.AddOns),
		Measurement:        measurement,
		SenderAddressee:    sender,
		ReceiverAddressee:  receiver,
		Customs:            omnivaCustomsFrom(s.Customs),
	}
}

// ── CarrierAdapter ────────────────────────────────────────────────────────────

// BookShipment registers a B2C shipment with Omniva.
// When Shipment.PaidByReceiver is true the request is routed to the C2C endpoint.
func (a *OmnivaAdapter) BookShipment(ctx context.Context, r BookingRequest) (*BookingResponse, error) {
	shipment := omnivaShipmentFrom(r)
	payload := omnivaShipmentRequest{
		CustomerCode: a.CustomerCode,
		Shipments:    []omnivaShipment{shipment},
	}

	path := "/api/v01/omx/shipments/business-to-client"
	if r.Shipment.PaidByReceiver {
		path = "/api/v01/omx/shipments/client-to-client"
	}

	var result omnivaShipmentResponse
	if err := a.do(ctx, http.MethodPost, path, payload, &result); err != nil {
		return nil, fmt.Errorf("omniva: book shipment: %w", err)
	}

	if result.ResultCode != "OK" || len(result.SavedShipments) == 0 {
		if len(result.FailedShipments) > 0 {
			f := result.FailedShipments[0]
			return nil, fmt.Errorf("omniva: book shipment failed: %s — %s", f.MessageCode, f.Message)
		}
		return nil, fmt.Errorf("omniva: book shipment failed: resultCode=%s", result.ResultCode)
	}

	barcode := result.SavedShipments[0].Barcode
	a.log.Info("omniva: shipment booked", zap.String("barcode", barcode))

	return &BookingResponse{
		TrackingNumber: barcode,
		Carrier:        "omniva",
		Status:         "booked",
		Colli: []ColliResponse{
			{
				ID:             r.Shipment.Colli[0].ID,
				TrackingNumber: barcode,
			},
		},
	}, nil
}

// TrackShipment retrieves all tracking events for the given barcode.
func (a *OmnivaAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	var result omnivaTrackingSlice
	path := "/api/v01/omx/shipments/" + trackingNumber
	if err := a.do(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, fmt.Errorf("omniva: track shipment: %w", err)
	}

	events := make([]TrackingEvent, 0, len(result.Content))
	for _, e := range result.Content {
		ns := normalizeOmnivaStatus(e.EventCode)
		events = append(events, TrackingEvent{
			Timestamp:        e.EventDate,
			Status:           e.EventCode,
			NormalizedStatus: ns,
			Location:         e.Location,
			Details:          e.Comment,
		})
	}

	current := StatusUnknown
	if len(events) > 0 {
		current = events[0].NormalizedStatus
	}

	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "omniva",
		Status:           string(current),
		NormalizedStatus: current,
		OriginalStatus:   string(current),
		Events:           events,
	}, nil
}

// FetchLabel retrieves the shipping label as base64-encoded PDF.
// Omniva only supports PDF; any other format returns an error.
func (a *OmnivaAdapter) FetchLabel(ctx context.Context, req LabelRequest) (*LabelResponse, error) {
	if req.Format != "" && req.Format != LabelFormatPDF {
		return nil, fmt.Errorf("omniva: only PDF labels are supported, got %s", req.Format)
	}

	payload := omnivaLabelRequest{
		CustomerCode:      a.CustomerCode,
		Barcodes:          []string{req.TrackingNumber},
		SendAddressCardTo: "RESPONSE",
	}

	var result omnivaLabelResponse
	if err := a.do(ctx, http.MethodPost, "/api/v01/omx/shipments/package-labels", payload, &result); err != nil {
		return nil, fmt.Errorf("omniva: fetch label: %w", err)
	}

	if len(result.SuccessAddressCards) == 0 {
		if len(result.FailedAddressCards) > 0 {
			f := result.FailedAddressCards[0]
			return nil, fmt.Errorf("omniva: label request failed: %s", f.MessageCode)
		}
		return nil, fmt.Errorf("omniva: label request returned no data")
	}

	entry := result.SuccessAddressCards[0]
	// Omniva returns the label as base64 in the filedata field.
	// Validate it is valid base64 before forwarding.
	if _, err := base64.StdEncoding.DecodeString(entry.Filedata); err != nil {
		return nil, fmt.Errorf("omniva: label data is not valid base64: %w", err)
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "omniva",
		Format:         LabelFormatPDF,
		Data:           entry.Filedata,
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// CancelShipment cancels a registered shipment that has not yet been handed to Omniva.
func (a *OmnivaAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	payload := omnivaCancelRequest{
		CustomerCode: a.CustomerCode,
		Barcodes:     []string{trackingNumber},
	}

	var result omnivaCancelResponse
	if err := a.do(ctx, http.MethodPost, "/api/v01/omx/shipments/cancel", payload, &result); err != nil {
		return nil, fmt.Errorf("omniva: cancel shipment: %w", err)
	}

	if result.ResultCode != "OK" {
		return nil, fmt.Errorf("omniva: cancel failed: %s — %s", result.MessageCode, result.Message)
	}

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "omniva",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment applies partial updates to a registered shipment.
// Only receiver contact fields and delivery channel are forwarded; Omniva does not
// support weight updates or country changes after booking.
// The shipment must still be in REGISTERED status (no scans yet).
func (a *OmnivaAdapter) UpdateShipment(ctx context.Context, req UpdateRequest) (*UpdateResponse, error) {
	// Fetch the current receiver details so we can send the full block as Omniva requires.
	var tracking omnivaTrackingSlice
	if err := a.do(ctx, http.MethodGet, "/api/v01/omx/shipments/"+req.TrackingNumber, nil, &tracking); err != nil {
		return nil, fmt.Errorf("omniva: update shipment — could not fetch current state: %w", err)
	}

	receiver := omnivaAddressee{}
	if req.ReceiverPhone != "" {
		receiver.ContactMobile = req.ReceiverPhone
		receiver.ContactPhone = req.ReceiverPhone
	}
	if req.ReceiverEmail != "" {
		receiver.ContactEmail = req.ReceiverEmail
	}

	if req.Weight != 0 {
		a.log.Warn("omniva: weight update requested but not supported by Omniva API — ignoring",
			zap.String("trackingNumber", req.TrackingNumber),
			zap.Float64("requestedWeight", req.Weight),
		)
	}

	channel := ""
	if req.ServicePointID != "" {
		channel = "PARCEL_MACHINE"
		receiver.Address.OffloadPostcode = req.ServicePointID
	}

	payload := omnivaUpdateRequest{
		CustomerCode:      a.CustomerCode,
		Barcode:           req.TrackingNumber,
		NeedsRelabel:      false,
		DeliveryChannel:   channel,
		ReceiverAddressee: receiver,
	}

	var result omnivaUpdateResponse
	if err := a.do(ctx, http.MethodPost, "/api/v01/omx/shipments", payload, &result); err != nil {
		return nil, fmt.Errorf("omniva: update shipment: %w", err)
	}

	if result.ResultCode != "OK" {
		return nil, fmt.Errorf("omniva: update failed: %s — %s", result.MessageCode, result.Message)
	}

	updated := []string{}
	if req.ReceiverPhone != "" {
		updated = append(updated, "phone")
	}
	if req.ReceiverEmail != "" {
		updated = append(updated, "email")
	}
	if req.ServicePointID != "" {
		updated = append(updated, "servicePointId")
	}

	return &UpdateResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "omniva",
		Status:         "updated",
		UpdatedFields:  updated,
	}, nil
}

// ── BookReturn ────────────────────────────────────────────────────────────────

// BookReturn registers a return shipment against an already-delivered Omniva shipment.
//
// Constraints (enforced by Omniva):
//   - Original shipment must have been registered under the same customerCode.
//   - For Baltic destinations the original must be in DELIVERED status.
//   - LETTER shipments cannot be returned.
//   - The return is routed to the original pickup location automatically.
func (a *OmnivaAdapter) BookReturn(ctx context.Context, req OmnivaReturnRequest) (*OmnivaReturnResult, error) {
	payload := omnivaReturnRequest{
		CustomerCode: a.CustomerCode,
		ReturnShipments: []omnivaReturnShipment{
			{
				Barcode:           req.OriginalBarcode,
				PartnerShipmentID: req.PartnerShipmentID,
				PaidByReceiver:    req.PaidByReceiver,
			},
		},
	}

	var result omnivaReturnResponse
	if err := a.do(ctx, http.MethodPost, "/api/v01/omx/shipments/omniva-return", payload, &result); err != nil {
		return nil, fmt.Errorf("omniva: book return: %w", err)
	}

	if result.ResultCode != "OK" || len(result.SavedShipments) == 0 {
		if len(result.FailedShipments) > 0 {
			f := result.FailedShipments[0]
			return nil, fmt.Errorf("omniva: return failed: %s — %s", f.MessageCode, f.Message)
		}
		return nil, fmt.Errorf("omniva: return failed: resultCode=%s", result.ResultCode)
	}

	saved := result.SavedShipments[0]
	return &OmnivaReturnResult{
		ReturnBarcode:     saved.Barcode,
		OriginalBarcode:   saved.OriginalShipmentBarcode,
		PartnerShipmentID: saved.ClientItemID,
	}, nil
}

// ── ManifestAdapter ───────────────────────────────────────────────────────────

// GetPickupAvailability returns available collection timeslots for the given address.
// Call this before BookPickup to select a valid window and avoid availability-zone errors.
func (a *OmnivaAdapter) GetPickupAvailability(ctx context.Context, req PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	payload := omnivaPickupAvailabilityRequest{
		CustomerCode: a.CustomerCode,
		PickupAddress: omnivaPickupAddress{
			Street:        req.Address.Street,
			House:         req.Address.HouseNumber,
			Deliverypoint: req.Address.City,
			Postcode:      req.Address.PostalCode,
			Country:       req.Address.Country,
		},
	}

	var raw omnivaPickupAvailabilityResponse
	if err := a.do(ctx, http.MethodPost, "/api/v01/omx/courierorders/pickup-availability", payload, &raw); err != nil {
		return nil, fmt.Errorf("omniva: get pickup availability: %w", err)
	}

	slots := make([]PickupSlot, 0, len(raw.AvailableTimeslots))
	for _, ts := range raw.AvailableTimeslots {
		// Omniva returns full UTC datetime strings e.g. "2023-01-31T12:00:00.000".
		// Extract the date from the start time for the PickupSlot.Date field.
		date := ""
		if len(ts.StartTime) >= 10 {
			date = ts.StartTime[:10]
		}
		slots = append(slots, PickupSlot{
			Date:      date,
			StartTime: ts.StartTime,
			EndTime:   ts.EndTime,
		})
	}

	return &PickupAvailabilityResponse{
		Carrier: "omniva",
		Slots:   slots,
	}, nil
}

// BookPickup creates a courier pickup order for the given address and time window.
// Call GetPickupAvailability first to obtain a valid StartTime/EndTime pair.
func (a *OmnivaAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	if req.Pickup.Date == "" {
		return nil, fmt.Errorf("omniva: pickup date is required")
	}

	startTime := req.Pickup.Date + "T" + req.Pickup.ReadyTime + ":00.000"
	endTime := req.Pickup.Date + "T" + req.Pickup.CloseTime + ":00.000"

	payload := omnivaPickupRequest{
		CustomerCode:      a.CustomerCode,
		ContactPersonName: req.Contact.Name,
		ContactPhone:      req.Contact.Phone,
		PickupComment:     req.Pickup.SpecialInstructions,
		StartTime:         startTime,
		EndTime:           endTime,
		PackageCount:      req.EstimatedParcels,
		PickupAddress: omnivaPickupAddress{
			Street:        req.Address.Street,
			House:         req.Address.HouseNumber,
			Deliverypoint: req.Address.City,
			Postcode:      req.Address.PostalCode,
			Country:       req.Address.Country,
		},
	}

	var result omnivaPickupResponse
	if err := a.do(ctx, http.MethodPost, "/api/v01/omx/courierorders/create-pickup-order", payload, &result); err != nil {
		return nil, fmt.Errorf("omniva: book pickup: %w", err)
	}

	return &PickupResponse{
		Carrier:            "omniva",
		ConfirmationNumber: result.CourierOrderNumber,
		Date:               req.Pickup.Date,
		ReadyTime:          result.StartTime,
		CloseTime:          result.EndTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported by Omniva — cancel and rebook instead.
func (a *OmnivaAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("Omniva", "pickup update", "cancel the existing order and create a new one")
}

// CancelPickup cancels a previously created courier pickup order.
func (a *OmnivaAdapter) CancelPickup(ctx context.Context, _, confirmationNumber string) error {
	payload := omnivaCancelPickupRequest{
		CustomerCode:       a.CustomerCode,
		CourierOrderNumber: confirmationNumber,
	}

	var result omnivaCancelPickupResponse
	if err := a.do(ctx, http.MethodPost, "/api/v01/omx/courierorders/cancel-pickup-order", payload, &result); err != nil {
		return fmt.Errorf("omniva: cancel pickup: %w", err)
	}

	if result.ResultCode != "OK" {
		return fmt.Errorf("omniva: cancel pickup failed: resultCode=%s", result.ResultCode)
	}
	return nil
}

// CloseManifest is not supported by Omniva — no end-of-day manifest close endpoint exists.
func (a *OmnivaAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("Omniva", "manifest close", "Omniva has no end-of-day manifest endpoint")
}

// ── Event poller ──────────────────────────────────────────────────────────────

// StartEventPoller launches a background goroutine that polls Omniva's eventID-based
// tracking event stream and dispatches normalized status updates via the notification service.
//
// The poller respects Omniva's rate limit (5 requests per 5 minutes) and uses an
// exponential backoff on transient errors. It exits when ctx is cancelled.
//
// prefs controls which webhook receives the dispatched events. webhookURL must be non-empty.
// If notifSvc was not set via WithNotificationService the poller returns immediately.
func (a *OmnivaAdapter) StartEventPoller(ctx context.Context, prefs notification.Preferences) {
	if a.notifSvc == nil {
		a.log.Warn("omniva: event poller started without a notification service — no events will be dispatched")
		return
	}
	go a.runEventPoller(ctx, prefs)
}

// runEventPoller is the background loop. It polls at the frequency recommended by
// Omniva for the account's shipment volume; the interval is read from the environment
// variable OMNIVA_POLL_INTERVAL_SECONDS and defaults to 60s (suitable for up to 100
// shipments/day). For volumes above 300/day, set to 10s and also set size=100 (already set).
func (a *OmnivaAdapter) runEventPoller(ctx context.Context, prefs notification.Preferences) {
	interval := 60 * time.Second

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	a.log.Info("omniva: event poller started", zap.Duration("interval", interval))

	for {
		select {
		case <-ctx.Done():
			a.log.Info("omniva: event poller stopped")
			return
		case <-ticker.C:
			a.pollOnce(ctx, prefs)
		}
	}
}

// pollOnce executes a single poll cycle, fetching up to omnivaEventPollSize events
// starting after lastEventID and dispatching each one as a webhook notification.
// It drains all available pages before returning.
func (a *OmnivaAdapter) pollOnce(ctx context.Context, prefs notification.Preferences) {
	a.pollerMu.Lock()
	fromID := a.lastEventID
	a.pollerMu.Unlock()

	path := fmt.Sprintf("/api/v01/omx/shipments?size=%d&fromTrackEventId=%d",
		omnivaEventPollSize, fromID)

	var slice omnivaTrackingSlice
	if err := a.do(ctx, http.MethodGet, path, nil, &slice); err != nil {
		a.log.Error("omniva: event poll failed", zap.Error(err))
		return
	}

	if len(slice.Content) == 0 {
		return
	}

	var maxEventID int64
	for _, e := range slice.Content {
		if e.EventID > maxEventID {
			maxEventID = e.EventID
		}
		ns := normalizeOmnivaStatus(e.EventCode)
		payload := notification.Payload{
			TrackingNumber: e.Barcode,
			Carrier:        "omniva",
			Status:         string(ns),
			Timestamp:      time.Now().UTC(),
		}
		event := notificationEventForStatus(ns)
		a.notifSvc.Dispatch(ctx, event, prefs, payload)
	}

	a.pollerMu.Lock()
	if maxEventID > a.lastEventID {
		a.lastEventID = maxEventID
	}
	a.pollerMu.Unlock()

	a.log.Info("omniva: poll cycle complete",
		zap.Int("events", len(slice.Content)),
		zap.Int64("lastEventID", maxEventID),
		zap.Bool("more", !slice.Last),
	)
}

// ── status mapping ────────────────────────────────────────────────────────────

// omnivaStatuses maps known Omniva event codes to normalised TrackingStatus values.
// Omniva does not publish its full event code list; this table is extended as codes
// are observed in production. Unknown codes fall back to StatusInTransit.
var omnivaStatuses = map[string]TrackingStatus{
	// Registration and pre-transit.
	"REGISTERED": StatusBooked,
	"ACCEPTED":   StatusPickedUp,
	"COLLECTED":  StatusPickedUp,
	// In-transit events.
	"IN_TRANSIT": StatusInTransit,
	"IN_SORTING": StatusInTransit,
	"SORTED":     StatusInTransit,
	"DISPATCHED": StatusInTransit,
	// Delivery events.
	"OUT_FOR_DELIVERY":     StatusOutForDelivery,
	"DELIVERED":            StatusDelivered,
	"DELIVERED_TO_MAILBOX": StatusDelivered,
	// Failure and return events.
	"DELIVERY_FAILED":  StatusFailed,
	"NOT_HOME":         StatusFailed,
	"REFUSED":          StatusFailed,
	"RETURNED":         StatusReturned,
	"RETURN_TO_SENDER": StatusReturned,
	// Held at post office or parcel machine.
	"READY_FOR_PICKUP": StatusInTransit,
	"STORED":           StatusInTransit,
}

// normalizeOmnivaStatus maps a raw Omniva event code to a gateway TrackingStatus.
func normalizeOmnivaStatus(code string) TrackingStatus {
	if s, ok := omnivaStatuses[code]; ok {
		return s
	}
	return StatusInTransit
}

// notificationEventForStatus maps a TrackingStatus to the corresponding notification Event.
func notificationEventForStatus(s TrackingStatus) notification.Event {
	switch s {
	case StatusBooked:
		return notification.EventBooked
	case StatusPickedUp:
		return notification.EventPickedUp
	case StatusInTransit:
		return notification.EventInTransit
	case StatusOutForDelivery:
		return notification.EventOutForDelivery
	case StatusDelivered:
		return notification.EventDelivered
	case StatusFailed:
		return notification.EventFailed
	case StatusReturned:
		return notification.EventReturned
	default:
		return notification.EventInTransit
	}
}

// ── mock ──────────────────────────────────────────────────────────────────────

// MockOmnivaAdapter satisfies CarrierAdapter and ManifestAdapter with canned responses.
// Used when OMNIVA_USERNAME is not set or MOCK_MODE=true.
type MockOmnivaAdapter struct{}

// BookShipment returns a canned booking response.
func (m *MockOmnivaAdapter) BookShipment(_ context.Context, r BookingRequest) (*BookingResponse, error) {
	colliID := ""
	if len(r.Shipment.Colli) > 0 {
		colliID = r.Shipment.Colli[0].ID
	}
	barcode := "MOCK-OMNIVA-" + colliID
	return &BookingResponse{
		TrackingNumber: barcode,
		Carrier:        "omniva",
		Status:         "booked",
		Colli:          []ColliResponse{{ID: colliID, TrackingNumber: barcode}},
	}, nil
}

// TrackShipment returns a canned tracking response.
func (m *MockOmnivaAdapter) TrackShipment(_ context.Context, trackingNumber string) (*TrackingResponse, error) {
	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "omniva",
		Status:           string(StatusBooked),
		NormalizedStatus: StatusBooked,
		OriginalStatus:   "REGISTERED",
	}, nil
}

// FetchLabel returns a minimal valid base64 PDF stub.
func (m *MockOmnivaAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "omniva",
		Format:         LabelFormatPDF,
		Data:           base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 mock")),
		MimeType:       MimeTypeForFormat(LabelFormatPDF),
	}, nil
}

// CancelShipment returns a canned cancellation response.
func (m *MockOmnivaAdapter) CancelShipment(_ context.Context, trackingNumber string) (*CancelResponse, error) {
	return &CancelResponse{TrackingNumber: trackingNumber, Carrier: "omniva", Status: "cancelled"}, nil
}

// UpdateShipment returns a canned update response.
func (m *MockOmnivaAdapter) UpdateShipment(_ context.Context, req UpdateRequest) (*UpdateResponse, error) {
	return &UpdateResponse{TrackingNumber: req.TrackingNumber, Carrier: "omniva", Status: "updated"}, nil
}

// BookReturn returns a canned return result.
func (m *MockOmnivaAdapter) BookReturn(_ context.Context, req OmnivaReturnRequest) (*OmnivaReturnResult, error) {
	return &OmnivaReturnResult{
		ReturnBarcode:     "MOCK-RETURN-" + req.OriginalBarcode,
		OriginalBarcode:   req.OriginalBarcode,
		PartnerShipmentID: req.PartnerShipmentID,
	}, nil
}

// GetPickupAvailability returns a canned availability response.
func (m *MockOmnivaAdapter) GetPickupAvailability(_ context.Context, req PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	return &PickupAvailabilityResponse{
		Carrier: req.Carrier,
		Slots: []PickupSlot{
			{Date: tomorrow, StartTime: tomorrow + "T08:00:00.000", EndTime: tomorrow + "T12:00:00.000"},
			{Date: tomorrow, StartTime: tomorrow + "T12:00:00.000", EndTime: tomorrow + "T17:00:00.000"},
		},
	}, nil
}

// BookPickup returns a canned pickup response.
func (m *MockOmnivaAdapter) BookPickup(_ context.Context, req PickupRequest) (*PickupResponse, error) {
	return &PickupResponse{
		Carrier:            "omniva",
		ConfirmationNumber: "MOCK-PICKUP-" + strconv.FormatInt(time.Now().UnixMilli(), 10),
		Date:               req.Pickup.Date,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported by Omniva.
func (m *MockOmnivaAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("Omniva", "pickup update", "cancel the existing order and create a new one")
}

// CancelPickup returns a canned success response.
func (m *MockOmnivaAdapter) CancelPickup(_ context.Context, _, _ string) error {
	return nil
}

// CloseManifest is not supported by Omniva.
func (m *MockOmnivaAdapter) CloseManifest(_ context.Context, _ ManifestRequest) (*ManifestResponse, error) {
	return nil, notSupported("Omniva", "manifest close", "Omniva has no end-of-day manifest endpoint")
}
