// Package adapter provides the DPD NL implementation of CarrierAdapter.
// This file is located at /internal/adapter/dpd_nl.go.
package adapter

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// DPD NL SOAP service endpoints (live environment).
//
// TODO: Confirm these against the soap:address location in each live WSDL:
//
//	https://wsshipper.dpd.nl/soap/WSDL/LoginServiceV21.wsdl
//	https://wsshipper.dpd.nl/soap/WSDL/ShipmentServiceV35.wsdl
//	https://wsshipper.dpd.nl/soap/WSDL/ParcelLifecycleServiceV20.wsdl
const (
	dpdNLDefaultLoginURL    = "https://wsshipper.dpd.nl/soap/services/LoginService"
	dpdNLDefaultShipmentURL = "https://wsshipper.dpd.nl/soap/services/ShipmentService"
	dpdNLDefaultTrackingURL = "https://wsshipper.dpd.nl/soap/services/ParcelLifecycleService"
)

// dpdNLTokenState holds the cached 24-hour auth token returned by LoginService.
type dpdNLTokenState struct {
	mu        sync.Mutex
	authToken string
	depot     string // sending depot returned alongside the token
	expiresAt time.Time
}

// valid reports whether the cached token can still be used.
// A 60-second margin guards against clock skew between caller and DPD servers.
func (s *dpdNLTokenState) valid() bool {
	return s.authToken != "" && time.Now().Before(s.expiresAt.Add(-60*time.Second))
}

// dpdNLLabel is a label entry stored in the in-process label cache.
type dpdNLLabel struct {
	data     string      // base64-encoded label bytes
	format   LabelFormat // PDF or PNG
	mimeType string
}

// DPDNLAdapter implements CarrierAdapter for the DPD Netherlands SOAP API
// (ShipmentService v3.5, ParcelLifecycleService v2.0, LoginService v2.1).
//
// Auth: LoginService issues a 24-hour token that must be cached. The adapter
// refreshes it automatically on expiry or when LOGIN_5/LOGIN_6 faults arrive.
//
// Label retrieval: DPD NL has no separate fetch-label endpoint; labels are
// returned inline at booking time. FetchLabel serves them from an in-process
// cache keyed by tracking number. Labels survive only for the lifetime of the
// process — bookings made in a previous process cannot be re-fetched.
//
// Concurrency: ShipmentService requires strictly sequential calls per account.
// A mutex serialises all storeOrders calls.
//
// Unsupported: CancelShipment and UpdateShipment return ErrNotSupported.
// DPD NL provides no programmatic cancel or update endpoint.
type DPDNLAdapter struct {
	delisID  string
	password string

	// Endpoint URLs — set by constructor, overridable in tests.
	LoginURL    string
	ShipmentURL string
	TrackingURL string

	tokenState dpdNLTokenState
	shipmentMu sync.Mutex // serialises storeOrders per API contract
	labelCache sync.Map   // map[string]dpdNLLabel, key = tracking number
	HTTPClient *http.Client
	log        *zap.Logger
}

// NewDPDNLAdapter creates a production DPDNLAdapter.
// delisID and password are the credentials issued by DPD NL.
func NewDPDNLAdapter(delisID, password string, log *zap.Logger) *DPDNLAdapter {
	return &DPDNLAdapter{
		delisID:     delisID,
		password:    password,
		LoginURL:    dpdNLDefaultLoginURL,
		ShipmentURL: dpdNLDefaultShipmentURL,
		TrackingURL: dpdNLDefaultTrackingURL,
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
		log:         log,
	}
}

// authToken returns a valid auth token, refreshing via LoginService when needed.
// It is safe for concurrent use; at most one refresh happens at a time.
func (a *DPDNLAdapter) authToken(ctx context.Context) (token, depot string, err error) {
	a.tokenState.mu.Lock()
	defer a.tokenState.mu.Unlock()

	if a.tokenState.valid() {
		return a.tokenState.authToken, a.tokenState.depot, nil
	}
	return a.refreshToken(ctx)
}

