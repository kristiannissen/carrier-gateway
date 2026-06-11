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
	ParcelOriginOrganization      string              `json:"parcelOriginOrganization"`
	ParcelDestinationOrganization string              `json:"parcelDestinationOrganization"`
	General                       dhlCustomsGeneral   `json:"general"`
	CCustoms                      dhlCCustoms         `json:"cCustoms"`
}

type dhlCustomsGeneral struct {
	ParcelIdentifier       string `json:"parcelIdentifier"`
	Timestamp              string `json:"timestamp"`
	CustomerIdentification string `json:"customerIdentification"`
}

type dhlCCustoms struct {
	CustomsIDs      dhlCustomsIDs       `json:"CustomsIDs"`
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
	Incoterms          string `json:"incoterms"`
	GoodsClassification string `json:"goodsClassification"`
	TotalValue         string `json:"totalValue"`
	Currency           string `json:"currency"`
	ShipmentWeight     string `json:"shipmentWeight"`
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
						Incoterms:          dhlIncoterms(req.Customs.Incoterms),
						GoodsClassification: "CommercialSaleOfGoods",
						TotalValue:         fmt.Sprintf("%.2f", req.Customs.CustomsValue),
						Currency:           req.Customs.CustomsCurrency,
						ShipmentWeight:     "0.000", // populated from shipment weight at call site if needed
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

// glsIncotermCode maps standard Incoterms 2020 codes to GLS-specific integer codes.
// GLS Customs API codes: 10=DDP, 20=DAP, 21=DAP-direct, 25=DAP-secure,
// 30=DDP-duties, 33=DDP-indirect, 40=DAP-cleared, 50=DDP-low-value.
// Unmapped terms default to 20 (DAP).
func glsIncotermCode(incoterms string) int {
	switch incoterms {
	case "DDP":
		return 10
	case "DAP":
		return 20
	default:
		return 20
	}
}

// glsCustomsConsignment is the GLS Customs API v3 POST /customs-consignments body.
type glsCustomsConsignment struct {
	ParcelNumbers                []string            `json:"parcelNumbers"`
	CustomerReference            string              `json:"customerReference,omitempty"`
	GLSIncotermCode              int                 `json:"glsIncotermCode"`
	IsExportDeclarationRequested bool                `json:"isExportDeclarationRequested"`
	SaveAsDraft                  bool                `json:"saveAsDraft,omitempty"`
	ExportDeclarationNumbers     []string            `json:"exportDeclarationNumbers,omitempty"`
	Exporter                     glsCustomsPartyBody `json:"exporter"`
	Importer                     glsCustomsPartyBody `json:"importer"`
	LineItems                    []glsCustomsLineItem `json:"lineItems"`
}

// glsCustomsPartyBody holds the address and tax ID for an exporter or importer.
type glsCustomsPartyBody struct {
	VATRegistrationNumber string            `json:"vatRegistrationNumber,omitempty"`
	EORI                  string            `json:"eori,omitempty"`
	Address               glsCustomsAddress `json:"address"`
}

// glsCustomsAddress is the address sub-object in GLS customs parties.
type glsCustomsAddress struct {
	Name        string `json:"name"`
	Street      string `json:"street"`
	HouseNumber string `json:"houseNumber,omitempty"`
	City        string `json:"city"`
	PostalCode  string `json:"postalCode"`
	CountryCode string `json:"countryCode"`
}

// glsCustomsLineItem maps one CustomsItem to the GLS lineItem schema.
type glsCustomsLineItem struct {
	CommodityCode          string  `json:"commodityCode"`
	CountryOfOrigin        string  `json:"countryOfOrigin"`
	ValueInInvoiceCurrency float64 `json:"valueInInvoiceCurrency"`
	InvoiceCurrency        string  `json:"invoiceCurrency"`
	Quantity               int     `json:"quantity"`
	Description            string  `json:"description,omitempty"`
}

// SubmitCustoms submits a customs consignment to the GLS Customs API v3
// (POST /customs-consignments). The GLS API validates the HS code server-side.
func (a *GLSAdapter) SubmitCustoms(ctx context.Context, req CustomsRequest) (*CustomsResponse, error) {
	token, err := a.bearerToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("gls customs: obtain bearer token: %w", err)
	}

	consignment := glsCustomsConsignment{
		ParcelNumbers:                []string{req.TrackingNumber},
		CustomerReference:            req.EDIItemID,
		GLSIncotermCode:              glsIncotermCode(req.Customs.Incoterms),
		IsExportDeclarationRequested: false,
		Exporter: glsCustomsPartyBody{
			VATRegistrationNumber: req.Customs.ExporterVATNumber,
			Address: glsCustomsAddress{
				Name:        req.Sender.Name,
				Street:      req.Sender.Street,
				HouseNumber: req.Sender.HouseNumber,
				City:        req.Sender.City,
				PostalCode:  req.Sender.PostalCode,
				CountryCode: req.Sender.Country,
			},
		},
		Importer: glsCustomsPartyBody{
			VATRegistrationNumber: req.Customs.ImporterVATNumber,
			Address: glsCustomsAddress{
				Name:        req.Receiver.Name,
				Street:      req.Receiver.Street,
				HouseNumber: req.Receiver.HouseNumber,
				City:        req.Receiver.City,
				PostalCode:  req.Receiver.PostalCode,
				CountryCode: req.Receiver.Country,
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
		ConsignmentID string `json:"consignmentId"`
		Status        string `json:"status"`
	}
	if jsonErr := json.Unmarshal(respBody, &glsResp); jsonErr != nil {
		// Response is not JSON-parseable but status was 2xx — treat as success.
		a.log.Warn("gls customs: could not parse response body", zap.Error(jsonErr))
	}

	a.log.Info("gls customs declaration submitted",
		zap.String("trackingNumber", req.TrackingNumber),
		zap.String("consignmentID", glsResp.ConsignmentID),
	)

	return &CustomsResponse{
		Carrier:       "gls",
		DeclarationID: glsResp.ConsignmentID,
		Status:        "submitted",
	}, nil
}

// buildGLSLineItems converts Customs.Items to GLS customs line items.
// Falls back to a single line item from top-level Customs fields when Items is empty.
func buildGLSLineItems(c Customs) []glsCustomsLineItem {
	if len(c.Items) > 0 {
		items := make([]glsCustomsLineItem, 0, len(c.Items))
		for _, ci := range c.Items {
			currency := ci.Currency
			if currency == "" {
				currency = c.CustomsCurrency
			}
			items = append(items, glsCustomsLineItem{
				CommodityCode:          ci.HSCode,
				CountryOfOrigin:        ci.CountryOfOrigin,
				ValueInInvoiceCurrency: ci.Value,
				InvoiceCurrency:        currency,
				Quantity:               ci.Quantity,
				Description:            ci.Description,
			})
		}
		return items
	}

	// Single top-level fallback.
	return []glsCustomsLineItem{
		{
			CommodityCode:          c.HSCode,
			CountryOfOrigin:        c.CountryOfOrigin,
			ValueInInvoiceCurrency: c.CustomsValue,
			InvoiceCurrency:        c.CustomsCurrency,
			Quantity:               1,
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
	ItemID                 string                    `json:"itemId"`
	SellerVATNumber        string                    `json:"sellerVatNumber,omitempty"`
	BuyerVATNumber         string                    `json:"buyerVatNumber,omitempty"`
	TermsOfSale            string                    `json:"termsOfSale,omitempty"`
	ReasonForExportation   int                       `json:"reasonForExportation"`
	Items                  []postNordCustomsItem     `json:"items"`
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
		return nil, fmt.Errorf("postnord customs: http request: %w", err)
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
