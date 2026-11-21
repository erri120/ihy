// ihy is the ihy server binary.
//
// Barebones start: stdlib HTTP server with a health endpoint.
// Everything else (router, API generation, Postgres wiring) comes
// online as the data model settles.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339),
		})
	})

	addr := ":7731"
	log.Printf("ihy listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
