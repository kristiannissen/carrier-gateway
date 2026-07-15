// Package adapter provides the FedEx implementation of the CarrierAdapter interface.
// This file is located at /internal/adapter/fedex.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// fedexTokenCache holds a cached OAuth2 bearer token with its expiry time.
type fedexTokenCache struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// valid reports whether the cached token is present and not yet expired.
// A 30-second buffer is applied to avoid using a token that expires mid-request.
func (c *fedexTokenCache) valid() bool {
	return c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-30*time.Second))
}

// FedExAdapter implements CarrierAdapter for FedEx.
//
// Authentication:
//   - OAuth2 Bearer token via POST /oauth/token with form-encoded body.
//   - grant_type=client_credentials for standard integrators.
//   - For CSP/integrator child accounts use grant_type=csp_credentials with
//     ChildKey and ChildSecret in addition to ClientID and ClientSecret.
//   - Token lifetime is 3600 seconds; the adapter refreshes automatically.
//
// Default service type selection:
//   - Same sender/receiver country: FEDEX_GROUND
//   - Cross-border: FEDEX_INTERNATIONAL_PRIORITY
//
// Pending implementation:
//   - FetchLabel: label reprint endpoint spec pending — labels must be saved from the booking response.
type FedExAdapter struct {
	// ClientID is the API Key from the FedEx Developer Portal project.
	ClientID string
	// ClientSecret is the Secret Key from the FedEx Developer Portal project.
	ClientSecret string
	// AccountNumber is the FedEx account number — required by the Ship API.
	AccountNumber string
	// GrantType controls which OAuth2 flow is used.
	// "client_credentials" for standard B2B.
	// "csp_credentials" for Integrator/Compatible customers with child accounts.
	// "client_pc_credentials" for Proprietary Parent Child customers.
	GrantType string
	// ChildKey is the Customer Key for csp_credentials and client_pc_credentials flows.
	ChildKey string
	// ChildSecret is the Customer Password for csp_credentials and client_pc_credentials flows.
	ChildSecret string
	// BaseURL is the FedEx API base URL.
	// Production: https://apis.fedex.com
	// Sandbox:    https://apis-sandbox.fedex.com
	BaseURL    string
	HTTPClient *http.Client
	tokenCache fedexTokenCache
	log        *zap.Logger
}

// NewFedExAdapter creates a new FedExAdapter for standard B2B integrators.
// clientID and clientSecret are the API Key and Secret Key from the FedEx Developer Portal.
// accountNumber is the FedEx account number used in shipping requests.
func NewFedExAdapter(clientID, clientSecret, accountNumber string, log *zap.Logger) *FedExAdapter {
	return &FedExAdapter{
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		AccountNumber: accountNumber,
		GrantType:     "client_credentials",
		BaseURL:       "https://apis.fedex.com",
		HTTPClient:    &http.Client{Timeout: 30 * time.Second},
		log:           log,
	}
}

