// Command server is the runnable entry point for the curtain-wall laminated
// glass assembly gate. It performs startup recovery (migrate the embedded
// relational database, reload committed state, reconcile film conservation,
// close expired leases and rebuild the retry index) before opening the
// listener, then mounts the JSON API and serves the static single-page
// frontend.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"curtainwall.example/assembly-gate/internal/httpapi"
	"curtainwall.example/assembly-gate/internal/instrument"
	"curtainwall.example/assembly-gate/internal/store"
)

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	dbPath := flag.String("db", envOr("DB_PATH", "assembly-gate.db"), "SQLite database path")
	frontendDir := flag.String("frontend", envOr("FRONTEND_DIR", "./frontend/dist"), "frontend build directory")
	flag.Parse()

	// Startup recovery happens before the listener opens. NewSQLite migrates
	// the schema, reloads committed state and reconciles invariants; an
	// irrecoverable invariant break aborts before serving.
	s, err := store.NewSQLite(*dbPath, instrument.NewPayloadAdapter())
	if err != nil {
		log.Fatalf("startup recovery failed: %v", err)
	}
	defer s.Close()

	srv := httpapi.New(s, *frontendDir)
	log.Printf("assembly gate listening on %s (db=%s)", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, srv); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
