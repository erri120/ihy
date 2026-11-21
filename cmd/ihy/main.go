// ihy is the ihy server binary.
//
// Startup: load bootstrap config from environment variables, bring the
// database schema up, scan library roots in the background, serve the
// first-party API on the configured address.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"ihy/internal/config"
	"ihy/internal/db"
	"ihy/internal/musicbrainz"
	"ihy/internal/resolver"
	"ihy/internal/scanner"
)

func main() {
	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	schemaVersion, err := db.Migrate(cfg.DSN())
	if err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Printf("database schema at version %d", schemaVersion)

	conn, err := sql.Open("pgx", cfg.DSN())
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	if len(cfg.Libraries) == 0 {
		log.Printf("no library roots configured; set IHY_LIBRARIES or manage them in the web UI later")
	} else {
		// Bootstrap seed (IHY_LIBRARIES); managed roots live in the
		// database and are added via the web UI later. The resolver
		// runs right after the scan, in the same background chain, so
		// startup never blocks.
		log.Printf("library roots: %s", strings.Join(cfg.Libraries, ", "))
		go func() {
			scanner.RunAll(context.Background(), conn, cfg.Libraries, scanner.DefaultTagReader)
			live := musicbrainz.NewLiveTransport(http.DefaultTransport)
			fetcher := musicbrainz.NewClient(
				musicbrainz.DefaultBaseURL,
				musicbrainz.UserAgent("0.0.1", musicbrainz.DefaultContactURL),
				musicbrainz.NewCachingTransport(conn, live))
			res := resolver.New(conn, fetcher)
			out, err := res.Resolve(context.Background())
			if err != nil {
				log.Printf("resolver: %v", err)
				return
			}
			log.Printf("resolver: %d artists, %d release-groups, %d releases, %d recordings, %d works, %d credits, %d aliases, %d conflicts, %d stale MBIDs",
				out.Artists, out.Groups, out.Releases, out.Recordings, out.Works, out.Credits, out.Aliases, out.Conflicts, out.NotFound)
		}()
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
