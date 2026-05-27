package api
// /api/dao_bookings.go

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// DAOExpressPayload defines the specific JSON schema required by the DAO API.
type DAOExpressPayload struct {
	CustomerToken  string `json:"customer_token"`
	WeightGrams    int    `json:"weight_grams"`
	Destination    string `json:"destination_country"`
	CustomerRef    string `json:"customer_reference"`
}

// DAOResponsePayload defines the expected JSON response contract from the DAO API.
type DAOResponsePayload struct {
	Success        bool   `json:"success"`
	ShipmentID     string `json:"shipment_id"`
	Barcode        string `json:"barcode"`
	LabelURL       string `json:"label_url"`
	ReturnLabelURL string `json:"return_label_url"`
	ErrorMsg       string `json:"error_message,omitempty"`
}

type DAOStrategy struct{}

// ExecuteBooking handles the operational trade compliance and dispatches the request
// either to the live DAO REST API or falls back gracefully to Sandbox simulation mode.
func (s DAOStrategy) ExecuteBooking(req BookingRequest) (*BookingResult, error) {
	
	// 1. Trade Compliance Validation for Non-EU Destinations
	if req.Destination.CountryCode == "NO" || req.Destination.CountryCode == "GB" {
		if req.Incoterm == "" || len(req.CustomsItems) == 0 {
			errMsg := "Trade Compliance Violation: Non-EU destination via DAO requires automated customs mapping and full HS datasets. Verify metrics at https://www.tariffnumber.com/"
			GlobalEM.Notify(ExceptionEvent{Carrier: "dao", Endpoint: "Strategy-Engine", ErrorMessage: errMsg, Timestamp: time.Now()})
			return nil, fmt.Errorf(errMsg)
		}
	}

	// 2. Fetch API Gateway Credentials from Environment Context
	apiKey := os.Getenv("DAO_API_KEY")
	apiURL := os.Getenv("DAO_API_URL")

	// 3. Automated Sandbox Fallback Mechanism
	// If variables are empty, the system safely degrades to simulated local mock responses.
	if apiKey == "" || apiURL == "" {
		// Log sandbox routing telemetry event via the Observer Pattern
		GlobalEM.Notify(ExceptionEvent{
			Carrier:      "dao",
			Endpoint:     "Strategy-Engine",
			ErrorMessage: "Missing API credentials. Gateway automatically routed shipment to Sandbox/Mock mode.",
			Timestamp:    time.Now(),
		})

		// Synthesize valid mock payload for sandbox confirmation loops
		mockBookingID := fmt.Sprintf("MOCK-DAO-%d", time.Now().Unix())
		mockResult := &BookingResult{
			BookingID: mockBookingID,
			Status:    "completed (sandbox)",
			LabelURL:  fmt.Sprintf("https://mock-carrier-cdn.io/sandbox/labels/%s.pdf", mockBookingID),
		}

		if req.IncludeReturnLabel {
			mockResult.ReturnFormat = "pdf"
			if req.ReturnFormat == "qr" {
				mockResult.ReturnFormat = "qr"
			}
			mockResult.ReturnLabelURL = fmt.Sprintf("https://mock-carrier-cdn.io/sandbox/returns/%s.%s", mockBookingID, mockResult.ReturnFormat)
		}

		return mockResult, nil
	}

	// =========================================================================
	// LIVE CARRIER API EXECUTION FLOW (PRODUCTION / LIVE TEST STAGE)
	// =========================================================================

	totalWeightGrams := 0
	for _, colli := range req.Colli {
		totalWeightGrams += int(colli.WeightKG * 1000)
	}

	daoPayload := DAOExpressPayload{
		CustomerToken:  apiKey,
		WeightGrams:    totalWeightGrams,
		Destination:    req.Destination.CountryCode,
		CustomerRef:    "Gateway-Booking-" + fmt.Sprintf("%d", time.Now().Unix()),
	}

	jsonPayload, err := json.Marshal(daoPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DAO payload: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	httpReq, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request for DAO: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(httpReq)
	if err != nil {
		GlobalEM.Notify(ExceptionEvent{Carrier: "dao", Endpoint: "Network-Post", ErrorMessage: err.Error(), Timestamp: time.Now()})
		return nil, fmt.Errorf("DAO API connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		GlobalEM.Notify(ExceptionEvent{Carrier: "dao", Endpoint: "HTTP-Status", ErrorMessage: fmt.Sprintf("Carrier returned status code %d", resp.StatusCode), Timestamp: time.Now()})
		return nil, fmt.Errorf("DAO API returned operational error code: %d", resp.StatusCode)
	}

	var daoResponse DAOResponsePayload
	if err := json.NewDecoder(resp.Body).Decode(&daoResponse); err != nil {
		return nil, fmt.Errorf("failed to decode DAO API response: %w", err)
	}

	if !daoResponse.Success {
		return nil, fmt.Errorf("DAO business logic rejection: %s", daoResponse.ErrorMsg)
	}

	// Map DAO's production response back to our unified BookingResult format.
	// We use the actual label and return URLs provided by the carrier API.
	res := &BookingResult{
		BookingID: daoResponse.ShipmentID,
		Status:    "completed",
		LabelURL:  daoResponse.LabelURL,
	}

	if req.IncludeReturnLabel {
		res.ReturnFormat = "pdf"
		res.ReturnLabelURL = daoResponse.ReturnLabelURL
	}

	return res, nil
}

func init() {
	RegisterStrategy("dao", DAOStrategy{})
}

func DAOBookingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req BookingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	req.CarrierCode = "dao"
	res, err := DAOStrategy{}.ExecuteBooking(req)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []string{err.Error()},
			"guided_correction_url": "https://www.tariffnumber.com/",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(res)
}