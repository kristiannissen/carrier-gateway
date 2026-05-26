package main
// /cmd/gateway-cli/main.go
import (
	"flag"
	"fmt"
	"os"
	"time"

	// Ret denne sti til at matche dit modulnavn fra go.mod
	"github.com/kristiannissen/logistics-gateway" 
)

// En simpel in-memory struktur til at simulere vores mock-kørsel i CLI'et
func main() {
	carrier := flag.String("carrier", "postnord", "Freight carrier switch (postnord or dao)")
	asyncMode := flag.Bool("async", false, "Toggle asynchronous background execution")
	flag.Parse()

	fmt.Println("🚀 LOGISTICS GATEWAY CLI v1.0")
	fmt.Printf("[CONFIG] Target Carrier: %s | Async Mode: %t\n\n", *carrier, *asyncMode)

	// 1. Forbered payload (vores interne standard-format)
	req := api.BookingRequest{
		CarrierCode:        *carrier,
		IncludeReturnLabel: true,
		Colli:              []interface{}{map[string]interface{}{"weight_kg": 4.2}},
	}

	// 2. Validering af valg (Vores CLI-router)
	if *carrier != "postnord" && *carrier != "dao" {
		// Vi tænder med det samme for vores Observer Pattern, hvis der tastes forkert
		api.GlobalEM.Notify(api.ExceptionEvent{
			Carrier:      *carrier,
			Endpoint:     "CLI-Main",
			ErrorMessage: "Unsupported carrier code passed to terminal client",
			Timestamp:    time.Now(),
		})
		os.Exit(1)
	}

	// 3. Afvikling baseret på sync/async strategien
	if *asyncMode {
		// --- ASYNKRON CLI FLOW ---
		bookingID := fmt.Sprintf("BK-%s-%d", *carrier, time.Now().Unix())
		fmt.Printf("📥 [202 Accepted] Shipment payload verified against schema.\n")
		fmt.Printf("Job successfully queued in background with ID: %s\n", bookingID)
		
		// Vi simulerer en Goroutine baggrundstråd i CLI'et
		go func(id string, c string) {
			time.Sleep(2 * time.Second) // Simulerer netværkskaldet mod kureren
			// Her vil kurerens specifikke adapter køre i fremtiden
		}(bookingID, *carrier)
		
		// CLI lukker med det samme (præcis som Vercel vil gøre)
		time.Sleep(100 * time.Millisecond) // Giver lige tråden lov at registrere
		fmt.Println("\n⚡ Execution released. Background thread working.")
	} else {
		// --- SYNKRON CLI FLOW ---
		fmt.Printf("Connecting to %s API link...\n", *carrier)
		time.Sleep(1 * time.Second) // Simulerer netværks-lag

		bookingID := fmt.Sprintf("BK-%s-%d", *carrier, time.Now().Unix())
		fmt.Println("✅ [201 Created] Booking resolved successfully!")
		fmt.Printf("📄 Label URL:        https://gateway.com/api/v1/%s-bookings/%s/label\n", *carrier, bookingID)
		fmt.Printf("🔄 Return Label URL: https://gateway.com/api/v1/%s-bookings/%s/return-label\n", *carrier, bookingID)
	}
}