// ihy is the ihy server binary.
//
// Startup: load bootstrap config from environment variables, bring the
// database schema up, serve the first-party API on the configured
// address.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"ihy/internal/config"
	"ihy/internal/db"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	schemaVersion, err := db.Migrate(cfg.DSN)
	if err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Printf("database schema at version %d", schemaVersion)

	if len(cfg.Libraries) > 0 {
		// Bootstrap seed; managed library roots live in the database
		// (web UI). Scanner registration comes with the scanner.
		log.Printf("library roots configured: %s", strings.Join(cfg.Libraries, ", "))
	}
	log.Printf("artwork dir: %s | transcode dir: %s", cfg.ArtworkDir, cfg.TranscodeDir)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":         "ok",
			"time":           time.Now().UTC().Format(time.RFC3339),
			"schema_version": schemaVersion,
		})
	})

	log.Printf("ihy listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}
