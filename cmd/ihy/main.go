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
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"ihy/internal/api"
	"ihy/internal/api/gen"
	"ihy/internal/auth"
	"ihy/internal/config"
	"ihy/internal/db"
	"ihy/internal/musicbrainz"
	"ihy/internal/resolver"
	"ihy/internal/scanner"
	"ihy/internal/webui"
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
	// Bootstrap the single admin account (first boot only).
	created, err := auth.EnsureAdminUser(context.Background(), conn, "admin", cfg.AdminPassword)
	switch {
	case err != nil:
		log.Printf("admin bootstrap: %v", err)
	case created:
		log.Printf("admin account created (set IHY_ADMIN_PASSWORD; log in to create API tokens)")
	case !created && cfg.AdminPassword == "":
		log.Printf("admin account already exists")
	}

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
			var rt http.RoundTripper = live
			// Dev/offline mode: serve recorded entities from the
			// replay manifest instead of the API; misses still go
			// live. Production leaves this unset.
			if dir := os.Getenv("IHY_MB_REPLAYS"); dir != "" {
				replay, err := musicbrainz.NewReplayTransport(dir)
				if err != nil {
					log.Printf("replays: %v (falling back to live)", err)
				} else {
					record, err := musicbrainz.NewRecordingTransport(dir, live)
					if err != nil {
						log.Printf("recording transport: %v (falling back to live)", err)
					} else {
						rt = musicbrainz.NewReplayFirstTransport(replay, record)
						log.Printf("MusicBrainz: replay-first from %s", dir)
					}
				}
			}
			fetcher := musicbrainz.NewClient(
				musicbrainz.DefaultBaseURL,
				musicbrainz.UserAgent("0.0.1", musicbrainz.DefaultContactURL),
				musicbrainz.NewCachingTransport(conn, rt))
			res := resolver.New(conn, fetcher)
			if _, err := res.Resolve(context.Background()); err != nil {
				log.Printf("resolver: %v", err)
			}
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

	// Generated API handlers, wrapped in the auth middleware: the
	// public auth routes (login/logout) pass through, everything else
	// requires a session or API token.
	server := api.NewServer(conn)
	mux.Handle("/", api.NewAuth(conn).Middleware(gen.Handler(server)))

	// Web UI routes: server-rendered templ pages with htmx. Specific
	// patterns win over the API catch-all above.
	webui.NewServer(conn).Register(mux)

	log.Printf("ihy listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}
