package api
// /api/dao_bookings.go

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

var (
	daoJobs  = make(map[string]*BookingResult)
	daoMutex sync.RWMutex
)

func DAOBookingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("id")
		asset := r.URL.Query().Get("asset")
		format := r.URL.Query().Get("format")

		if asset != "" {
			if format == "qr" {
				w.Header().Set("Content-Type", "image/png")
				_, _ = fmt.Fprintf(w, "[MOCK QR CODE STREAM FOR DAO ASSET %s ID %s]", asset, id)
				return
			}
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"dao-%s-%s.pdf\"", asset, id))
			_, _ = fmt.Fprintf(w, "%%PDF-1.4 [MOCK DAO PDF FOR ID: %s]", id)
			return
		}

		if id != "" {
			daoMutex.RLock()
			job, exists := daoJobs[id]
			daoMutex.RUnlock()

			if !exists {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Booking job not found"})
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

		if len(req.Colli) == 0 || req.Destination.CountryCode == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errors": []string{"Missing required fields: colli array or destination.country_code"},
			})
			return
		}

		// TRADE COMPLIANCE & GUIDED SELF-CORRECTION (Symmetrisk tjek for DAO)
		if req.Destination.CountryCode == "NO" || req.Destination.CountryCode == "GB" {
			if req.Incoterm == "" || len(req.CustomsItems) == 0 {
				errMsg := "Trade Compliance Violation: Non-EU destination via DAO requires automated customs mapping and full HS datasets. Verify metrics at https://www.tariffnumber.com/"
				GlobalEM.Notify(ExceptionEvent{Carrier: "dao", Endpoint: "Bookings-Compliance", ErrorMessage: errMsg, Timestamp: time.Now()})
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = json.NewEncoder(w).Encode(map[string]interface{}{
					"errors": []string{errMsg},
					"guided_correction_url": "https://www.tariffnumber.com/",
				})
				return
			}
		}

		bookingID := fmt.Sprintf("BK-DAO-%d", time.Now().Unix())
		execMode := r.Header.Get("X-Execution-Mode")

		retFormat := "pdf"
		if req.ReturnFormat == "qr" {
			retFormat = "qr"
		}

		// --- ASYNCHRONOUS STRATEGY ---
		if execMode == "async" {
			daoMutex.Lock()
			daoJobs[bookingID] = &BookingResult{BookingID: bookingID, Status: "queued"}
			daoMutex.Unlock()

			go func(id string, wantReturn bool, format string, host string) {
				time.Sleep(3 * time.Second)
				daoMutex.Lock()
				if job, exists := daoJobs[id]; exists {
					job.Status = "completed"
					job.LabelURL = fmt.Sprintf("https://%s/api/v1/dao-bookings/%s/label", host, id)
					if wantReturn {
						job.ReturnFormat = format
						job.ReturnLabelURL = fmt.Sprintf("https://%s/api/v1/dao-bookings/%s/return-label?format=%s", host, id, format)
					}
				}
				daoMutex.Unlock()
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
			LabelURL:  fmt.Sprintf("https://%s/api/v1/dao-bookings/%s/label", r.Host, bookingID),
		}
		if req.IncludeReturnLabel {
			res.ReturnFormat = retFormat
			res.ReturnLabelURL = fmt.Sprintf("https://%s/api/v1/dao-bookings/%s/return-label?format=%s", r.Host, bookingID, retFormat)
		}
		_, _ = json.NewEncoder(w).Encode(res)
		return
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
}
