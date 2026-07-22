// Package adapter provides carrier-specific customs declaration implementations.
// This file is located at /internal/adapter/customs.go.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// CustomsSubmitter submits a pre-validated customs declaration to a carrier API.
// It is defined here at the consumer side; each carrier adapter satisfies it.
// Callers should validate the Customs block with validation.ValidateCustoms before
// calling SubmitCustoms — this interface does not re-validate.
type CustomsSubmitter interface {
	// SubmitCustoms sends the customs declaration for a booked shipment.
	// TrackingNumber in req must correspond to a shipment already booked with
	// the same carrier.
	SubmitCustoms(ctx context.Context, req CustomsRequest) (*CustomsResponse, error)
}

// CustomsRequest carries all data needed to submit a customs declaration to a
// carrier. It is carrier-agnostic; each adapter maps it to its own wire format.
type CustomsRequest struct {
	// TrackingNumber is the carrier tracking number returned from BookShipment.
	TrackingNumber string
	// EDIItemID is the carrier's internal item identifier required by some APIs
	// (e.g. PostNord) to associate the customs declaration with the booked parcel.
	EDIItemID string
	// OriginCountry is the ISO 3166-1 alpha-2 sender country code.
	OriginCountry string
	// DestinationCountry is the ISO 3166-1 alpha-2 receiver country code.
	DestinationCountry string
	// Customs holds the declaration data including line items.
	Customs Customs
	// Sender and Receiver provide party information for customs declaration forms.
	Sender   Address
	Receiver Address
}

// CustomsResponse is returned after a successful customs declaration submission.
type CustomsResponse struct {
	// Carrier identifies which carrier handled the declaration.
	Carrier string `json:"carrier"`
	// DeclarationID is the carrier-assigned customs declaration reference.
	DeclarationID string `json:"declarationId,omitempty"`
	// Status is a carrier-specific status string, e.g. "submitted", "draft".
	Status string `json:"status"`
	// Warnings lists non-fatal issues noted by the carrier (e.g. missing optional fields).
	Warnings []string `json:"warnings,omitempty"`
}

// ─── Bring ───────────────────────────────────────────────────────────────────

// SubmitCustoms is not available as a standalone API call for Bring.
// Bring embeds customs data directly in the booking request payload under
// product.customsInformation. Pass customs data via BookingRequest.Shipment.Customs
// when calling BookShipment — the adapter will embed it automatically.
//
// Callers should NOT add BringAdapter to any CustomsSubmitter dispatch loop.
// This method exists solely to surface a clear error if it is called accidentally.
func (a *BringAdapter) SubmitCustoms(_ context.Context, _ CustomsRequest) (*CustomsResponse, error) {
	return nil, fmt.Errorf("bring: customs must be provided at booking time via Shipment.Customs " +
		"— Bring has no separate customs submission endpoint; " +
		"pass customs data to BookShipment instead")
}

// ─── DHL ─────────────────────────────────────────────────────────────────────

// SubmitCustoms submits a customs declaration to the DHL eConnect cCustoms API
// (POST /ccc/send-cCustoms). Only DDP and DAP are accepted by DHL; other
// Incoterms are mapped to DAP. Maximum 99 items per consignment.
func (a *DHLAdapter) SubmitCustoms(ctx context.Context, req CustomsRequest) (*CustomsResponse, error) {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("dhl customs: obtain bearer token: %w", err)
	}

	payload := a.buildCCustomsRequest(req)

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("dhl customs: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.BookingBaseURL+"/ccc/send-cCustoms", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("dhl customs: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("dhl customs: http request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dhl customs: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dhl customs: carrier returned %d: %s", resp.StatusCode, string(respBody))
	}

	a.log.Info("dhl customs declaration submitted",
		zap.String("trackingNumber", req.TrackingNumber),
		zap.Int("statusCode", resp.StatusCode),
	)

	return &CustomsResponse{
		Carrier: "dhl",
		Status:  "submitted",
	}, nil
}

