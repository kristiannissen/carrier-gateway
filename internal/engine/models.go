package engine
// /internal/engine/models.go

import "time"

// BookingRequest definerer det schema, som vores HTML/frontend sender ind
type BookingRequest struct {
	Carrier        string    `json:"carrier"` // f.eks. "postnord"
	SenderName     string    `json:"sender_name"`
	SenderAddress  string    `json:"sender_address"`
	SenderZip      string    `json:"sender_zip"`
	SenderCity     string    `json:"sender_city"`
	ReceiverName   string    `json:"receiver_name"`
	ReceiverAddress string   `json:"receiver_address"`
	ReceiverZip    string    `json:"receiver_zip"`
	ReceiverCity   string    `json:"receiver_city"`
	WeightKG       float64   `json:"weight_kg"`
}

// BookingResponse definerer det schema, som gatewayen sender tilbage til HTML-siden
type BookingResponse struct {
	TrackingNumber string    `json:"tracking_number"`
	Status         string    `json:"status"`
	Carrier        string    `json:"carrier"`
	LabelURL       string    `json:"label_url,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}