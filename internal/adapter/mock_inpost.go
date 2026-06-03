package adapter

import (
	"context"
	"sync"
)

// MockInPostAdapter is a mock implementation of the InPostAdapter for testing.
type MockInPostAdapter struct {
	// Mutex to ensure thread-safe access to the mock data
	mu sync.Mutex

	// Expected request for validation
	expectedRequest *InPostBookingRequest

	// Response to return
	response *InPostBookingResponse

	// Error to return
	err error

	// Track if BookShipment was called
	bookShipmentCalled bool

	// Track the last request received
	lastRequest *InPostBookingRequest
}

// NewMockInPostAdapter creates a new MockInPostAdapter instance.
func NewMockInPostAdapter() *MockInPostAdapter {
	return &MockInPostAdapter{}
}

// WithExpectedRequest sets the expected request for validation.
func (m *MockInPostAdapter) WithExpectedRequest(req *InPostBookingRequest) *MockInPostAdapter {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expectedRequest = req
	return m
}

// WithResponse sets the response to return.
func (m *MockInPostAdapter) WithResponse(resp *InPostBookingResponse) *MockInPostAdapter {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.response = resp
	return m
}

// WithError sets the error to return.
func (m *MockInPostAdapter) WithError(err error) *MockInPostAdapter {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
	return m
}

// BookShipment is the mock implementation of the InPostAdapter.BookShipment method.
func (m *MockInPostAdapter) BookShipment(ctx context.Context, unifiedReq *UnifiedBookingRequest) (*InPostBookingResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.bookShipmentCalled = true

	// Translate the unified request to InPost format
	inpostReq := m.TranslateUnifiedToInPost(unifiedReq)
	m.lastRequest = inpostReq

	// If an expected request is set, validate it
	if m.expectedRequest != nil {
		// Here you can add assertions to validate the request
		// For simplicity, we just store the last request
		_ = inpostReq
	}

	// Return the configured response or error
	if m.err != nil {
		return nil, m.err
	}

	return m.response, nil
}

// TranslateUnifiedToInPost is a copy of the translation function from InPostAdapter.
// This ensures the mock behaves identically to the real adapter.
func (m *MockInPostAdapter) TranslateUnifiedToInPost(unifiedReq *UnifiedBookingRequest) *InPostBookingRequest {
	// Extract house number from street (if available)
	extractHouseNumber := func(street string) (streetName, houseNumber string) {
		parts := splitStreetAddress(street)
		if len(parts) > 1 {
			return parts[0], parts[1]
		}
		return street, ""
	}

	inpostReq := &InPostBookingRequest{}
	streetName, houseNumber := extractHouseNumber(unifiedReq.Shipment.Sender.Street)
	inpostReq.Shipment.Sender = InPostSender{
		Name: unifiedReq.Shipment.Sender.Name,
		Address: InPostAddress{
			StreetName:  streetName,
			HouseNumber: houseNumber,
			City:        unifiedReq.Shipment.Sender.City,
			PostalCode:  unifiedReq.Shipment.Sender.PostalCode,
			Country:     unifiedReq.Shipment.Sender.Country,
		},
		Contact: InPostContact{
			Phone: unifiedReq.Shipment.Sender.Phone,
			Email: unifiedReq.Shipment.Sender.Email,
		},
	}
	inpostReq.Shipment.Recipient = InPostRecipient{
		Name: unifiedReq.Shipment.Receiver.Name,
		Address: InPostAddress{
			StreetName:  extractHouseNumber(unifiedReq.Shipment.Receiver.Street),
			HouseNumber: extractHouseNumber(unifiedReq.Shipment.Receiver.Street),
			City:        unifiedReq.Shipment.Receiver.City,
			PostalCode:  unifiedReq.Shipment.Receiver.PostalCode,
			Country:     unifiedReq.Shipment.Receiver.Country,
		},
		Contact: InPostContact{
			Phone: unifiedReq.Shipment.Receiver.Phone,
			Email: unifiedReq.Shipment.Receiver.Email,
		},
	}

	// Convert colli to parcels
	for _, colli := range unifiedReq.Shipment.Colli {
		inpostParcel := InPostParcel{
			ID:     colli.ID,
			Weight: colli.Weight,
			Dimensions: InPostDimensions{
				Length: int(colli.Dimensions.Length),
				Width:  int(colli.Dimensions.Width),
				Height: int(colli.Dimensions.Height),
			},
		}
		inpostReq.Shipment.Parcels = append(inpostReq.Shipment.Parcels, inpostParcel)
	}

	// Set service details
	inpostReq.Shipment.Service = InPostService{
		ID:           "INPOST_STANDARD",
		PickupDate:   "2026-06-10", // Default to a fixed date for testing
		TargetLocker: "",
	}

	// Set reference
	if len(unifiedReq.Shipment.Colli) > 0 && unifiedReq.Shipment.Colli[0].Reference != "" {
		inpostReq.Shipment.Reference = unifiedReq.Shipment.Colli[0].Reference
	} else {
		inpostReq.Shipment.Reference = "INPOST-MOCK-REFERENCE"
	}

	return inpostReq
}

// splitStreetAddress is a copy of the helper function from InPostAdapter.
func splitStreetAddress(street string) []string {
	var parts []string
	lastSpace := -1
	for i := len(street) - 1; i >= 0; i-- {
		if street[i] == ' ' {
			lastSpace = i
			break
		}
	}
	if lastSpace != -1 {
		parts = append(parts, street[:lastSpace])
		parts = append(parts, street[lastSpace+1:])
	} else {
		parts = append(parts, street)
	}
	return parts
}

// BookShipmentCalled returns whether BookShipment was called.
func (m *MockInPostAdapter) BookShipmentCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bookShipmentCalled
}

// LastRequest returns the last request received by the mock.
func (m *MockInPostAdapter) LastRequest() *InPostBookingRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRequest
}

// Reset resets the mock state.
func (m *MockInPostAdapter) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expectedRequest = nil
	m.response = nil
	m.err = nil
	m.bookShipmentCalled = false
	m.lastRequest = nil
}
