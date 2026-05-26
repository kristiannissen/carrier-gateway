package api
// /api/dao-bookings.go

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

	// 1. GET: Polling eller download
	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("id")
		asset := r.URL.Query().Get("asset")

		if asset != "" {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"dao-%s-%s.pdf\"", asset, id))
			fmt.Fprintf(w, "%%PDF-1.4 [MOCK DAO %s DATA FOR ID: %s]", asset, id)
			return
		}

		if id != "" {
			daoMutex.RLock()
			job, exists := daoJobs[id]
			daoMutex.RUnlock()

			if !exists {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]string{"error": "Booking job not found"})
				return
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(job)
			return
		}

		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Missing 'id' parameter"})
		return
	}

	// 2. POST: Oprettelse
	if r.Method == http.MethodPost {
		var req BookingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON payload"})
			return
		}

		if len(req.Colli) == 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{"At least one colli item is required"}})
			return
		}

		bookingID := fmt.Sprintf("BK-DAO-%d", time.Now().Unix())
		execMode := r.Header.Get("X-Execution-Mode")

		// --- ASYNKRONT FLOW ---
		if execMode == "async" {
			daoMutex.Lock()
			daoJobs[bookingID] = &BookingResult{BookingID: bookingID, Status: "queued"}
			daoMutex.Unlock()

			go func(id string, wantReturn bool, host string) {
				time.Sleep(3 * time.Second) // Simulerer DAO backend api-kald
				
				daoMutex.Lock()
				if job, exists := daoJobs[id]; exists {
					job.Status = "completed"
					job.LabelURL = fmt.Sprintf("https://%s/api/v1/dao-bookings/%s/label", host, id)
					if wantReturn {
						job.ReturnLabelURL = fmt.Sprintf("https://%s/api/v1/dao-bookings/%s/return-label", host, id)
					}
				}
				daoMutex.Unlock()
			}(bookingID, req.IncludeReturnLabel, r.Host)

			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{
				"booking_id": bookingID,
				"status":     "queued",
				"message":    "Shipment request accepted. Processing in background.",
			})
			return
		}

		// --- SYNKRONT FLOW ---
		w.WriteHeader(http.StatusCreated)
		res := BookingResult{
			BookingID: bookingID,
			Status:    "completed",
			LabelURL:  fmt.Sprintf("https://%s/api/v1/dao-bookings/%s/label", r.Host, bookingID),
		}
		if req.IncludeReturnLabel {
			res.ReturnLabelURL = fmt.Sprintf("https://%s/api/v1/dao-bookings/%s/return-label", r.Host, bookingID)
		}
		json.NewEncoder(w).Encode(res)
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
}