// dhlCustomsPayload is the top-level wrapper for the DHL cCustoms API request.
type dhlCustomsPayload struct {
	DataElement dhlCustomsDataElement `json:"dataElement"`
}

type dhlCustomsDataElement struct {
	ParcelOriginOrganization      string            `json:"parcelOriginOrganization"`
	ParcelDestinationOrganization string            `json:"parcelDestinationOrganization"`
	General                       dhlCustomsGeneral `json:"general"`
	CCustoms                      dhlCCustoms       `json:"cCustoms"`
}

type dhlCustomsGeneral struct {
	ParcelIdentifier       string `json:"parcelIdentifier"`
	Timestamp              string `json:"timestamp"`
	CustomerIdentification string `json:"customerIdentification"`
}

type dhlCCustoms struct {
	CustomsIDs       dhlCustomsIDs       `json:"CustomsIDs"`
	GoodsDescription dhlGoodsDescription `json:"goodsDescription"`
}

type dhlCustomsIDs struct {
	Sender    dhlCustomsParty `json:"sender"`
	Recipient dhlCustomsParty `json:"recipient"`
}

// dhlCustomsParty represents a VAT/EORI party in the DHL cCustoms request.
type dhlCustomsParty struct {
	// IDType is one of: VAT, EORI, inboundVAT, IOSS.
	IDType     string `json:"idType"`
	Identifier string `json:"identifier"`
}

type dhlGoodsDescription struct {
	General dhlGoodsGeneral `json:"general"`
	Items   []dhlGoodsItem  `json:"item"`
}

type dhlGoodsGeneral struct {
	// Incoterms accepts DDP or DAP only.
	Incoterms           string `json:"incoterms"`
	GoodsClassification string `json:"goodsClassification"`
	TotalValue          string `json:"totalValue"`
	Currency            string `json:"currency"`
	ShipmentWeight      string `json:"shipmentWeight"`
}

type dhlGoodsItem struct {
	Description         string `json:"description"`
	CustomsTariffNumber string `json:"customsTariffNumber"`
	OriginCountry       string `json:"originCountry"`
	Quantity            string `json:"quantity"`
	NetWeight           string `json:"netWeight"`
	Value               string `json:"value"`
	Currency            string `json:"currency"`
}

// dhlIncoterms maps standard Incoterms to DHL-accepted values.
// DHL cCustoms only supports DDP and DAP; all others default to DAP.
func dhlIncoterms(incoterms string) string {
	switch incoterms {
	case "DDP":
		return "DDP"
	case "DAP":
		return "DAP"
	default:
		return "DAP"
	}
}

// buildCCustomsRequest constructs the DHL cCustoms wire payload from a CustomsRequest.
func (a *DHLAdapter) buildCCustomsRequest(req CustomsRequest) dhlCustomsPayload {
	senderIDType := "VAT"
	if req.Customs.ExporterVATNumber == "" {
		senderIDType = "EORI"
	}
	recipientIDType := "VAT"
	if req.Customs.ImporterVATNumber == "" {
		recipientIDType = "EORI"
	}

	items := make([]dhlGoodsItem, 0, len(req.Customs.Items))
	for _, ci := range req.Customs.Items {
		items = append(items, dhlGoodsItem{
			Description:         ci.Description,
			CustomsTariffNumber: ci.HSCode,
			OriginCountry:       ci.CountryOfOrigin,
			Quantity:            fmt.Sprintf("%d", ci.Quantity),
			NetWeight:           fmt.Sprintf("%.3f", ci.NetWeight),
			Value:               fmt.Sprintf("%.2f", ci.Value),
			Currency:            ci.Currency,
		})
	}

	return dhlCustomsPayload{
		DataElement: dhlCustomsDataElement{
			ParcelOriginOrganization:      req.OriginCountry,
			ParcelDestinationOrganization: req.DestinationCountry,
			General: dhlCustomsGeneral{
				ParcelIdentifier:       req.TrackingNumber,
				Timestamp:              time.Now().UTC().Format(time.RFC3339),
				CustomerIdentification: a.CustomerID,
			},
			CCustoms: dhlCCustoms{
				CustomsIDs: dhlCustomsIDs{
					Sender: dhlCustomsParty{
						IDType:     senderIDType,
						Identifier: req.Customs.ExporterVATNumber,
					},
					Recipient: dhlCustomsParty{
						IDType:     recipientIDType,
						Identifier: req.Customs.ImporterVATNumber,
					},
				},
				GoodsDescription: dhlGoodsDescription{
					General: dhlGoodsGeneral{
						Incoterms:           dhlIncoterms(req.Customs.Incoterms),
						GoodsClassification: "CommercialSaleOfGoods",
						TotalValue:          fmt.Sprintf("%.2f", req.Customs.CustomsValue),
						Currency:            req.Customs.CustomsCurrency,
						ShipmentWeight:      "0.000", // populated from shipment weight at call site if needed
					},
					Items: items,
				},
			},
		},
	}
}

