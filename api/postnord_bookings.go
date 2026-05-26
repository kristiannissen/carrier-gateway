package api
// /api/postnord_bookings.go

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var (
	pnJobs  = make(map[string]*BookingResult)
	pnMutex sync.RWMutex
)

var euCountries = map[string]bool{
	"DK": true, "SE": true, "FI": true, "DE": true, "FR": true, "NL": true,
	"BE": true, "IT": true, "ES": true, "AT": true, "PL": true, "IE": true,
}

func PostNordBookingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("id")
		asset := r.URL.Query().Get("asset")
		format := r.URL.Query().Get("format")

		if asset != "" {
			if format == "qr" {
				w.Header().Set("Content-Type", "image/png")
				_, _ = fmt.Fprintf(w, "[MOCK QR CODE STREAM FOR POSTNORD ASSET %s ID %s]", asset, id)
				return
			}
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"postnord-%s-%s.pdf\"", asset, id))
			_, _ = fmt.Fprintf(w, "%%PDF-1.4 [MOCK POSTNORD PDF FOR ID: %s]", id)
			return
		}

		if id != "" {
			pnMutex.RLock()
			job, exists := pnJobs[id]
			pnMutex.RUnlock()

			if !exists {
				w.WriteHeader(http.StatusNotFound)
				_, _ = json.NewEncoder(w).Encode(map[string]string{"error": "Booking job not found"})
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = json.NewEncoder(w).Encode(job)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodPost {
		var req BookingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// 1. Core Schema Validation
		if len(req.Colli) == 0 || req.Destination.CountryCode == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []string{"Missing required fields: colli array or destination.country_code"},
			})
			return
		}

		// 2. AUTOMATED TRADE COMPLIANCE & GUIDED SELF-CORRECTION LOOP
		isEU := euCountries[req.Destination.CountryCode]
		if !isEU {
			if req.Incoterm != "DDP" && req.Incoterm != "DAP" {
				errMsg := "Trade Compliance Violation: Non-EU shipments require a valid Incoterm (DDP or DAP)."
				GlobalEM.Notify(ExceptionEvent{Carrier: "postnord", Endpoint: "Bookings-Compliance", ErrorMessage: errMsg, Timestamp: time.Now()})
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{errMsg}})
				return
			}
			if len(req.CustomsItems) == 0 {
				errMsg := "Trade Compliance Violation: Missing mandatory customs_items and HS Codes for Non-EU destination. Look up valid tariffs here: https://www.tariffnumber.com/"
				GlobalEM.Notify(ExceptionEvent{Carrier: "postnord", Endpoint: "Bookings-Compliance", ErrorMessage: errMsg, Timestamp: time.Now()})
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = json.NewEncoder(w).Encode(map[string]interface{}{
					"errors": []string{errMsg},
					"guided_correction_url": "https://www.tariffnumber.com/",
				})
				return
			}
			for _, item := range req.CustomsItems {
				if item.HSCode == "" || item.Description == "" || item.CountryOfOrigin == "" {
					errMsg := "Trade Compliance Violation: Each customs item must contain a valid hs_code, description, and country_of_origin. Verify codes at https://www.tariffnumber.com/"
					GlobalEM.Notify(ExceptionEvent{Carrier: "postnord", Endpoint: "Bookings-Compliance", ErrorMessage: errMsg, Timestamp: time.Now()})
					w.WriteHeader(http.StatusUnprocessableEntity)
					_, _ = json.NewEncoder(w).Encode(map[string]interface{}{
						"errors": []string{errMsg},
						"guided_correction_url": "https://www.tariffnumber.com/",
					})
					return
				}
			}
		}

		bookingID := fmt.Sprintf("BK-PN-%d", time.Now().Unix())
		execMode := r.Header.Get("X-Execution-Mode")

		retFormat := "pdf"
		if req.ReturnFormat == "qr" {
			retFormat = "qr"
		}

		// --- ASYNCHRONOUS STRATEGY ---
		if execMode == "async" {
			pnMutex.Lock()
			pnJobs[bookingID] = &BookingResult{BookingID: bookingID, Status: "queued"}
			pnMutex.Unlock()

			go func(id string, wantReturn bool, format string, host string) {
				time.Sleep(3 * time.Second)
				pnMutex.Lock()
				if job, exists := pnJobs[id]; exists {
					job.Status = "completed"
					job.LabelURL = fmt.Sprintf("https://%s/api/v1/postnord-bookings/%s/label", host, id)
					if wantReturn {
						job.ReturnFormat = format
						job.ReturnLabelURL = fmt.Sprintf("https://%s/api/v1/postnord-bookings/%s/return-label?format=%s", host, id, format)
					}
				}
				pnMutex.Unlock()
			}(bookingID, req.IncludeReturnLabel, retFormat, r.Host)

			w.WriteHeader(http.StatusAccepted)
			_, _ = json.NewEncoder(w).Encode(map[string]string{"booking_id": bookingID, "status": "queued"})
			return
		}

		// --- SYNCHRONOUS STRATEGY ---
		w.WriteHeader(http.StatusCreated)
		res := BookingResult{
			BookingID: bookingID,
			Status:    "completed",
			LabelURL:  fmt.Sprintf("https://%s/api/v1/postnord-bookings/%s/label", r.Host, bookingID),
		}
		if req.IncludeReturnLabel {
			res.ReturnFormat = retFormat
			res.ReturnLabelURL = fmt.Sprintf("https://%s/api/v1/postnord-bookings/%s/return-label?format=%s", r.Host, bookingID, retFormat)
		}
		_, _ = json.NewEncoder(w).Encode(res)
		return
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
}