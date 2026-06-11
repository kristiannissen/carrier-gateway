// Package validation provides stateless pre-flight validation for booking
// requests before they are forwarded to carrier APIs.
// This file is located at /internal/validation/carrier_customs_test.go.
package validation

import (
	"testing"

	"github.com/kristiannissen/carrier-gateway/internal/adapter"
)

func TestValidateCarrierCustomsRules_UnknownCarrier(t *testing.T) {
	t.Parallel()

	if err := ValidateCarrierCustomsRules("unknown", adapter.Customs{}); err != nil {
		t.Errorf("unknown carrier should return nil, got %v", err)
	}
}

func TestValidateCarrierCustomsRules_DHL_TooManyItems(t *testing.T) {
	t.Parallel()

	items := make([]adapter.CustomsItem, 100)
	c := adapter.Customs{Items: items}
	if err := ValidateCarrierCustomsRules("dhl", c); err == nil {
		t.Error("expected error for 100 DHL items, got nil")
	}
}

func TestValidateCarrierCustomsRules_DHL_MaxItemsOK(t *testing.T) {
	t.Parallel()

	items := make([]adapter.CustomsItem, 99)
	c := adapter.Customs{
		Items:             items,
		ExporterVATNumber: "DE123456789",
	}
	if err := ValidateCarrierCustomsRules("dhl", c); err != nil {
		t.Errorf("99 DHL items should pass, got %v", err)
	}
}

func TestValidateCarrierCustomsRules_DHL_MissingExporterVAT(t *testing.T) {
	t.Parallel()

	c := adapter.Customs{ExporterVATNumber: ""}
	if err := ValidateCarrierCustomsRules("dhl", c); err == nil {
		t.Error("expected error for missing DHL exporter VAT, got nil")
	}
}

func TestValidateCarrierCustomsRules_PostNord_TooManyItems(t *testing.T) {
	t.Parallel()

	items := make([]adapter.CustomsItem, 6)
	c := adapter.Customs{Items: items}
	if err := ValidateCarrierCustomsRules("postnord", c); err == nil {
		t.Error("expected error for 6 PostNord items, got nil")
	}
}

func TestValidateCarrierCustomsRules_PostNord_MaxItemsOK(t *testing.T) {
	t.Parallel()

	items := make([]adapter.CustomsItem, 5)
	c := adapter.Customs{Items: items}
	if err := ValidateCarrierCustomsRules("postnord", c); err != nil {
		t.Errorf("5 PostNord items should pass, got %v", err)
	}
}

func TestValidateCarrierCustomsRules_GLS_NoItemLimit(t *testing.T) {
	t.Parallel()

	items := make([]adapter.CustomsItem, 200)
	c := adapter.Customs{Items: items}
	if err := ValidateCarrierCustomsRules("gls", c); err != nil {
		t.Errorf("GLS has no item limit, got unexpected error: %v", err)
	}
}