// ─── GLS ─────────────────────────────────────────────────────────────────────

// glsCustomsBaseURL is the GLS Customs API v3 production base URL.
const glsCustomsBaseURL = "https://api.gls-group.net/customs-management/export/public/v3"

// glsIncotermCode maps standard Incoterms 2020 codes to GLS-specific string codes.
// GLS Customs API v3 uses string values: "10"=DDP, "20"=DAP.
// Unmapped terms default to "20" (DAP).
func glsIncotermCode(incoterms string) string {
	switch incoterms {
	case "DDP":
		return "10"
	case "DAP":
		return "20"
	default:
		return "20"
	}
}

// glsCustomsWeight is the Weight schema in GLS Customs API v3.
// Unit is always "KGM".
type glsCustomsWeight struct {
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

// glsCustomsAmountOfMoney holds a monetary amount with ISO 4217 currency.
type glsCustomsAmountOfMoney struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// glsCustomsContactPerson is the ContactPerson schema in GLS Customs API v3.
type glsCustomsContactPerson struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"emailAddress,omitempty"`
	Phone string `json:"phoneNumber,omitempty"`
}

// glsCustomsAddress is the CustomsAddress schema in GLS Customs API v3.
// Field names differ from the ShipIT Address schema.
type glsCustomsAddress struct {
	Name1       string `json:"name1"`
	Street1     string `json:"street1"`
	HouseNumber string `json:"houseNumber,omitempty"`
	City1       string `json:"city1"`
	Postcode    string `json:"postcode"`
	CountryCode string `json:"countryCode"`
}

// glsCustomsPartyBody holds the address, contact, and identifiers for an
// exporter or importer in GLS Customs API v3.
type glsCustomsPartyBody struct {
	Address               glsCustomsAddress       `json:"address"`
	ContactPerson         glsCustomsContactPerson `json:"contactPerson"`
	IsCommercial          bool                    `json:"isCommercial"`
	VATRegistrationNumber string                  `json:"vatRegistrationNumber,omitempty"`
	EORINumber            string                  `json:"eoriNumber,omitempty"`
}

// glsCustomsQuantity is the Quantity schema in GLS Customs API v3.
// Unit is always "PCE" (pieces).
type glsCustomsQuantity struct {
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

// glsCustomsLineItem maps one CustomsItem to the GLS Customs API v3 lineItem schema.
type glsCustomsLineItem struct {
	Quantity               glsCustomsQuantity `json:"quantity"`
	CommodityCode          string             `json:"commodityCode"`
	GoodsDescription       string             `json:"goodsDescription"`
	CountryOfOrigin        string             `json:"countryOfOrigin"`
	ValueInInvoiceCurrency float64            `json:"valueInInvoiceCurrency"`
	GrossWeight            glsCustomsWeight   `json:"grossWeight"`
	NetWeight              glsCustomsWeight   `json:"netWeight"`
}

// glsCustomsInvoice is the invoice block required by GLS Customs API v3.
type glsCustomsInvoice struct {
	InvoiceNumber   string                  `json:"invoiceNumber"`
	InvoiceDate     string                  `json:"invoiceDate"` // format: "2006-01-02"
	TotalGoodsValue glsCustomsAmountOfMoney `json:"totalGoodsValue"`
}

// glsCustomsConsignment is the GLS Customs API v3 POST /customs-consignments body.
type glsCustomsConsignment struct {
	ParcelNumbers                []string             `json:"parcelNumbers"`
	CustomerReference            string               `json:"customerReference,omitempty"`
	GLSIncotermCode              string               `json:"glsIncotermCode"`
	IsExportDeclarationRequested bool                 `json:"isExportDeclarationRequested"`
	TotalGrossWeight             glsCustomsWeight     `json:"totalGrossWeight"`
	SaveAsDraft                  bool                 `json:"saveAsDraft,omitempty"`
	ExportDeclarationNumbers     []string             `json:"exportDeclarationNumbers,omitempty"`
	Exporter                     glsCustomsPartyBody  `json:"exporter"`
	Importer                     glsCustomsPartyBody  `json:"importer"`
	Invoice                      glsCustomsInvoice    `json:"invoice"`
	LineItems                    []glsCustomsLineItem `json:"lineItems"`
}

// glsCustomsParty builds a glsCustomsPartyBody from an Address and optional VAT/EORI numbers.
// isCommercial is true for B2B shipments or when a VAT number is present.
func glsCustomsParty(addr Address, vatNumber, eori string, isCommercial bool) glsCustomsPartyBody {
	return glsCustomsPartyBody{
		Address: glsCustomsAddress{
			Name1:       addr.Name,
			Street1:     addr.Street,
			HouseNumber: addr.HouseNumber,
			City1:       addr.City,
			Postcode:    addr.PostalCode,
			CountryCode: addr.Country,
		},
		ContactPerson: glsCustomsContactPerson{
			Name:  addr.Name,
			Email: addr.Email,
			Phone: addr.Phone,
		},
		IsCommercial:          isCommercial,
		VATRegistrationNumber: vatNumber,
		EORINumber:            eori,
	}
}

// glsTotalGrossWeight sums the net weights of all items as a gross weight approximation.
// If items is empty, falls back to the top-level customs value weight estimate.
func glsTotalGrossWeight(c Customs) glsCustomsWeight {
	var total float64
	for _, ci := range c.Items {
		total += ci.NetWeight * float64(ci.Quantity)
	}
	if total == 0 {
		// No item weights available — use a nominal 0.5 kg placeholder so the
		// required field is not zero. The caller should supply item weights.
		total = 0.5
	}
	return glsCustomsWeight{Amount: total, Unit: "KGM"}
}

// SubmitCustoms submits a customs consignment to the GLS Customs API v3
// (POST /customs-consignments). The GLS API validates the HS code server-side.
func (a *GLSAdapter) SubmitCustoms(ctx context.Context, req CustomsRequest) (*CustomsResponse, error) {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("gls customs: obtain bearer token: %w", err)
	}

	isB2B := req.Customs.ShipmentType == "B2B"
	invoiceRef := req.EDIItemID
	if invoiceRef == "" {
		invoiceRef = req.TrackingNumber
	}

	consignment := glsCustomsConsignment{
		ParcelNumbers:                []string{req.TrackingNumber},
		CustomerReference:            req.EDIItemID,
		GLSIncotermCode:              glsIncotermCode(req.Customs.Incoterms),
		IsExportDeclarationRequested: false,
		TotalGrossWeight:             glsTotalGrossWeight(req.Customs),
		Exporter:                     glsCustomsParty(req.Sender, req.Customs.ExporterVATNumber, "", isB2B || req.Customs.ExporterVATNumber != ""),
		Importer:                     glsCustomsParty(req.Receiver, req.Customs.ImporterVATNumber, "", isB2B),
		Invoice: glsCustomsInvoice{
			InvoiceNumber: invoiceRef,
			InvoiceDate:   time.Now().UTC().Format("2006-01-02"),
			TotalGoodsValue: glsCustomsAmountOfMoney{
				Amount:   req.Customs.CustomsValue,
				Currency: req.Customs.CustomsCurrency,
			},
		},
		LineItems: buildGLSLineItems(req.Customs),
	}

	body, err := json.Marshal(consignment)
	if err != nil {
		return nil, fmt.Errorf("gls customs: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		glsCustomsBaseURL+"/customs-consignments", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gls customs: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gls customs: http request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gls customs: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gls customs: carrier returned %d: %s", resp.StatusCode, string(respBody))
	}

	var glsResp struct {
		Key string `json:"key"` // unique key returned on 201 Created
	}
	if jsonErr := json.Unmarshal(respBody, &glsResp); jsonErr != nil {
		// Not JSON-parseable but status was 2xx — treat as success.
		a.log.Warn("gls customs: could not parse response body", zap.Error(jsonErr))
	}

	a.log.Info("gls customs declaration submitted",
		zap.String("trackingNumber", req.TrackingNumber),
		zap.String("consignmentKey", glsResp.Key),
	)

	return &CustomsResponse{
		Carrier:       "gls",
		DeclarationID: glsResp.Key,
		Status:        "submitted",
	}, nil
}

// buildGLSLineItems converts Customs.Items to GLS Customs API v3 line items.
// Falls back to a single line item from top-level Customs fields when Items is empty.
func buildGLSLineItems(c Customs) []glsCustomsLineItem {
	if len(c.Items) > 0 {
		items := make([]glsCustomsLineItem, 0, len(c.Items))
		for _, ci := range c.Items {
			items = append(items, glsCustomsLineItem{
				Quantity:               glsCustomsQuantity{Amount: float64(ci.Quantity), Unit: "PCE"},
				CommodityCode:          ci.HSCode,
				GoodsDescription:       ci.Description,
				CountryOfOrigin:        ci.CountryOfOrigin,
				ValueInInvoiceCurrency: ci.Value,
				GrossWeight:            glsCustomsWeight{Amount: ci.NetWeight, Unit: "KGM"},
				NetWeight:              glsCustomsWeight{Amount: ci.NetWeight, Unit: "KGM"},
			})
		}
		return items
	}

	// Single top-level fallback.
	return []glsCustomsLineItem{
		{
			Quantity:               glsCustomsQuantity{Amount: 1, Unit: "PCE"},
			CommodityCode:          c.HSCode,
			GoodsDescription:       c.HSCode, // no top-level description available
			CountryOfOrigin:        c.CountryOfOrigin,
			ValueInInvoiceCurrency: c.CustomsValue,
			GrossWeight:            glsCustomsWeight{Amount: 0.5, Unit: "KGM"},
			NetWeight:              glsCustomsWeight{Amount: 0.5, Unit: "KGM"},
		},
	}
}

// ─── PostNord ─────────────────────────────────────────────────────────────────

// postNordReasonForExportation maps shipment types to PostNord export reason codes.
// 1000=other, 4000=sale of goods, 4010=returned goods, 6110=gift, 6123=personal belongings.
func postNordReasonForExportation(shipmentType string) int {
	switch shipmentType {
	case "B2B", "B2C":
		return 4000 // sale of goods
	default:
		return 1000 // other
	}
}

// postNordCustomsDeclaration is the PostNord customs declaration request body.
// Endpoint: POST /rest/shipment/v3/customs/declaration.
type postNordCustomsDeclaration struct {
	// ItemID is the PostNord EDI item identifier from the prior booking.
	ItemID               string                `json:"itemId"`
	SellerVATNumber      string                `json:"sellerVatNumber,omitempty"`
	BuyerVATNumber       string                `json:"buyerVatNumber,omitempty"`
	TermsOfSale          string                `json:"termsOfSale,omitempty"`
	ReasonForExportation int                   `json:"reasonForExportation"`
	Items                []postNordCustomsItem `json:"items"`
}

// postNordCustomsItem is a single line item in a PostNord customs declaration.
// PostNord accepts at most 5 items (CN22/CN23 limit).
type postNordCustomsItem struct {
	HSTariffNumber  string  `json:"hsTariffNumber"`
	Quantity        int     `json:"quantity"`
	CountryOfOrigin string  `json:"countryOfOrigin"`
	NetWeight       float64 `json:"netWeight"`
	ItemValue       float64 `json:"itemValue"`
	Currency        string  `json:"currency"`
	CategoryOfItem  string  `json:"categoryOfItem,omitempty"`
}

// maxPostNordItems is the PostNord CN22/CN23 limit on customs line items.
const maxPostNordItems = 5

// SubmitCustoms submits a customs declaration to the PostNord shipment API
// (POST /rest/shipment/v3/customs/declaration). EDIItemID in req must contain
// the parcel item ID returned during booking; without it PostNord cannot
// associate the declaration with the shipment.
func (a *PostNordAdapter) SubmitCustoms(ctx context.Context, req CustomsRequest) (*CustomsResponse, error) {
	if req.EDIItemID == "" {
		return nil, fmt.Errorf("postnord customs: EDIItemID is required (parcel item ID from booking)")
	}

	items, warnings := buildPostNordItems(req.Customs)

	decl := postNordCustomsDeclaration{
		ItemID:               req.EDIItemID,
		SellerVATNumber:      req.Customs.ExporterVATNumber,
		BuyerVATNumber:       req.Customs.ImporterVATNumber,
		TermsOfSale:          req.Customs.Incoterms,
		ReasonForExportation: postNordReasonForExportation(req.Customs.ShipmentType),
		Items:                items,
	}

	body, err := json.Marshal(decl)
	if err != nil {
		return nil, fmt.Errorf("postnord customs: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/rest/shipment/v3/customs/declaration?apikey=%s",
		a.BaseURL, a.APIKey)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("postnord customs: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("postnord customs: http request: %w", sanitizeTransportError(err))
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("postnord customs: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("postnord customs: carrier returned %d: %s", resp.StatusCode, string(respBody))
	}

	a.log.Info("postnord customs declaration submitted",
		zap.String("itemID", req.EDIItemID),
		zap.Int("lineItems", len(items)),
	)

	return &CustomsResponse{
		Carrier:  "postnord",
		Status:   "submitted",
		Warnings: warnings,
	}, nil
}

// buildPostNordItems converts Customs.Items to PostNord customs items, capped at
// maxPostNordItems. A warning is appended if items were truncated.
func buildPostNordItems(c Customs) (items []postNordCustomsItem, warnings []string) {
	source := c.Items

	if len(source) == 0 && c.HSCode != "" {
		// Single top-level fallback.
		return []postNordCustomsItem{
			{
				HSTariffNumber:  c.HSCode,
				Quantity:        1,
				CountryOfOrigin: c.CountryOfOrigin,
				ItemValue:       c.CustomsValue,
				Currency:        c.CustomsCurrency,
			},
		}, nil
	}

	for i, ci := range source {
		if i >= maxPostNordItems {
			warnings = append(warnings,
				fmt.Sprintf("postnord customs: %d items truncated to %d (CN22/CN23 limit); submit remaining items manually",
					len(source), maxPostNordItems))
			break
		}
		currency := ci.Currency
		if currency == "" {
			currency = c.CustomsCurrency
		}
		items = append(items, postNordCustomsItem{
			HSTariffNumber:  ci.HSCode,
			Quantity:        ci.Quantity,
			CountryOfOrigin: ci.CountryOfOrigin,
			NetWeight:       ci.NetWeight,
			ItemValue:       ci.Value,
			Currency:        currency,
		})
	}

	return items, warnings
}
