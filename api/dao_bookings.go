package api
// /api/dao_bookings.go

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type DAOStrategy struct{}

func (s DAOStrategy) ExecuteBooking(req BookingRequest) (*BookingResult, error) {
	// Trade Compliance Check for Non-EU
	if req.Destination.CountryCode == "NO" || req.Destination.CountryCode == "GB" {
		if req.Incoterm == "" || len(req.CustomsItems) == 0 {
			errMsg := "Trade Compliance Violation: Non-EU destination via DAO requires automated customs mapping and full HS datasets. Verify metrics at https://www.tariffnumber.com/"
			GlobalEM.Notify(ExceptionEvent{Carrier: "dao", Endpoint: "Strategy-Engine", ErrorMessage: errMsg, Timestamp: time.Now()})
			return nil, fmt.Errorf(errMsg)
		}
	}

	bookingID := fmt.Sprintf("BK-DAO-%d", time.Now().Unix())
	res := &BookingResult{
		BookingID: bookingID,
		Status:    "completed",
		LabelURL:  fmt.Sprintf("https://mock-carrier-cdn.io/labels/%s.pdf", bookingID),
	}

	if req.IncludeReturnLabel {
		res.ReturnFormat = "pdf"
		if req.ReturnFormat == "qr" {
			res.ReturnFormat = "qr"
		}
		res.ReturnLabelURL = fmt.Sprintf("https://mock-carrier-cdn.io/returns/%s.%s", bookingID, res.ReturnFormat)
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

	req.CarrierCode = "dao" // Tvinger carrier-koden i denne specifikke handler
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