// refreshToken calls LoginService and updates the cached token.
// Must be called with tokenState.mu held.
func (a *DPDNLAdapter) refreshToken(ctx context.Context) (token, depot string, err error) {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope
    xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"
    xmlns:ns="http://dpd.com/common/service/types/LoginService/2.0">
  <soapenv:Header/>
  <soapenv:Body>
    <ns:getAuth>
      <delisId>%s</delisId>
      <password>%s</password>
      <messageLanguage>en_EN</messageLanguage>
    </ns:getAuth>
  </soapenv:Body>
</soapenv:Envelope>`, a.delisID, a.password)

	raw, err := a.soapPost(ctx, a.LoginURL, "login", body)
	if err != nil {
		return "", "", fmt.Errorf("dpd_nl: login: %w", err)
	}

	var env struct {
		Body struct {
			Response struct {
				Return struct {
					AuthToken        string `xml:"authToken"`
					Depot            string `xml:"depot"`
					AuthTokenExpires string `xml:"authTokenExpires"`
				} `xml:"return"`
			} `xml:"getAuthResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(raw, &env); err != nil {
		return "", "", fmt.Errorf("dpd_nl: parse login response: %w", err)
	}

	ret := env.Body.Response.Return
	if ret.AuthToken == "" {
		return "", "", fmt.Errorf("dpd_nl: login returned empty token")
	}

	// authTokenExpires format: "2020-05-08T13:02:56.06"
	expires, err := time.Parse("2006-01-02T15:04:05.99", ret.AuthTokenExpires)
	if err != nil {
		// Fall back to 24 h from now if parsing fails — conservative but safe.
		expires = time.Now().Add(24 * time.Hour)
		a.log.Warn("dpd_nl: could not parse authTokenExpires, using 24h fallback",
			zap.String("raw", ret.AuthTokenExpires),
		)
	}

	a.tokenState.authToken = ret.AuthToken
	a.tokenState.depot = ret.Depot
	a.tokenState.expiresAt = expires

	a.log.Info("dpd_nl: auth token refreshed",
		zap.String("depot", ret.Depot),
		zap.Time("expiresAt", expires),
	)
	return ret.AuthToken, ret.Depot, nil
}

// soapPost performs a SOAP HTTP POST and returns the raw response bytes.
// HTTP 500 responses are parsed for a SOAP Fault and returned as an error.
func (a *DPDNLAdapter) soapPost(ctx context.Context, url, action, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", `"`+action+`"`)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode == http.StatusInternalServerError {
		return nil, fmt.Errorf("soap fault: %s", parseSoapFault(raw))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}

