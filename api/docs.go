package api
// api/docs.go

import (
	"fmt"
	"net/http"
)

func DocsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status": "Logistics Gateway Booking API Live"}`)
}
