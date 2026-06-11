// Package customs provides customs declaration form generation.
// This file is located at /internal/customs/cn_forms_test.go.
package customs

import (
	"strings"
	"testing"
)

func TestGenerate_CN23ForHighValue(t *testing.T) {
	t.Parallel()

	req := FormRequest{
		TrackingNumber:  "TRK123456",
		SenderName:      "Unisport DK",
		SenderAddress:   "Hørkær 26, 2730 Herlev",
		SenderCountry:   "DK",
		SenderVATNumber: "12345678",
		ReceiverName:    "Oslo Sports AS",
		ReceiverAddress: "Karl Johans gate 1, 0154 Oslo",
		ReceiverCountry: "NO",
		Incoterms:       "DDP",
		TotalValue:      850.00,
		Currency:        "NOK",
		TotalWeightKG:   1.2,
		Reason:          ReasonSaleOfGoods,
		Items: []FormItem{
			{
				Description:     "Football boots",
				HSCode:          "6403999100",
				CountryOfOrigin: "CN",
				Quantity:        2,
				NetWeightKG:     0.6,
				Value:           425.00,
				Currency:        "NOK",
			},
		},
	}

	form, err := Generate(req)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if form.Type != FormTypeCN23 {
		t.Errorf("form.Type = %s, want CN23", form.Type)
	}
	if len(form.Text) == 0 {
		t.Error("form.Text is empty")
	}
	text := string(form.Text)
	if !strings.Contains(text, "CN23") {
		t.Errorf("form text does not contain CN23 header")
	}
	if !strings.Contains(text, "TRK123456") {
		t.Errorf("form text missing tracking number")
	}
	if !strings.Contains(text, "6403999100") {
		t.Errorf("form text missing HS code")
	}
}

func TestGenerate_CN22ForLowEURValue(t *testing.T) {
	t.Parallel()

	req := FormRequest{
		TrackingNumber:  "TRK000001",
		SenderName:      "Unisport DK",
		SenderAddress:   "Hørkær 26, 2730 Herlev",
		SenderCountry:   "DK",
		ReceiverName:    "Jane Doe",
		ReceiverAddress: "Karl Johans gate 1, 0154 Oslo",
		ReceiverCountry: "NO",
		Incoterms:       "DAP",
		TotalValue:      18.00,
		Currency:        "EUR",
		TotalWeightKG:   0.2,
		Reason:          ReasonSaleOfGoods,
		Items: []FormItem{
			{
				Description:     "Sports socks",
				HSCode:          "6115100000",
				CountryOfOrigin: "PT",
				Quantity:        3,
				NetWeightKG:     0.2,
				Value:           18.00,
				Currency:        "EUR",
			},
		},
	}

	form, err := Generate(req)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if form.Type != FormTypeCN22 {
		t.Errorf("form.Type = %s, want CN22", form.Type)
	}
}

func TestGenerate_ErrorOnEmptyItems(t *testing.T) {
	t.Parallel()

	req := FormRequest{
		SenderCountry:   "DK",
		ReceiverCountry: "NO",
		Items:           nil,
	}

	_, err := Generate(req)
	if err == nil {
		t.Error("Generate() expected error for empty items, got nil")
	}
}

func TestGenerate_ErrorOnMissingCountry(t *testing.T) {
	t.Parallel()

	req := FormRequest{
		SenderCountry:   "",
		ReceiverCountry: "NO",
		Items:           []FormItem{{Description: "item", Quantity: 1}},
	}

	_, err := Generate(req)
	if err == nil {
		t.Error("Generate() expected error for missing sender country, got nil")
	}
}

func TestSelectFormType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    float64
		currency string
		want     FormType
	}{
		{"EUR below threshold", 10.0, "EUR", FormTypeCN22},
		{"EUR at threshold", cn22MaxValueEUR, "EUR", FormTypeCN22},
		{"EUR above threshold", 23.0, "EUR", FormTypeCN23},
		{"NOK high value", 850.0, "NOK", FormTypeCN23},
		{"NOK low value", 5.0, "NOK", FormTypeCN23}, // non-EUR always CN23
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := selectFormType(tc.value, tc.currency)
			if got != tc.want {
				t.Errorf("selectFormType(%v, %q) = %s, want %s", tc.value, tc.currency, got, tc.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short string = %q, want %q", got, "hello")
	}
	got := truncate("abcdefghij", 5)
	if len([]rune(got)) > 5 {
		t.Errorf("truncate did not shorten: %q", got)
	}
}
