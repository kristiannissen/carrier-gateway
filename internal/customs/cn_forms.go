// Package customs generates customs declaration forms for cross-border shipments.
// This file is located at /internal/customs/cn_forms.go.
package customs

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// FormType indicates whether the declaration is CN22 or CN23.
// CN22 covers low-value items; CN23 covers commercial shipments above the threshold.
type FormType string

const (
	// FormTypeCN22 is used for shipments with total customs value ≤ cn22MaxValueEUR.
	FormTypeCN22 FormType = "CN22"
	// FormTypeCN23 is used for shipments with total customs value > cn22MaxValueEUR.
	FormTypeCN23 FormType = "CN23"
)

// cn22MaxValueEUR is the maximum declared value (in EUR) for which a CN22 form
// suffices. Values above this threshold require a CN23 form.
// Source: UPU Letter Post Regulations — CN22 is for items of negligible value (≤ €22);
// CN23 is required for commercial shipments and all higher-value items.
const cn22MaxValueEUR = 22.0

// ReasonForExport classifies the purpose of the export.
type ReasonForExport string

const (
	ReasonSaleOfGoods      ReasonForExport = "sale_of_goods"
	ReasonReturnedGoods    ReasonForExport = "returned_goods"
	ReasonGift             ReasonForExport = "gift"
	ReasonPersonalEffects  ReasonForExport = "personal_effects"
	ReasonOther            ReasonForExport = "other"
)

// FormRequest carries the data needed to generate a CN22 or CN23 form.
// It is carrier-agnostic; callers build it from the booking request and response.
type FormRequest struct {
	// TrackingNumber is the carrier tracking number assigned at booking.
	TrackingNumber string
	// SenderName is the full name or company name of the exporter.
	SenderName string
	// SenderAddress is the formatted street + postal code + city of the exporter.
	SenderAddress string
	// SenderCountry is the ISO 3166-1 alpha-2 country code of the exporter.
	SenderCountry string
	// SenderVATNumber is the exporter's VAT registration number (optional).
	SenderVATNumber string
	// ReceiverName is the full name or company name of the importer.
	ReceiverName string
	// ReceiverAddress is the formatted street + postal code + city of the importer.
	ReceiverAddress string
	// ReceiverCountry is the ISO 3166-1 alpha-2 country code of the importer.
	ReceiverCountry string
	// ReceiverVATNumber is the importer's VAT registration number (optional).
	ReceiverVATNumber string
	// Incoterms is the trade term (e.g. DDP, DAP).
	Incoterms string
	// TotalValue is the total declared customs value of the shipment.
	TotalValue float64
	// Currency is the ISO 4217 currency code for TotalValue.
	Currency string
	// TotalWeightKG is the total gross weight of the shipment in kilograms.
	TotalWeightKG float64
	// Reason is the reason for export.
	Reason ReasonForExport
	// Items holds the line-item breakdown. At least one item is required.
	Items []FormItem
}

// FormItem is a single commodity line on the CN22/CN23 form.
type FormItem struct {
	// Description is a plain-language description of the goods (required).
	Description string
	// HSCode is the 6-10 digit Harmonized System code (required for CN23).
	HSCode string
	// CountryOfOrigin is the ISO 3166-1 alpha-2 code where goods were manufactured.
	CountryOfOrigin string
	// Quantity is the number of units of this line item.
	Quantity int
	// NetWeightKG is the net weight in kg for this line item.
	NetWeightKG float64
	// Value is the declared value of this line item.
	Value float64
	// Currency is the ISO 4217 currency for Value.
	Currency string
}

// Form is the generated customs declaration ready for printing or transmission.
type Form struct {
	// Type is CN22 or CN23.
	Type FormType
	// Date is the generation date in YYYY-MM-DD format (UTC).
	Date string
	// Text is the formatted plain-text representation of the form.
	// Callers may base64-encode this for inclusion in API responses.
	Text []byte
}