// fetchToken obtains a new OAuth2 bearer token via POST /oauth/token.
// The request body is application/x-www-form-urlencoded.
func (a *FedExAdapter) fetchToken(ctx context.Context) error {
	form := url.Values{}
	form.Set("grant_type", a.GrantType)
	form.Set("client_id", a.ClientID)
	form.Set("client_secret", a.ClientSecret)
	if a.ChildKey != "" {
		form.Set("child_Key", a.ChildKey)
	}
	if a.ChildSecret != "" {
		form.Set("child_secret", a.ChildSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BaseURL+"/oauth/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create FedEx token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("FedEx token request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read FedEx token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("FedEx token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to decode FedEx token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return fmt.Errorf("FedEx token response contained no access_token")
	}

	a.tokenCache.mu.Lock()
	a.tokenCache.accessToken = tokenResp.AccessToken
	a.tokenCache.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	a.tokenCache.mu.Unlock()

	return nil
}

// bearerToken returns a valid Bearer token, fetching a new one if expired or absent.
func (a *FedExAdapter) bearerToken(ctx context.Context) (string, error) {
	a.tokenCache.mu.Lock()
	valid := a.tokenCache.valid()
	token := a.tokenCache.accessToken
	a.tokenCache.mu.Unlock()

	if valid {
		return token, nil
	}
	if err := a.fetchToken(ctx); err != nil {
		return "", err
	}
	a.tokenCache.mu.Lock()
	token = a.tokenCache.accessToken
	a.tokenCache.mu.Unlock()
	return token, nil
}

// newFedExRequest builds an authenticated JSON request ready for the FedEx APIs.
func (a *FedExAdapter) newFedExRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain FedEx bearer token: %w", err)
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, a.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create FedEx request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-locale", "en_US")
	return req, nil
}

// ── Wire types ────────────────────────────────────────────────────────────────

type fedexAccountNumber struct {
	Value string `json:"value"`
}

// fedexShipRequest is the top-level body for POST /ship/v1/shipments.
type fedexShipRequest struct {
	AccountNumber        fedexAccountNumber     `json:"accountNumber"`
	LabelResponseOptions string                 `json:"labelResponseOptions"`
	RequestedShipment    fedexRequestedShipment `json:"requestedShipment"`
}

type fedexRequestedShipment struct {
	ServiceType               string                         `json:"serviceType"`
	PackagingType             string                         `json:"packagingType"`
	PickupType                string                         `json:"pickupType"`
	Shipper                   fedexParty                     `json:"shipper"`
	Recipients                []fedexParty                   `json:"recipients"`
	ShippingChargesPayment    fedexPayment                   `json:"shippingChargesPayment"`
	TotalWeight               fedexWeight                    `json:"totalWeight"`
	LabelSpecification        fedexLabelSpec                 `json:"labelSpecification"`
	RequestedPackageLineItems []fedexPackageLineItem         `json:"requestedPackageLineItems"`
	SpecialServicesRequested  *fedexSpecialServicesRequested `json:"specialServicesRequested,omitempty"`
	CustomsClearanceDetail    *fedexCustomsClearanceDetail   `json:"customsClearanceDetail,omitempty"`
}

// fedexSpecialServicesRequested carries shipment-level special service flags.
type fedexSpecialServicesRequested struct {
	SpecialServiceTypes     []string                      `json:"specialServiceTypes"`
	HoldAtLocationDetail    *fedexHoldAtLocationDetail    `json:"holdAtLocationDetail,omitempty"`
	ReturnShipmentDetail    *fedexReturnShipmentDetail    `json:"returnShipmentDetail,omitempty"`
	ShipmentCODDetail       *fedexShipmentCODDetail       `json:"shipmentCODDetail,omitempty"`
	EmailNotificationDetail *fedexEmailNotificationDetail `json:"emailNotificationDetail,omitempty"`
}

// fedexHoldAtLocationDetail specifies the Hold-at-Location destination.
// LocationID is the 4-character FedEx facility code returned by the
// Location Search API (e.g. "YBZA"). It maps to Address.ServicePointID
// in the gateway request.
type fedexHoldAtLocationDetail struct {
	LocationID string `json:"locationId"`
}

// fedexReturnShipmentDetail configures a printed return label.
// ReturnType must be PRINT_RETURN_LABEL for inline label generation.
type fedexReturnShipmentDetail struct {
	ReturnType string    `json:"returnType"`
	Rma        *fedexRMA `json:"rma,omitempty"`
}

// fedexRMA is an optional return merchant authorisation reference
// printed on the return label.
type fedexRMA struct {
	Reason string `json:"reason,omitempty"`
}

// fedexShipmentCODDetail carries Cash on Delivery parameters.
// FedEx COD is supported for Ground services only.
// CodCollectionType accepted values: ANY, CASH, GUARANTEED_FUNDS, COMPANY_CHECK, PERSONAL_CHECK.
type fedexShipmentCODDetail struct {
	CodCollectionType   string     `json:"codCollectionType"`
	CodCollectionAmount fedexMoney `json:"codCollectionAmount"`
}

// fedexEmailNotificationDetail enables proactive shipment-event email notifications.
// AggregationType controls whether notifications fire per shipment or per package.
type fedexEmailNotificationDetail struct {
	AggregationType             string                            `json:"aggregationType,omitempty"`
	EmailNotificationRecipients []fedexEmailNotificationRecipient `json:"emailNotificationRecipients"`
}

// fedexEmailNotificationRecipient is a single email notification target.
// NotificationEventTypes controls which events trigger an email.
type fedexEmailNotificationRecipient struct {
	EmailNotificationRecipientType string   `json:"emailNotificationRecipientType"`
	EmailAddress                   string   `json:"emailAddress"`
	NotificationType               string   `json:"notificationType,omitempty"`
	NotificationFormatType         string   `json:"notificationFormatType,omitempty"`
	NotificationEventTypes         []string `json:"notificationEventTypes"`
}

// fedexPackageSpecialServices holds per-package optional services.
type fedexPackageSpecialServices struct {
	// SignatureOptionType controls the signature requirement on delivery.
	// Accepted values: SERVICE_DEFAULT, NO_SIGNATURE_REQUIRED, INDIRECT, DIRECT, ADULT.
	SignatureOptionType string `json:"signatureOptionType,omitempty"`
}

type fedexParty struct {
	Address fedexAddress `json:"address"`
	Contact fedexContact `json:"contact"`
	Tins    []fedexTIN   `json:"tins,omitempty"`
}

// fedexTIN is a tax identification number attached to a shipper or recipient
// party. FedEx uses this for EORI, VAT, IOSS, and similar registrations.
// Usage identifies the purpose when the tinType enum has no dedicated IOSS value —
// FedEx accepts IOSS numbers on the shipper via tinType BUSINESS_NATIONAL + usage IOSS.
type fedexTIN struct {
	Number  string `json:"number"`
	TINType string `json:"tinType"`
	Usage   string `json:"usage,omitempty"`
}

// fedexMoney is a currency-qualified monetary amount.
type fedexMoney struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// fedexCustomerReference carries a typed reference on the commercial invoice.
type fedexCustomerReference struct {
	CustomerReferenceType string `json:"customerReferenceType"`
	Value                 string `json:"value"`
}

// fedexCommercialInvoice is the commercial invoice block within
// CustomsClearanceDetail. At least one field must be populated; FedEx
// requires the object to be present even when most fields are omitted.
type fedexCommercialInvoice struct {
	CustomerReferences []fedexCustomerReference `json:"customerReferences,omitempty"`
	Comments           []string                 `json:"comments,omitempty"`
}

// fedexDutiesPayment indicates who pays import duties.
// SENDER corresponds to DDP; RECIPIENT to DAP/DDU.
type fedexDutiesPayment struct {
	PaymentType string `json:"paymentType"`
}

// fedexCommodity is a single line item in the customs declaration.
type fedexCommodity struct {
	Description          string       `json:"description"`
	NumberOfPieces       int          `json:"numberOfPieces,omitempty"`
	Quantity             int          `json:"quantity,omitempty"`
	QuantityUnits        string       `json:"quantityUnits,omitempty"`
	Weight               *fedexWeight `json:"weight,omitempty"`
	CustomsValue         *fedexMoney  `json:"customsValue,omitempty"`
	CountryOfManufacture string       `json:"countryOfManufacture,omitempty"`
	HarmonizedCode       string       `json:"harmonizedCode,omitempty"`
}

// fedexCustomsClearanceDetail is the top-level customs block attached to the
// shipment request for international and intra-country customs-declarable
// shipments.
type fedexCustomsClearanceDetail struct {
	DutiesPayment     *fedexDutiesPayment    `json:"dutiesPayment,omitempty"`
	CommercialInvoice fedexCommercialInvoice `json:"commercialInvoice"`
	Commodities       []fedexCommodity       `json:"commodities"`
	TotalCustomsValue *fedexMoney            `json:"totalCustomsValue,omitempty"`
}

type fedexAddress struct {
	StreetLines         []string `json:"streetLines"`
	City                string   `json:"city"`
	StateOrProvinceCode string   `json:"stateOrProvinceCode,omitempty"`
	PostalCode          string   `json:"postalCode"`
	CountryCode         string   `json:"countryCode"`
	Residential         bool     `json:"residential"`
}

type fedexContact struct {
	PersonName  string `json:"personName,omitempty"`
	PhoneNumber string `json:"phoneNumber"`
	CompanyName string `json:"companyName,omitempty"`
}

type fedexPayment struct {
	PaymentType string `json:"paymentType"`
}

type fedexWeight struct {
	Units string  `json:"units"`
	Value float64 `json:"value"`
}

type fedexDimensions struct {
	Length int    `json:"length"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Units  string `json:"units"`
}

type fedexLabelSpec struct {
	ImageType      string `json:"imageType"`
	LabelStockType string `json:"labelStockType"`
}

type fedexPackageLineItem struct {
	Weight                 fedexWeight                  `json:"weight"`
	Dimensions             *fedexDimensions             `json:"dimensions,omitempty"`
	DeclaredValue          *fedexMoney                  `json:"declaredValue,omitempty"`
	PackageSpecialServices *fedexPackageSpecialServices `json:"packageSpecialServices,omitempty"`
}

// fedexShipResponse is the top-level response from POST /ship/v1/shipments.
type fedexShipResponse struct {
	TransactionID string          `json:"transactionId"`
	Output        fedexShipOutput `json:"output"`
}

type fedexShipOutput struct {
	TransactionShipments []fedexTransactionShipment `json:"transactionShipments"`
}

type fedexTransactionShipment struct {
	MasterTrackingNumber string               `json:"masterTrackingNumber"`
	PieceResponses       []fedexPieceResponse `json:"pieceResponses"`
}

type fedexPieceResponse struct {
	TrackingNumber   string          `json:"trackingNumber"`
	PackageDocuments []fedexLabelDoc `json:"packageDocuments"`
}

// fedexLabelDoc holds an inline encoded label or a retrieval URL.
type fedexLabelDoc struct {
	EncodedLabel string `json:"encodedLabel"`
	DocType      string `json:"docType"`
	URL          string `json:"url"`
}

// fedexCancelRequest is the body for PUT /ship/v1/shipments/cancel.
type fedexCancelRequest struct {
	AccountNumber   fedexAccountNumber `json:"accountNumber"`
	TrackingNumber  string             `json:"trackingNumber"`
	DeletionControl string             `json:"deletionControl,omitempty"`
}

// fedexTrackRequest is the body for POST /track/v1/trackingnumbers.
type fedexTrackRequest struct {
	IncludeDetailedScans bool                `json:"includeDetailedScans"`
	TrackingInfo         []fedexTrackingInfo `json:"trackingInfo"`
}

type fedexTrackingInfo struct {
	TrackingNumberInfo fedexTrackingNumberInfo `json:"trackingNumberInfo"`
}

type fedexTrackingNumberInfo struct {
	TrackingNumber string `json:"trackingNumber"`
}

// fedexTrackResponse is the top-level response from POST /track/v1/trackingnumbers.
// The output field schema in the spec is empty; in practice it contains
// completeTrackResults matching TrackingNumbersResponse.
type fedexTrackResponse struct {
	Output fedexTrackOutput `json:"output"`
}

type fedexTrackOutput struct {
	CompleteTrackResults []fedexCompleteTrackResult `json:"completeTrackResults"`
}

type fedexCompleteTrackResult struct {
	TrackingNumber string             `json:"trackingNumber"`
	TrackResults   []fedexTrackResult `json:"trackResults"`
}

type fedexTrackResult struct {
	LatestStatusDetail fedexStatusDetail  `json:"latestStatusDetail"`
	ScanEvents         []fedexScanEvent   `json:"scanEvents"`
	DateAndTimes       []fedexDateAndTime `json:"dateAndTimes"`
}

type fedexStatusDetail struct {
	Code           string `json:"code"`
	DerivedCode    string `json:"derivedCode"`
	Description    string `json:"description"`
	StatusByLocale string `json:"statusByLocale"`
}

type fedexScanEvent struct {
	Date              string          `json:"date"`
	EventType         string          `json:"eventType"`
	DerivedStatusCode string          `json:"derivedStatusCode"`
	EventDescription  string          `json:"eventDescription"`
	DerivedStatus     string          `json:"derivedStatus"`
	ScanLocation      fedexAddressVO1 `json:"scanLocation"`
}

// fedexAddressVO1 mirrors the AddressVO1 schema used for scan locations.
type fedexAddressVO1 struct {
	City                string `json:"city"`
	StateOrProvinceCode string `json:"stateOrProvinceCode"`
	CountryCode         string `json:"countryCode"`
}

type fedexDateAndTime struct {
	DateTime string `json:"dateTime"`
	Type     string `json:"type"`
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// fedexServiceType picks a FedEx service type based on sender/receiver countries.
// Same-country shipments default to FEDEX_GROUND; cross-border defaults to
// FEDEX_INTERNATIONAL_PRIORITY. A future extension can expose this as a
// carrier-specific override on BookingRequest.
func fedexServiceType(s Shipment) string {
	if s.Sender.Country == s.Receiver.Country {
		return "FEDEX_GROUND"
	}
	return "FEDEX_INTERNATIONAL_PRIORITY"
}

// fedexPartyFrom maps a gateway Address to a FedEx Party.
// FedEx requires streetLines (array) rather than a single street field.
// HouseNumber is appended to Street on the same line — FedEx does not have
// a separate house-number field.
func fedexPartyFrom(addr Address) fedexParty {
	streetLine := addr.Street
	if addr.HouseNumber != "" {
		streetLine += " " + addr.HouseNumber
	}
	lines := []string{streetLine}
	if addr.Supplement != "" {
		lines = append(lines, addr.Supplement)
	}

	return fedexParty{
		Address: fedexAddress{
			StreetLines:         lines,
			City:                addr.City,
			StateOrProvinceCode: addr.State,
			PostalCode:          addr.PostalCode,
			CountryCode:         addr.Country,
		},
		Contact: fedexContact{
			PersonName:  addr.Name,
			PhoneNumber: addr.Phone,
		},
	}
}

// fedexImageType maps a gateway LabelFormat to the FedEx imageType string.
// Defaults to "PDF" for unrecognised or empty formats.
func fedexImageType(f LabelFormat) string {
	switch f {
	case LabelFormatZPL, LabelFormatZPLGK:
		return "ZPLII"
	case LabelFormatPNG:
		return "PNG"
	case LabelFormatEPL:
		return "EPL2"
	default:
		return "PDF"
	}
}

// fedexLabelStockType returns an appropriate label stock type for the given imageType.
// Thermal/ZPL printers use roll stock; PDF uses paper.
func fedexLabelStockType(imageType string) string {
	switch imageType {
	case "ZPLII", "EPL2":
		return "STOCK_4X6"
	default:
		return "PAPER_7X475"
	}
}

// fedexShipmentSpecialServices builds the shipment-level special services block
// from the gateway Shipment. Returns nil when no special services are needed.
//
// Handled services:
//   - HOLD_AT_LOCATION when Receiver.ServicePointID is set
//   - RETURN_SHIPMENT when DeliveryType is "return"
//   - COD when AddOnCashOnDelivery is present (Ground only)
//   - Email notifications when AddOnEmailNotification is present and Receiver.Email is set
func fedexShipmentSpecialServices(s Shipment) *fedexSpecialServicesRequested {
	var types []string
	var ss fedexSpecialServicesRequested

	if s.Receiver.ServicePointID != "" {
		types = append(types, "HOLD_AT_LOCATION")
		ss.HoldAtLocationDetail = &fedexHoldAtLocationDetail{
			LocationID: s.Receiver.ServicePointID,
		}
	}

	if s.DeliveryType == "return" {
		types = append(types, "RETURN_SHIPMENT")
		ss.ReturnShipmentDetail = &fedexReturnShipmentDetail{
			ReturnType: "PRINT_RETURN_LABEL",
		}
	}

	if cod, ok := getAddOn(s.AddOns, AddOnCashOnDelivery); ok && cod.CODAmount > 0 {
		cur := cod.CODCurrency
		if cur == "" {
			cur = "EUR"
		}
		types = append(types, "COD")
		ss.ShipmentCODDetail = &fedexShipmentCODDetail{
			CodCollectionType:   "ANY",
			CodCollectionAmount: fedexMoney{Amount: cod.CODAmount, Currency: cur},
		}
	}

	if hasAddOn(s.AddOns, AddOnEmailNotification) && s.Receiver.Email != "" {
		ss.EmailNotificationDetail = &fedexEmailNotificationDetail{
			AggregationType: "PER_SHIPMENT",
			EmailNotificationRecipients: []fedexEmailNotificationRecipient{
				{
					EmailNotificationRecipientType: "RECIPIENT",
					EmailAddress:                   s.Receiver.Email,
					NotificationFormatType:         "HTML",
					NotificationEventTypes: []string{
						"ON_SHIPMENT",
						"ON_ESTIMATED_DELIVERY",
						"ON_DELIVERY",
						"ON_EXCEPTION",
					},
				},
			},
		}
	}

	if len(types) == 0 && ss.EmailNotificationDetail == nil {
		return nil
	}
	ss.SpecialServiceTypes = types
	return &ss
}

// fedexCustomsBlock builds a CustomsClearanceDetail from the gateway Customs
// struct. Returns nil when there are no line items (domestic / no customs).
// IOSS has no FedEx tinType equivalent and is skipped with a warning.
// InvoiceDate has no dedicated FedEx field and is shimmed into
// commercialInvoice.comments when non-empty.
func fedexCustomsBlock(c Customs, log *zap.Logger) *fedexCustomsClearanceDetail {
	if len(c.Items) == 0 {
		return nil
	}

	detail := &fedexCustomsClearanceDetail{}

	// Duties payment: DDP → sender pays, everything else → recipient pays.
	if c.Incoterms == "DDP" {
		detail.DutiesPayment = &fedexDutiesPayment{PaymentType: "SENDER"}
	} else {
		detail.DutiesPayment = &fedexDutiesPayment{PaymentType: "RECIPIENT"}
	}

	// Total customs value.
	if c.CustomsValue > 0 {
		cur := c.CustomsCurrency
		if cur == "" {
			cur = "EUR"
		}
		detail.TotalCustomsValue = &fedexMoney{Amount: c.CustomsValue, Currency: cur}
	}

	// Commercial invoice.
	inv := fedexCommercialInvoice{}
	if c.InvoiceNumber != "" {
		inv.CustomerReferences = []fedexCustomerReference{
			{CustomerReferenceType: "INVOICE_NUMBER", Value: c.InvoiceNumber},
		}
	}
	if c.InvoiceDate != "" {
		inv.Comments = []string{"Invoice date: " + c.InvoiceDate}
	}
	detail.CommercialInvoice = inv

	// Commodities.
	cur := c.CustomsCurrency
	if cur == "" {
		cur = "EUR"
	}
	commodities := make([]fedexCommodity, len(c.Items))
	for i, item := range c.Items {
		com := fedexCommodity{
			Description:   item.Description,
			Quantity:      item.Quantity,
			QuantityUnits: "EA",
		}
		hsCode := item.HSCode
		if hsCode == "" {
			hsCode = c.HSCode
		}
		com.HarmonizedCode = hsCode

		origin := item.CountryOfOrigin
		if origin == "" {
			origin = c.CountryOfOrigin
		}
		com.CountryOfManufacture = origin

		if item.NetWeight > 0 {
			com.Weight = &fedexWeight{Units: "KG", Value: item.NetWeight}
		}

		itemCur := item.Currency
		if itemCur == "" {
			itemCur = cur
		}
		if item.Value > 0 {
			com.CustomsValue = &fedexMoney{Amount: item.Value, Currency: itemCur}
		}

		commodities[i] = com
	}
	detail.Commodities = commodities

	// IossNumber is handled at the shipper party level (fedexPartyTINs), not
	// in the customs block — FedEx accepts IOSS via shipper.tins with usage IOSS.

	return detail
}

// fedexPartyTINs returns the tins slice for the shipper party from EORI, VAT,
// and IOSS numbers. Any may be empty; nil is returned when all are empty.
//
// FedEx has no dedicated IOSS tinType — the IOSS number is passed on the
// shipper with tinType BUSINESS_NATIONAL and usage IOSS, per FedEx EU VAT
// documentation (shipper\tins\number).
func fedexPartyTINs(eori, vat, ioss string) []fedexTIN {
	var tins []fedexTIN
	if eori != "" {
		tins = append(tins, fedexTIN{Number: eori, TINType: "BUSINESS_NATIONAL"})
	}
	if vat != "" {
		tins = append(tins, fedexTIN{Number: vat, TINType: "FEDERAL"})
	}
	if ioss != "" {
		tins = append(tins, fedexTIN{Number: ioss, TINType: "BUSINESS_NATIONAL", Usage: "IOSS"})
	}
	return tins
}

// fedexPackageItems converts gateway Colli to FedEx RequestedPackageLineItems,
// applying per-shipment add-ons (insurance, signature) to every package.
//
// Declared value is divided evenly across packages and rounded to two decimal
// places. Dimensions are rounded up from float64 cm to integer cm to avoid
// underreporting.
func fedexPackageItems(colli []Colli, addOns []AddOn) []fedexPackageLineItem {
	var sigOption string
	if hasAddOn(addOns, AddOnSignatureRequired) {
		sigOption = "DIRECT"
	}

	var declaredPerPkg *fedexMoney
	if ins, ok := getAddOn(addOns, AddOnInsurance); ok && ins.InsuranceValue > 0 {
		n := float64(len(colli))
		perPkg := math.Round(ins.InsuranceValue/n*100) / 100
		cur := ins.InsuranceCurrency
		if cur == "" {
			cur = "EUR"
		}
		declaredPerPkg = &fedexMoney{Amount: perPkg, Currency: cur}
	}

	items := make([]fedexPackageLineItem, len(colli))
	for i, c := range colli {
		item := fedexPackageLineItem{
			Weight: fedexWeight{Units: "KG", Value: c.Weight},
		}
		d := c.Dimensions
		if d.Length > 0 || d.Width > 0 || d.Height > 0 {
			item.Dimensions = &fedexDimensions{
				Length: int(math.Ceil(d.Length)),
				Width:  int(math.Ceil(d.Width)),
				Height: int(math.Ceil(d.Height)),
				Units:  "CM",
			}
		}
		if declaredPerPkg != nil {
			item.DeclaredValue = declaredPerPkg
		}
		if sigOption != "" {
			item.PackageSpecialServices = &fedexPackageSpecialServices{
				SignatureOptionType: sigOption,
			}
		}
		items[i] = item
	}
	return items
}

// ── CarrierAdapter methods ────────────────────────────────────────────────────

// BookShipment books a FedEx shipment via POST /ship/v1/shipments.
//
// Service type is derived from sender/receiver countries (see fedexServiceType).
// Labels are returned inline as base64-encoded data URIs in each ColliResponse.LabelURL.
// The label format is taken from r.LabelFormat (PDF, PNG, ZPL, EPL); defaults to PDF.
func (a *FedExAdapter) BookShipment(ctx context.Context, r BookingRequest) (*BookingResponse, error) {
	customs := r.Shipment.Customs

	shipper := fedexPartyFrom(r.Shipment.Sender)
	shipper.Tins = fedexPartyTINs("", customs.ExporterVATNumber, customs.IossNumber)

	recipient := fedexPartyFrom(r.Shipment.Receiver)
	recipient.Tins = fedexPartyTINs(customs.ImporterOfRecord, customs.ImporterVATNumber, "")

	imageType := fedexImageType(r.LabelFormat)

	shipReq := fedexShipRequest{
		AccountNumber:        fedexAccountNumber{Value: a.AccountNumber},
		LabelResponseOptions: "LABEL",
		RequestedShipment: fedexRequestedShipment{
			ServiceType:            fedexServiceType(r.Shipment),
			PackagingType:          "YOUR_PACKAGING",
			PickupType:             "USE_SCHEDULED_PICKUP",
			Shipper:                shipper,
			Recipients:             []fedexParty{recipient},
			ShippingChargesPayment: fedexPayment{PaymentType: "SENDER"},
			TotalWeight:            fedexWeight{Units: "KG", Value: r.Shipment.TotalWeight},
			LabelSpecification: fedexLabelSpec{
				ImageType:      imageType,
				LabelStockType: fedexLabelStockType(imageType),
			},
			RequestedPackageLineItems: fedexPackageItems(r.Shipment.Colli, r.Shipment.AddOns),
			SpecialServicesRequested:  fedexShipmentSpecialServices(r.Shipment),
			CustomsClearanceDetail:    fedexCustomsBlock(customs, a.log),
		},
	}

	body, err := json.Marshal(shipReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FedEx ship request: %w", err)
	}

	httpReq, err := a.newFedExRequest(ctx, http.MethodPost, "/ship/v1/shipments", body)
	if err != nil {
		return nil, fmt.Errorf("fedex: create ship request: %w", err)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("FedEx ship request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read FedEx ship response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FedEx ship API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var shipResp fedexShipResponse
	if err := json.Unmarshal(respBody, &shipResp); err != nil {
		return nil, fmt.Errorf("failed to decode FedEx ship response: %w", err)
	}
	if len(shipResp.Output.TransactionShipments) == 0 {
		return nil, fmt.Errorf("FedEx ship response contained no transaction shipments")
	}

	txn := shipResp.Output.TransactionShipments[0]

	colli := make([]ColliResponse, 0, len(txn.PieceResponses))
	for i, piece := range txn.PieceResponses {
		cr := ColliResponse{
			TrackingNumber: piece.TrackingNumber,
			Status:         "booked",
		}
		if i < len(r.Shipment.Colli) {
			cr.ID = r.Shipment.Colli[i].ID
		}
		if len(piece.PackageDocuments) > 0 {
			if doc := piece.PackageDocuments[0]; doc.EncodedLabel != "" {
				mime := MimeTypeForFormat(r.LabelFormat)
				if mime == "" {
					mime = "application/pdf"
				}
				cr.LabelURL = "data:" + mime + ";base64," + doc.EncodedLabel
			}
		}
		colli = append(colli, cr)
	}

	masterTN := txn.MasterTrackingNumber
	if masterTN == "" && len(colli) > 0 {
		masterTN = colli[0].TrackingNumber
	}

	var labelURL string
	if len(colli) > 0 {
		labelURL = colli[0].LabelURL
	}

	a.log.Info("FedEx shipment booked",
		zap.String("masterTrackingNumber", masterTN),
		zap.Int("packages", len(colli)),
	)

	return &BookingResponse{
		TrackingNumber: masterTN,
		LabelURL:       labelURL,
		Carrier:        "fedex",
		Status:         "booked",
		Colli:          colli,
	}, nil
}

// TrackShipment retrieves FedEx shipment status via POST /track/v1/trackingnumbers.
//
// The top-level status is taken from latestStatusDetail.code; individual scan
// events are sourced from scanEvents[]. Estimated delivery is surfaced when
// a dateAndTimes entry with type ESTIMATED_DELIVERY is present.
func (a *FedExAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	trackReq := fedexTrackRequest{
		IncludeDetailedScans: true,
		TrackingInfo: []fedexTrackingInfo{
			{TrackingNumberInfo: fedexTrackingNumberInfo{TrackingNumber: trackingNumber}},
		},
	}

	body, err := json.Marshal(trackReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FedEx track request: %w", err)
	}

	httpReq, err := a.newFedExRequest(ctx, http.MethodPost, "/track/v1/trackingnumbers", body)
	if err != nil {
		return nil, fmt.Errorf("fedex: create track request: %w", err)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("FedEx track request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read FedEx track response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FedEx track API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var trackResp fedexTrackResponse
	if err := json.Unmarshal(respBody, &trackResp); err != nil {
		return nil, fmt.Errorf("failed to decode FedEx track response: %w", err)
	}

	if len(trackResp.Output.CompleteTrackResults) == 0 {
		return nil, fmt.Errorf("FedEx track response contained no results for %s", trackingNumber)
	}
	ctr := trackResp.Output.CompleteTrackResults[0]
	if len(ctr.TrackResults) == 0 {
		return nil, fmt.Errorf("FedEx track response contained no track results for %s", trackingNumber)
	}
	result := ctr.TrackResults[0]

	// Derive top-level status from latestStatusDetail.code.
	rawStatus := result.LatestStatusDetail.Code
	if rawStatus == "" {
		rawStatus = result.LatestStatusDetail.DerivedCode
	}
	normalized := normalizeStatus("fedex", rawStatus)

	// Build event list from scan events (newest first from FedEx).
	events := make([]TrackingEvent, 0, len(result.ScanEvents))
	for _, e := range result.ScanEvents {
		evtCode := e.EventType
		if evtCode == "" {
			evtCode = e.DerivedStatusCode
		}
		events = append(events, TrackingEvent{
			Timestamp:        e.Date,
			Status:           evtCode,
			NormalizedStatus: normalizeStatus("fedex", evtCode),
			Location:         fedexLocation(e.ScanLocation),
			Details:          e.EventDescription,
		})
	}

	// Estimated delivery from dateAndTimes.
	var estimatedDelivery string
	for _, dt := range result.DateAndTimes {
		if dt.Type == "ESTIMATED_DELIVERY" || dt.Type == "ACTUAL_DELIVERY" {
			estimatedDelivery = dt.DateTime
			break
		}
	}

	return &TrackingResponse{
		TrackingNumber:    trackingNumber,
		Carrier:           "fedex",
		Status:            rawStatus,
		NormalizedStatus:  normalized,
		OriginalStatus:    rawStatus,
		Events:            events,
		EstimatedDelivery: estimatedDelivery,
	}, nil
}

// fedexLocation formats a FedEx AddressVO1 scan location into a human-readable string.
func fedexLocation(addr fedexAddressVO1) string {
	if addr.City == "" {
		return addr.CountryCode
	}
	parts := addr.City
	if addr.StateOrProvinceCode != "" {
		parts += ", " + addr.StateOrProvinceCode
	}
	if addr.CountryCode != "" {
		parts += ", " + addr.CountryCode
	}
	return parts
}

// FetchLabel retrieves a FedEx shipping label.
//
// FedEx labels are returned inline in the BookShipment response as base64.
// A dedicated reprint endpoint may exist — pending documentation.
func (a *FedExAdapter) FetchLabel(_ context.Context, _ LabelRequest) (*LabelResponse, error) {
	return nil, notSupported("FedEx", "label fetch", "label reprint endpoint pending — spec not yet available")
}

// CancelShipment cancels a FedEx shipment via PUT /ship/v1/shipments/cancel.
// All packages in the shipment are cancelled (DeletionControl: DELETE_ALL_PACKAGES).
func (a *FedExAdapter) CancelShipment(ctx context.Context, trackingNumber string) (*CancelResponse, error) {
	cancelReq := fedexCancelRequest{
		AccountNumber:   fedexAccountNumber{Value: a.AccountNumber},
		TrackingNumber:  trackingNumber,
		DeletionControl: "DELETE_ALL_PACKAGES",
	}

	body, err := json.Marshal(cancelReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FedEx cancel request: %w", err)
	}

	httpReq, err := a.newFedExRequest(ctx, http.MethodPut, "/ship/v1/shipments/cancel", body)
	if err != nil {
		return nil, fmt.Errorf("fedex: create cancel request: %w", err)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("FedEx cancel request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read FedEx cancel response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FedEx cancel API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	a.log.Info("FedEx shipment cancelled", zap.String("trackingNumber", trackingNumber))

	return &CancelResponse{
		TrackingNumber: trackingNumber,
		Carrier:        "fedex",
		Status:         "cancelled",
	}, nil
}

// UpdateShipment applies post-booking updates to a FedEx shipment.
//
// FedEx does not support post-booking updates via the Ship API.
// Address corrections require a cancel-and-rebook cycle.
func (a *FedExAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("FedEx", "post-booking update", "")
}

// ── Pickup wire types ─────────────────────────────────────────────────────────

// fedexPickupOriginDetail holds the collection location and time window.
type fedexPickupOriginDetail struct {
	PickupLocation     fedexPickupLocationParty `json:"pickupLocation"`
	ReadyDateTimestamp string                   `json:"readyDateTimestamp"`
	CustomerCloseTime  string                   `json:"customerCloseTime"`
	// PackageLocation is required for FDXG (Ground) pickups.
	// Accepted values: FRONT, NONE, REAR, SIDE.
	PackageLocation string `json:"packageLocation,omitempty"`
}

type fedexPickupLocationParty struct {
	Contact fedexPickupContact `json:"contact"`
	Address fedexPickupAddress `json:"address"`
}

type fedexPickupContact struct {
	PersonName  string `json:"personName,omitempty"`
	CompanyName string `json:"companyName,omitempty"`
	PhoneNumber string `json:"phoneNumber"`
}

type fedexPickupAddress struct {
	StreetLines         []string `json:"streetLines"`
	City                string   `json:"city"`
	StateOrProvinceCode string   `json:"stateOrProvinceCode,omitempty"`
	PostalCode          string   `json:"postalCode"`
	CountryCode         string   `json:"countryCode"`
}

// fedexCreatePickupRequest is the body for POST /pickup/v1/pickups.
type fedexCreatePickupRequest struct {
	AssociatedAccountNumber fedexAccountNumber      `json:"associatedAccountNumber"`
	OriginDetail            fedexPickupOriginDetail `json:"originDetail"`
	PackageCount            int                     `json:"packageCount,omitempty"`
	TotalWeight             *fedexWeight            `json:"totalWeight,omitempty"`
	// CarrierCode selects the FedEx operating company: FDXE (Express) or FDXG (Ground).
	CarrierCode string `json:"carrierCode"`
	// Remarks is passed to the courier; max 60 characters.
	Remarks string `json:"remarks,omitempty"`
}

// fedexCreatePickupResponse wraps the Create Pickup API response.
type fedexCreatePickupResponse struct {
	TransactionID string                  `json:"transactionId"`
	Output        fedexCreatePickupOutput `json:"output"`
}

type fedexCreatePickupOutput struct {
	// PickupConfirmationCode is the carrier-issued pickup reference.
	PickupConfirmationCode string `json:"pickupConfirmationCode"`
	// Location is the FedEx Express facility responsible for the dispatch.
	// Required when cancelling a FedEx Express pickup.
	Location string `json:"location"`
}

// fedexCancelPickupRequest is the body for PUT /pickup/v1/pickups/cancel.
type fedexCancelPickupRequest struct {
	AssociatedAccountNumber fedexAccountNumber `json:"associatedAccountNumber"`
	PickupConfirmationCode  string             `json:"pickupConfirmationCode"`
	// ScheduledDate is the pickup dispatch date in YYYY-MM-DD format.
	ScheduledDate string `json:"scheduledDate"`
	// CarrierCode is optional; defaults to FDXE on the FedEx side.
	CarrierCode string `json:"carrierCode,omitempty"`
	// Location is required for FedEx Express pickups; returned by BookPickup.
	Location string `json:"location,omitempty"`
	// Remarks is passed to the courier; max 60 characters.
	Remarks string `json:"remarks,omitempty"`
}

// fedexPickupAvailabilityRequest is the body for POST /pickup/v1/pickups/availabilities.
type fedexPickupAvailabilityRequest struct {
	PickupAddress       fedexAvailabilityAddress `json:"pickupAddress"`
	Carriers            []string                 `json:"carriers"`
	CountryRelationship string                   `json:"countryRelationship"`
	PickupRequestType   []string                 `json:"pickupRequestType"`
	DispatchDate        string                   `json:"dispatchDate,omitempty"`
	PackageReadyTime    string                   `json:"packageReadyTime,omitempty"`
	CustomerCloseTime   string                   `json:"customerCloseTime,omitempty"`
}

type fedexAvailabilityAddress struct {
	PostalCode  string `json:"postalCode"`
	CountryCode string `json:"countryCode"`
}

// fedexPickupAvailabilityResponse wraps the Pickup Availability API response.
type fedexPickupAvailabilityResponse struct {
	TransactionID string                        `json:"transactionId"`
	Output        fedexPickupAvailabilityOutput `json:"output"`
}

type fedexPickupAvailabilityOutput struct {
	Options []fedexPickupScheduleOption `json:"options"`
}

type fedexPickupScheduleOption struct {
	Carrier           string   `json:"carrier"`
	Available         bool     `json:"available"`
	PickupDate        string   `json:"pickupDate"`
	CutOffTime        string   `json:"cutOffTime"`
	ReadyTimeOptions  []string `json:"readyTimeOptions"`
	LatestTimeOptions []string `json:"latestTimeOptions"`
	ScheduleDay       string   `json:"scheduleDay"`
}

// fedexEndOfDayRequest is the body for PUT /ship/v1/endofday/.
// closeReqType must be "GCDR" for a Ground end-of-day close.
type fedexEndOfDayRequest struct {
	CloseReqType               string             `json:"closeReqType"`
	AccountNumber              fedexAccountNumber `json:"accountNumber"`
	GroundServiceCategory      string             `json:"groundServiceCategory"`
	CloseDate                  string             `json:"closeDate,omitempty"`
	CloseDocumentSpecification *fedexCloseDocSpec `json:"closeDocumentSpecification,omitempty"`
}

// fedexCloseDocSpec requests specific document types in the close response.
type fedexCloseDocSpec struct {
	CloseDocumentTypes []string `json:"closeDocumentTypes"`
}

// fedexEndOfDayResponse is the top-level response from PUT /ship/v1/endofday/.
type fedexEndOfDayResponse struct {
	TransactionID string           `json:"transactionId"`
	Output        fedexCloseOutput `json:"output"`
}

type fedexCloseOutput struct {
	CloseDocuments []fedexCloseDocument `json:"closeDocuments"`
}

type fedexCloseDocument struct {
	Type  string           `json:"type"`
	Parts []fedexClosePart `json:"parts"`
}

type fedexClosePart struct {
	Image string `json:"image"`
}

// ── ManifestAdapter methods ───────────────────────────────────────────────────

// BookPickup schedules a FedEx collection via POST /pickup/v1/pickups.
//
// The returned ConfirmationNumber is an opaque pipe-delimited token encoding
// the FedEx confirmation code, scheduled date, and Express facility location:
//
//	{confirmationCode}|{YYYY-MM-DD}|{location}
//
// Callers must pass this token unchanged to CancelPickup; do not parse it.
// The location field is empty for Ground pickups and populated for Express.
func (a *FedExAdapter) BookPickup(ctx context.Context, req PickupRequest) (*PickupResponse, error) {
	// Build readyDateTimestamp as YYYY-MM-DDTHH:MM:SS (no TZD per FedEx spec).
	readyTime := req.Pickup.ReadyTime
	if readyTime == "" {
		readyTime = "09:00"
	}
	readyTS := req.Pickup.Date + "T" + readyTime + ":00"

	closeTime := req.Pickup.CloseTime
	if closeTime == "" {
		closeTime = "18:00"
	}
	// Pad HH:MM → HH:MM:SS.
	if len(closeTime) == 5 {
		closeTime += ":00"
	}

	streetLine := req.Address.Street
	if req.Address.HouseNumber != "" {
		streetLine += " " + req.Address.HouseNumber
	}

	pickupReq := fedexCreatePickupRequest{
		AssociatedAccountNumber: fedexAccountNumber{Value: a.AccountNumber},
		OriginDetail: fedexPickupOriginDetail{
			PickupLocation: fedexPickupLocationParty{
				Contact: fedexPickupContact{
					PersonName:  req.Contact.Name,
					PhoneNumber: req.Contact.Phone,
				},
				Address: fedexPickupAddress{
					StreetLines: []string{streetLine},
					City:        req.Address.City,
					PostalCode:  req.Address.PostalCode,
					CountryCode: req.Address.Country,
				},
			},
			ReadyDateTimestamp: readyTS,
			CustomerCloseTime:  closeTime,
		},
		CarrierCode: "FDXE",
	}

	if req.EstimatedParcels > 0 {
		pickupReq.PackageCount = req.EstimatedParcels
	}
	if req.EstimatedWeight > 0 {
		pickupReq.TotalWeight = &fedexWeight{Units: "KG", Value: req.EstimatedWeight}
	}
	if req.Pickup.SpecialInstructions != "" {
		// FedEx limits remarks to 60 characters.
		remarks := req.Pickup.SpecialInstructions
		if len(remarks) > 60 {
			remarks = remarks[:60]
		}
		pickupReq.Remarks = remarks
	}

	body, err := json.Marshal(pickupReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FedEx create pickup request: %w", err)
	}

	httpReq, err := a.newFedExRequest(ctx, http.MethodPost, "/pickup/v1/pickups", body)
	if err != nil {
		return nil, fmt.Errorf("fedex: create pickup request: %w", err)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("FedEx create pickup request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read FedEx create pickup response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FedEx pickup API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var pickupResp fedexCreatePickupResponse
	if err := json.Unmarshal(respBody, &pickupResp); err != nil {
		return nil, fmt.Errorf("failed to decode FedEx create pickup response: %w", err)
	}
	if pickupResp.Output.PickupConfirmationCode == "" {
		return nil, fmt.Errorf("FedEx pickup API returned empty confirmation code")
	}

	// Encode the three fields needed for cancellation into an opaque token.
	// Format: {code}|{date}|{location} — location may be empty for Ground pickups.
	token := pickupResp.Output.PickupConfirmationCode + "|" + req.Pickup.Date + "|" + pickupResp.Output.Location

	a.log.Info("FedEx pickup booked",
		zap.String("confirmationCode", pickupResp.Output.PickupConfirmationCode),
		zap.String("location", pickupResp.Output.Location),
		zap.String("date", req.Pickup.Date),
	)

	return &PickupResponse{
		Carrier:            "fedex",
		ConfirmationNumber: token,
		Date:               req.Pickup.Date,
		ReadyTime:          req.Pickup.ReadyTime,
		CloseTime:          req.Pickup.CloseTime,
		Status:             "booked",
	}, nil
}

// UpdatePickup is not supported by FedEx.
// Cancel the existing pickup and book a new one instead.
func (a *FedExAdapter) UpdatePickup(_ context.Context, _ string, _ PickupRequest) (*PickupResponse, error) {
	return nil, notSupported("FedEx", "pickup update", "cancel the existing pickup and book a new one")
}

// CancelPickup cancels a FedEx pickup via PUT /pickup/v1/pickups/cancel.
//
// confirmationNumber must be the opaque token returned by BookPickup
// ({code}|{date}|{location}). Passing the raw FedEx confirmation code
// directly is not supported because the cancel endpoint also requires
// the scheduled date and Express facility location.
func (a *FedExAdapter) CancelPickup(ctx context.Context, _ string, confirmationNumber string) error {
	// Parse the opaque token produced by BookPickup.
	parts := strings.SplitN(confirmationNumber, "|", 3)
	if len(parts) != 3 {
		return fmt.Errorf("FedEx: invalid confirmation number %q: expected {code}|{date}|{location} token from BookPickup", confirmationNumber)
	}
	code, scheduledDate, location := parts[0], parts[1], parts[2]

	cancelReq := fedexCancelPickupRequest{
		AssociatedAccountNumber: fedexAccountNumber{Value: a.AccountNumber},
		PickupConfirmationCode:  code,
		ScheduledDate:           scheduledDate,
		CarrierCode:             "FDXE",
	}
	if location != "" {
		cancelReq.Location = location
	}

	body, err := json.Marshal(cancelReq)
	if err != nil {
		return fmt.Errorf("failed to marshal FedEx cancel pickup request: %w", err)
	}

	httpReq, err := a.newFedExRequest(ctx, http.MethodPut, "/pickup/v1/pickups/cancel", body)
	if err != nil {
		return err
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("FedEx cancel pickup request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read FedEx cancel pickup response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("FedEx pickup cancel API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	a.log.Info("FedEx pickup cancelled",
		zap.String("confirmationCode", code),
		zap.String("scheduledDate", scheduledDate),
	)

	return nil
}

// CloseManifest closes FedEx Ground shipments for the day via PUT /ship/v1/endofday/.
// This is required for FedEx Ground (FDXG) accounts before the driver arrives.
// FedEx Express (FDXE) accounts do not require a close call; if no Ground
// shipments are open the API returns success with an empty closeDocuments list,
// which is surfaced as a warning rather than an error.
func (a *FedExAdapter) CloseManifest(ctx context.Context, req ManifestRequest) (*ManifestResponse, error) {
	closeDate := req.Date
	if closeDate == "" {
		closeDate = time.Now().Format("2006-01-02")
	}

	closeReq := fedexEndOfDayRequest{
		CloseReqType:          "GCDR",
		AccountNumber:         fedexAccountNumber{Value: a.AccountNumber},
		GroundServiceCategory: "GROUND",
		CloseDate:             closeDate,
		CloseDocumentSpecification: &fedexCloseDocSpec{
			CloseDocumentTypes: []string{"MANIFEST"},
		},
	}

	body, err := json.Marshal(closeReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FedEx close request: %w", err)
	}

	httpReq, err := a.newFedExRequest(ctx, http.MethodPut, "/ship/v1/endofday/", body)
	if err != nil {
		return nil, fmt.Errorf("fedex: create end-of-day request: %w", err)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("FedEx end-of-day close request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read FedEx close response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FedEx close API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var closeResp fedexEndOfDayResponse
	if err := json.Unmarshal(respBody, &closeResp); err != nil {
		return nil, fmt.Errorf("failed to decode FedEx close response: %w", err)
	}

	result := &ManifestResponse{
		Carrier:  req.Carrier,
		Date:     closeDate,
		Status:   "closed",
		Warnings: []string{},
	}

	if len(closeResp.Output.CloseDocuments) == 0 {
		result.Warnings = append(result.Warnings,
			"FedEx returned no close documents — no open Ground shipments for "+closeDate)
		a.log.Warn("FedEx end-of-day close returned no documents",
			zap.String("closeDate", closeDate))
		return result, nil
	}

	// Prefer MANIFEST type; fall back to first available document.
	for _, doc := range closeResp.Output.CloseDocuments {
		if doc.Type == "MANIFEST" && len(doc.Parts) > 0 {
			result.ManifestDocument = doc.Parts[0].Image
			result.ManifestDocumentFormat = "PDF"
			break
		}
	}
	if result.ManifestDocument == "" && len(closeResp.Output.CloseDocuments[0].Parts) > 0 {
		result.ManifestDocument = closeResp.Output.CloseDocuments[0].Parts[0].Image
		result.ManifestDocumentFormat = "PDF"
	}

	a.log.Info("FedEx Ground end-of-day close completed",
		zap.String("closeDate", closeDate),
		zap.Bool("hasDocument", result.ManifestDocument != ""))

	return result, nil
}

// GetPickupAvailability checks FedEx pickup availability via POST /pickup/v1/pickups/availabilities.
//
// Returns available collection slots as PickupSlot values. Each slot covers
// one (readyTime, latestTime) pair for a single pickup date. Unavailable options
// are filtered out. When the carrier returns no fine-grained time windows, a
// single slot ending at the cut-off time is synthesised.
func (a *FedExAdapter) GetPickupAvailability(ctx context.Context, req PickupAvailabilityRequest) (*PickupAvailabilityResponse, error) {
	avReq := fedexPickupAvailabilityRequest{
		PickupAddress: fedexAvailabilityAddress{
			PostalCode:  req.Address.PostalCode,
			CountryCode: req.Address.Country,
		},
		Carriers:            []string{"FDXE"},
		CountryRelationship: "DOMESTIC",
		PickupRequestType:   []string{"SAME_DAY", "FUTURE_DAY"},
	}

	body, err := json.Marshal(avReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal FedEx pickup availability request: %w", err)
	}

	httpReq, err := a.newFedExRequest(ctx, http.MethodPost, "/pickup/v1/pickups/availabilities", body)
	if err != nil {
		return nil, fmt.Errorf("fedex: create pickup availability request: %w", err)
	}

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("FedEx pickup availability request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read FedEx pickup availability response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("FedEx pickup availability API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var avResp fedexPickupAvailabilityResponse
	if err := json.Unmarshal(respBody, &avResp); err != nil {
		return nil, fmt.Errorf("failed to decode FedEx pickup availability response: %w", err)
	}

	slots := make([]PickupSlot, 0, len(avResp.Output.Options))
	for _, opt := range avResp.Output.Options {
		if !opt.Available {
			continue
		}
		if len(opt.ReadyTimeOptions) > 0 && len(opt.LatestTimeOptions) > 0 {
			// Pair each ready time with its corresponding latest time.
			// When the slices differ in length, the shorter one is the limit.
			n := len(opt.ReadyTimeOptions)
			if len(opt.LatestTimeOptions) < n {
				n = len(opt.LatestTimeOptions)
			}
			for i := range n {
				slots = append(slots, PickupSlot{
					Date:      opt.PickupDate,
					StartTime: fedexTrimSeconds(opt.ReadyTimeOptions[i]),
					EndTime:   fedexTrimSeconds(opt.LatestTimeOptions[i]),
				})
			}
		} else {
			// Fallback: single slot ending at the cut-off time.
			slots = append(slots, PickupSlot{
				Date:      opt.PickupDate,
				StartTime: "09:00",
				EndTime:   fedexTrimSeconds(opt.CutOffTime),
			})
		}
	}

	return &PickupAvailabilityResponse{
		Carrier: "fedex",
		Slots:   slots,
	}, nil
}

// fedexTrimSeconds strips the trailing ":SS" from an "HH:MM:SS" time string.
// It is a no-op for strings that are not exactly 8 characters.
func fedexTrimSeconds(t string) string {
	if len(t) == 8 && t[5] == ':' {
		return t[:5]
	}
	return t
}
