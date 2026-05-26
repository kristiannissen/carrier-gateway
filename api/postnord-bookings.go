package api
// /api/postnord-bookings.go

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

func PostNordBookingsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 1. GET: Polling af asynkrone jobs eller download af labels
	if r.Method == http.MethodGet {
		id := r.URL.Query().Get("id")
		asset := r.URL.Query().Get("asset")

		if asset != "" {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"postnord-%s-%s.pdf\"", asset, id))
			fmt.Fprintf(w, "%%PDF-1.4 [MOCK POSTNORD %s DATA FOR ID: %s]", asset, id)
			return
		}

		if id != "" {
			pnMutex.RLock()
			job, exists := pnJobs[id]
			pnMutex.RUnlock()

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

	// 2. POST: Oprettelse af bookinger
	if r.Method == http.MethodPost {
		var req BookingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON payload"})
			return
		}

		// Validering baseret på bookings-v1.json krav
		if len(req.Colli) == 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{"At least one colli item is required"}})
			return
		}

		bookingID := fmt.Sprintf("BK-PN-%d", time.Now().Unix())
		execMode := r.Header.Get("X-Execution-Mode")

		// --- ASYNKRONT FLOW ---
		if execMode == "async" {
			pnMutex.Lock()
			pnJobs[bookingID] = &BookingResult{BookingID: bookingID, Status: "queued"}
			pnMutex.Unlock()

			// Start Goroutine baggrundstråd (Strategy)
			go func(id string, wantReturn bool, host string) {
				time.Sleep(3 * time.Second) // Simulerer eksternt PostNord API-kald
				
				pnMutex.Lock()
				if job, exists := pnJobs[id]; exists {
					job.Status = "completed"
					job.LabelURL = fmt.Sprintf("https://%s/api/v1/postnord-bookings/%s/label", host, id)
					if wantReturn {
						job.ReturnLabelURL = fmt.Sprintf("https://%s/api/v1/postnord-bookings/%s/return-label", host, id)
					}
				}
				pnMutex.Unlock()
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
			LabelURL:  fmt.Sprintf("https://%s/api/v1/postnord-bookings/%s/label", r.Host, bookingID),
		}
		if req.IncludeReturnLabel {
			res.ReturnLabelURL = fmt.Sprintf("https://%s/api/v1/postnord-bookings/%s/return-label", r.Host, bookingID)
		}
		json.NewEncoder(w).Encode(res)
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
}