// Generate produces a CN22 or CN23 form from req.
// The form type is selected based on TotalValue and Currency:
// CN22 when value ≤ 22 EUR (or when Currency is not EUR and value is zero),
// CN23 otherwise.
// Returns an error if req contains no items or is missing required fields.
func Generate(req FormRequest) (*Form, error) {
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("cn form: at least one item is required")
	}
	if req.SenderCountry == "" || req.ReceiverCountry == "" {
		return nil, fmt.Errorf("cn form: sender and receiver country codes are required")
	}

	formType := selectFormType(req.TotalValue, req.Currency)

	date := time.Now().UTC().Format("2006-01-02")

	var buf bytes.Buffer
	writeForm(&buf, formType, date, req)

	return &Form{
		Type: formType,
		Date: date,
		Text: buf.Bytes(),
	}, nil
}

// selectFormType returns CN22 when the shipment qualifies as low-value,
// CN23 otherwise. Currency must be EUR for the CN22 path to apply.
func selectFormType(totalValue float64, currency string) FormType {
	if currency == "EUR" && totalValue <= cn22MaxValueEUR {
		return FormTypeCN22
	}
	return FormTypeCN23
}

// writeForm renders the form content into buf.
func writeForm(buf *bytes.Buffer, ft FormType, date string, req FormRequest) {
	line := func(format string, args ...any) {
		fmt.Fprintf(buf, format+"\n", args...)
	}
	divider := func() { line(strings.Repeat("-", 72)) }
	blank := func() { line("") }

	line("%-36s %s", fmt.Sprintf("CUSTOMS DECLARATION — %s", ft), date)
	divider()

	line("TRACKING NUMBER : %s", req.TrackingNumber)
	line("INCOTERMS       : %s", req.Incoterms)
	line("REASON          : %s", reasonLabel(req.Reason))
	blank()

	line("SENDER")
	line("  Name    : %s", req.SenderName)
	line("  Address : %s", req.SenderAddress)
	line("  Country : %s", req.SenderCountry)
	if req.SenderVATNumber != "" {
		line("  VAT     : %s", req.SenderVATNumber)
	}
	blank()

	line("RECIPIENT")
	line("  Name    : %s", req.ReceiverName)
	line("  Address : %s", req.ReceiverAddress)
	line("  Country : %s", req.ReceiverCountry)
	if req.ReceiverVATNumber != "" {
		line("  VAT     : %s", req.ReceiverVATNumber)
	}
	blank()

	line("CONTENTS")
	divider()
	line("%-4s %-30s %-8s %-6s %-8s %s",
		"QTY", "DESCRIPTION", "HS CODE", "ORIGIN", "WEIGHT", "VALUE")
	divider()

	for _, item := range req.Items {
		cur := item.Currency
		if cur == "" {
			cur = req.Currency
		}
		line("%-4d %-30s %-8s %-6s %6.3f kg %s %.2f",
			item.Quantity,
			truncate(item.Description, 30),
			item.HSCode,
			item.CountryOfOrigin,
			item.NetWeightKG,
			cur,
			item.Value,
		)
	}

	divider()
	line("%-51s %6.3f kg %s %.2f",
		"TOTALS",
		req.TotalWeightKG,
		req.Currency,
		req.TotalValue,
	)
	blank()

	if ft == FormTypeCN23 {
		line("I certify that the particulars given in this customs declaration")
		line("are correct and that this item does not contain any dangerous")
		line("article or articles prohibited by legislation or by postal or")
		line("customs regulations.")
		blank()
		line("Sender signature / Date: ____________________________")
	}
}

// reasonLabel returns a human-readable label for a ReasonForExport.
func reasonLabel(r ReasonForExport) string {
	switch r {
	case ReasonSaleOfGoods:
		return "Sale of goods"
	case ReasonReturnedGoods:
		return "Returned goods"
	case ReasonGift:
		return "Gift"
	case ReasonPersonalEffects:
		return "Personal effects"
	default:
		return "Other"
	}
}

// truncate shortens s to at most n runes, appending "…" if trimmed.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