// parseSoapFault extracts the faultstring from a SOAP 1.1 Fault envelope.
// Returns the raw body string if parsing fails.
func parseSoapFault(raw []byte) string {
	var env struct {
		Body struct {
			Fault struct {
				FaultString string `xml:"faultstring"`
			} `xml:"Fault"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(raw, &env); err != nil {
		return string(raw)
	}
	if env.Body.Fault.FaultString != "" {
		return env.Body.Fault.FaultString
	}
	return string(raw)
}

// isTokenFault reports whether a SOAP fault string indicates an expired/invalid token.
func isTokenFault(msg string) bool {
	return strings.Contains(msg, "LOGIN_5") || strings.Contains(msg, "LOGIN_6")
}

// BookShipment creates a DPD NL shipment via ShipmentService storeOrders and
// returns the 14-digit parcel label number as the tracking number.
//
// The inline PDF label returned by storeOrders is stored in the label cache so
// FetchLabel can serve it without a second network call.
//
// Product selection:
//   - DeliveryType "servicepoint" → PSD (parcel shop delivery)
//   - DeliveryType "return"       → B2C with returns=true per parcel
//   - DeliveryType "home"         → B2C
//   - default                     → B2B
//
// Multi-colli (MPS) is supported; one <parcels> element is emitted per colli.
// The <international> block is nested inside each <parcels> element when
// Customs data is present, per the ShipmentService v3.5 specification.
func (a *DPDNLAdapter) BookShipment(ctx context.Context, req BookingRequest) (*BookingResponse, error) {
	if len(req.Shipment.Colli) == 0 {
		return nil, fmt.Errorf("dpd_nl: shipment must contain at least one colli")
	}

	// Acquire shipment lock — DPD NL requires sequential calls per account.
	a.shipmentMu.Lock()
	defer a.shipmentMu.Unlock()

	token, depot, err := a.authToken(ctx)
	if err != nil {
		return nil, err
	}

	raw, err := a.storeOrders(ctx, token, depot, req)
	if err != nil {
		// On token faults, refresh once and retry.
		if isTokenFault(err.Error()) {
			a.log.Info("dpd_nl: token fault on storeOrders, refreshing and retrying")
			a.tokenState.mu.Lock()
			token, depot, err = a.refreshToken(ctx)
			a.tokenState.mu.Unlock()
			if err != nil {
				return nil, err
			}
			raw, err = a.storeOrders(ctx, token, depot, req)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return a.parseBookingResponse(req, raw)
}

// storeOrders builds and sends the storeOrders SOAP call.
func (a *DPDNLAdapter) storeOrders(ctx context.Context, token, depot string, req BookingRequest) ([]byte, error) {
	product := dpdNLProduct(req.Shipment.DeliveryType)
	isReturn := req.Shipment.DeliveryType == "return"
	isParcelShop := req.Shipment.DeliveryType == "servicepoint"

	// Predict (delivery notification) — sent when the receiver has an email address.
	predict := ""
	if email := req.Shipment.Receiver.Email; email != "" {
		predict = fmt.Sprintf(`
      <predict>
        <channel>1</channel>
        <value>%s</value>
        <language>NL</language>
      </predict>`, email)
	}

	// Parcel shop delivery section.
	parcelShopSection := ""
	if isParcelShop && req.Shipment.Receiver.ServicePointID != "" {
		notifyEmail := req.Shipment.Receiver.Email
		if notifyEmail == "" {
			notifyEmail = "noreply@example.com"
		}
		parcelShopSection = fmt.Sprintf(`
      <parcelShopDelivery>
        <parcelShopId>%s</parcelShopId>
        <parcelShopNotification>
          <channel>1</channel>
          <value>%s</value>
          <language>NL</language>
        </parcelShopNotification>
      </parcelShopDelivery>`, req.Shipment.Receiver.ServicePointID, notifyEmail)
	}

	// Build the <international> block once if customs data is present.
	// It is nested inside each <parcels> element per the API spec.
	internationalBlock := ""
	if req.Shipment.Customs.CustomsValue > 0 && len(req.Shipment.Customs.Items) > 0 {
		internationalBlock = buildDPDNLInternational(req.Shipment.Customs)
	}

	// Build <parcels> elements — one per colli.
	var parcelsXML strings.Builder
	for _, c := range req.Shipment.Colli {
		weightDg := kgToDecagrams(c.Weight)
		ref := ""
		if c.Reference != "" {
			ref = fmt.Sprintf("<customerReferenceNumber1>%s</customerReferenceNumber1>", xmlEscape(c.Reference))
		}
		returnTag := ""
		if isReturn {
			returnTag = "<returns>true</returns>"
		}
		fmt.Fprintf(&parcelsXML, `
    <parcels>
      %s
      <weight>%d</weight>
      %s
      %s
    </parcels>`, ref, weightDg, returnTag, internationalBlock)
	}

	// Label format: PDF (default) or QR PNG via dropOffType.
	labelFormat := "PDF"
	dropOffType := ""
	if req.LabelFormat == LabelFormatPNG {
		dropOffType = "<dropOffType>QR_CODE</dropOffType>"
	}

	senderContent := buildDPDNLAddressContent(req.Shipment.Sender)
	recipientContent := buildDPDNLAddressContent(req.Shipment.Receiver)

	envelope := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope
    xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"
    xmlns:ns="http://dpd.com/common/service/types/Authentication/2.0"
    xmlns:ns1="http://dpd.com/common/service/types/ShipmentService/3.5">
  <soapenv:Header>
    <ns:authentication>
      <delisId>%s</delisId>
      <authToken>%s</authToken>
      <messageLanguage>en_EN</messageLanguage>
    </ns:authentication>
  </soapenv:Header>
  <soapenv:Body>
    <ns1:storeOrders>
      <printOptions>
        <printerLanguage>%s</printerLanguage>
        <paperFormat>A6</paperFormat>
        %s
      </printOptions>
      <order>
        <generalShipmentData>
          <sendingDepot>%s</sendingDepot>
          <product>%s</product>
          <sender>
            %s
          </sender>
          <recipient>
            %s
          </recipient>
        </generalShipmentData>
        %s
        <productAndServiceData>
          <orderType>consignment</orderType>
          %s
          %s
        </productAndServiceData>
      </order>
    </ns1:storeOrders>
  </soapenv:Body>
</soapenv:Envelope>`,
		a.delisID, token,
		labelFormat, dropOffType,
		depot, product,
		senderContent,
		recipientContent,
		parcelsXML.String(),
		parcelShopSection,
		predict,
	)

	raw, err := a.soapPost(ctx, a.ShipmentURL, "storeOrders", envelope)
	if err != nil {
		return nil, fmt.Errorf("dpd_nl: storeOrders: %w", err)
	}
	return raw, nil
}

// parseBookingResponse decodes the storeOrders response and builds a BookingResponse.
func (a *DPDNLAdapter) parseBookingResponse(req BookingRequest, raw []byte) (*BookingResponse, error) {
	var env struct {
		Body struct {
			Response struct {
				OrderResult struct {
					LabelsPDF string `xml:"parcellabelsPDF"`
					LabelsPNG string `xml:"parcellabelsPNG_qr"`
					Responses []struct {
						MpsID  string `xml:"mpsId"`
						Parcel struct {
							LabelNumber string `xml:"parcelLabelNumber"`
						} `xml:"parcelInformation"`
					} `xml:"shipmentResponses"`
				} `xml:"orderResult"`
			} `xml:"storeOrdersResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("dpd_nl: parse storeOrders response: %w", err)
	}

	result := env.Body.Response.OrderResult
	if len(result.Responses) == 0 {
		return nil, fmt.Errorf("dpd_nl: storeOrders returned no shipment responses")
	}

	trackingNumber := result.Responses[0].Parcel.LabelNumber
	shipmentID := result.Responses[0].MpsID

	a.log.Info("dpd_nl: shipment booked",
		zap.String("trackingNumber", trackingNumber),
		zap.String("shipmentID", shipmentID),
	)

	// Cache label for FetchLabel — DPD NL has no standalone label endpoint.
	labelData := result.LabelsPDF
	labelFmt := LabelFormatPDF
	if req.LabelFormat == LabelFormatPNG && result.LabelsPNG != "" {
		labelData = result.LabelsPNG
		labelFmt = LabelFormatPNG
	}
	if labelData != "" && trackingNumber != "" {
		a.labelCache.Store(trackingNumber, dpdNLLabel{
			data:     labelData,
			format:   labelFmt,
			mimeType: MimeTypeForFormat(labelFmt),
		})
	}

	colliResp := make([]ColliResponse, len(req.Shipment.Colli))
	for i, c := range req.Shipment.Colli {
		pn := ""
		if i < len(result.Responses) {
			pn = result.Responses[i].Parcel.LabelNumber
		}
		colliResp[i] = ColliResponse{ID: c.ID, TrackingNumber: pn, Status: "booked"}
	}

	return &BookingResponse{
		ShipmentID:     shipmentID,
		TrackingNumber: trackingNumber,
		Carrier:        "dpd_nl",
		Status:         "booked",
		Colli:          colliResp,
		BetaWarning:    "dpd_nl adapter is in beta — validate in sandbox before production use",
	}, nil
}

// TrackShipment retrieves parcel status via ParcelLifecycleService getTrackingData.
//
// Tracking numbers must be exactly 14 numeric digits (DPD NL API requirement).
// The service is designed for single-parcel on-demand tracking only; do not use
// for bulk polling (max 60 req/min, 12,000/day per account).
//
// TODO: Verify the XML field names against the live WSDL schema at
// https://wsshipper.dpd.nl/soap/WSDL/ParcelLifecycleServiceV20.wsdl
func (a *DPDNLAdapter) TrackShipment(ctx context.Context, trackingNumber string) (*TrackingResponse, error) {
	if trackingNumber == "" {
		return nil, fmt.Errorf("dpd_nl: tracking number must not be empty")
	}

	token, _, err := a.authToken(ctx)
	if err != nil {
		return nil, err
	}

	raw, err := a.getTrackingData(ctx, token, trackingNumber)
	if err != nil {
		if isTokenFault(err.Error()) {
			a.log.Info("dpd_nl: token fault on getTrackingData, refreshing and retrying")
			a.tokenState.mu.Lock()
			token, _, err = a.refreshToken(ctx)
			a.tokenState.mu.Unlock()
			if err != nil {
				return nil, err
			}
			raw, err = a.getTrackingData(ctx, token, trackingNumber)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return a.parseTrackingResponse(trackingNumber, raw)
}

// getTrackingData sends the getTrackingData SOAP request.
func (a *DPDNLAdapter) getTrackingData(ctx context.Context, token, trackingNumber string) ([]byte, error) {
	envelope := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope
    xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"
    xmlns:ns="http://dpd.com/common/service/types/Authentication/2.0"
    xmlns:ns1="http://dpd.com/common/service/types/ParcelLifecycleService/2.0">
  <soapenv:Header>
    <ns:authentication>
      <delisId>%s</delisId>
      <authToken>%s</authToken>
      <messageLanguage>en_EN</messageLanguage>
    </ns:authentication>
  </soapenv:Header>
  <soapenv:Body>
    <ns1:getTrackingData>
      <parcelLabelNumber>%s</parcelLabelNumber>
    </ns1:getTrackingData>
  </soapenv:Body>
</soapenv:Envelope>`, a.delisID, token, trackingNumber)

	raw, err := a.soapPost(ctx, a.TrackingURL, "getTrackingData", envelope)
	if err != nil {
		return nil, fmt.Errorf("dpd_nl: getTrackingData: %w", err)
	}
	return raw, nil
}

// parseTrackingResponse decodes a ParcelLifecycleService response.
//
// The schema follows the DPD NL ParcelLifecycleService v2.0 structure.
// TODO: Cross-check field names against the live WSDL schema.
func (a *DPDNLAdapter) parseTrackingResponse(trackingNumber string, raw []byte) (*TrackingResponse, error) {
	var env struct {
		Body struct {
			Response struct {
				Data struct {
					StatusInfo struct {
						Status   string `xml:"status"`
						Date     string `xml:"date"`
						Location string `xml:"depotCity"`
					} `xml:"statusInfo"`
					Events []struct {
						Description string `xml:"description"`
						Date        string `xml:"eventDate"`
						Time        string `xml:"eventTime"`
						Location    string `xml:"depotCity"`
						Code        struct {
							EventCode string `xml:"eventCode"`
						} `xml:"parcelEventCode"`
					} `xml:"parcelEvent"`
				} `xml:"parcelLifeCycleData"`
			} `xml:"getTrackingDataResponse"`
		} `xml:"Body"`
	}
	if err := xml.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("dpd_nl: parse tracking response: %w", err)
	}

	data := env.Body.Response.Data
	rawStatus := data.StatusInfo.Status
	norm := normalizeDPDNLStatus(rawStatus)

	events := make([]TrackingEvent, 0, len(data.Events))
	for _, e := range data.Events {
		ts := e.Date
		if e.Time != "" {
			ts = e.Date + "T" + e.Time
		}
		events = append(events, TrackingEvent{
			Timestamp:        ts,
			Status:           e.Code.EventCode,
			NormalizedStatus: normalizeDPDNLStatus(e.Code.EventCode),
			Location:         e.Location,
			Details:          e.Description,
		})
	}

	return &TrackingResponse{
		TrackingNumber:   trackingNumber,
		Carrier:          "dpd_nl",
		Status:           rawStatus,
		NormalizedStatus: norm,
		OriginalStatus:   rawStatus,
		Events:           events,
	}, nil
}

// FetchLabel returns the label cached at booking time.
//
// DPD NL provides no standalone label fetch endpoint; labels are returned
// inline in the storeOrders response and stored here at that time.
// If the label was booked in a previous process the cache is empty and an
// error is returned directing the caller to rebook.
func (a *DPDNLAdapter) FetchLabel(_ context.Context, req LabelRequest) (*LabelResponse, error) {
	switch req.Format {
	case LabelFormatPDF, LabelFormatPNG:
	default:
		return nil, unsupportedFormat("DPD NL", req.Format, LabelFormatPDF, LabelFormatPNG)
	}
	if req.TrackingNumber == "" {
		return nil, fmt.Errorf("dpd_nl: tracking number must not be empty")
	}

	val, ok := a.labelCache.Load(req.TrackingNumber)
	if !ok {
		return nil, fmt.Errorf(
			"dpd_nl: label for %s not in cache — DPD NL does not expose a fetch-label endpoint; "+
				"the label must be captured at booking time",
			req.TrackingNumber,
		)
	}
	entry := val.(dpdNLLabel)

	if req.Format != entry.format {
		return nil, fmt.Errorf(
			"dpd_nl: label for %s was cached as %s; requested %s — rebook with the desired format",
			req.TrackingNumber, entry.format, req.Format,
		)
	}

	return &LabelResponse{
		TrackingNumber: req.TrackingNumber,
		Carrier:        "dpd_nl",
		Format:         entry.format,
		Data:           entry.data,
		MimeType:       entry.mimeType,
	}, nil
}

// CancelShipment is not supported by DPD NL.
// Contact DPD customer service to cancel before 22:00 on the booking day.
func (a *DPDNLAdapter) CancelShipment(_ context.Context, _ string) (*CancelResponse, error) {
	return nil, notSupported("DPD NL", "cancellation", "contact DPD customer service before 22:00 on the booking day")
}

// UpdateShipment is not supported by DPD NL.
func (a *DPDNLAdapter) UpdateShipment(_ context.Context, _ UpdateRequest) (*UpdateResponse, error) {
	return nil, notSupported("DPD NL", "post-booking update", "cancel and rebook")
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// buildDPDNLAddressContent renders the inner XML elements of a DPD NL address block.
// The caller wraps the result in a <sender> or <recipient> element.
func buildDPDNLAddressContent(a Address) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<name1>%s</name1>\n", xmlEscape(a.Name))
	fmt.Fprintf(&b, "<street>%s</street>\n", xmlEscape(a.Street))
	if a.HouseNumber != "" {
		fmt.Fprintf(&b, "<houseNo>%s</houseNo>\n", xmlEscape(a.HouseNumber))
	}
	if a.Supplement != "" {
		fmt.Fprintf(&b, "<street2>%s</street2>\n", xmlEscape(a.Supplement))
	}
	fmt.Fprintf(&b, "<country>%s</country>\n", xmlEscape(a.Country))
	fmt.Fprintf(&b, "<zipCode>%s</zipCode>\n", xmlEscape(strings.ReplaceAll(a.PostalCode, " ", "")))
	fmt.Fprintf(&b, "<city>%s</city>\n", xmlEscape(a.City))
	if a.Phone != "" {
		fmt.Fprintf(&b, "<phone>%s</phone>\n", xmlEscape(a.Phone))
	}
	if a.Email != "" {
		fmt.Fprintf(&b, "<email>%s</email>\n", xmlEscape(a.Email))
	}
	return b.String()
}

// buildDPDNLInternational renders the <international> customs block.
// This block must be nested inside each <parcels> element for non-EU shipments.
// Only called when Customs.CustomsValue > 0 and there is at least one Item.
func buildDPDNLInternational(c Customs) string {
	amountCents := int64(math.Round(c.CustomsValue * 100))
	currency := c.CustomsCurrency
	if currency == "" {
		currency = "EUR"
	}
	// Default to DAP (06) — the most common incoterms for non-EU parcel shipping.
	incoterms := "06"
	if c.Incoterms != "" {
		incoterms = c.Incoterms
	}
	invoiceDate := strings.ReplaceAll(c.InvoiceDate, "-", "")

	var lines strings.Builder
	for _, item := range c.Items {
		lineValue := int64(math.Round(item.Value * float64(item.Quantity) * 100))
		weightDg := kgToDecagrams(item.NetWeight * float64(item.Quantity))
		hsCode := item.HSCode
		if hsCode == "" {
			hsCode = c.HSCode
		}
		fmt.Fprintf(&lines, `
      <commercialInvoiceLine>
        <customsTarif>%s</customsTarif>
        <receiverCustomsTarif>%s</receiverCustomsTarif>
        <content>%s</content>
        <grossWeight>%d</grossWeight>
        <itemsNumber>%d</itemsNumber>
        <amountLine>%d</amountLine>
        <customsOrigin>%s</customsOrigin>
      </commercialInvoiceLine>`,
			xmlEscape(hsCode), xmlEscape(hsCode),
			xmlEscape(item.Description),
			weightDg, item.Quantity, lineValue,
			xmlEscape(item.CountryOfOrigin),
		)
	}

	return fmt.Sprintf(`
      <international>
        <parcelType>false</parcelType>
        <customsAmount>%d</customsAmount>
        <customsCurrency>%s</customsCurrency>
        <customsAmountEx>%d</customsAmountEx>
        <customsCurrencyEx>%s</customsCurrencyEx>
        <clearanceCleared>N</clearanceCleared>
        <prealertStatus>S03</prealertStatus>
        <exportReason>01</exportReason>
        <customsTerms>%s</customsTerms>
        <customsContent>%s</customsContent>
        <customsInvoice>%s</customsInvoice>
        <customsInvoiceDate>%s</customsInvoiceDate>
        %s
      </international>`,
		amountCents, currency, amountCents, currency,
		incoterms,
		xmlEscape(c.HSCode),
		xmlEscape(c.InvoiceNumber), invoiceDate,
		lines.String(),
	)
}

// dpdNLProduct maps a DeliveryType string to the DPD NL product code.
func dpdNLProduct(deliveryType string) string {
	switch deliveryType {
	case "servicepoint":
		return "PSD"
	case "home", "return":
		return "B2C"
	default:
		return "B2B"
	}
}

// kgToDecagrams converts a weight in kg to DPD NL's decagram unit (1 kg = 100 dg).
// The value is rounded up to avoid under-declaring weight.
func kgToDecagrams(kg float64) int {
	return int(math.Ceil(kg * 100))
}

// xmlEscape escapes the five XML special characters for safe embedding in SOAP XML.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

// normalizeDPDNLStatus maps a DPD NL ParcelLifecycleService status string to
// the gateway's carrier-agnostic TrackingStatus.
func normalizeDPDNLStatus(raw string) TrackingStatus {
	m, ok := normalizedStatuses["dpd_nl"]
	if !ok {
		return StatusUnknown
	}
	if s, found := m[raw]; found {
		return s
	}
	return StatusUnknown
}
