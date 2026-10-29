// Command ledger merges every sidecar spool into one log per day and answers
// GET /v1/trace/{id} (internal only).
//
//	ledger -spool /spool -dir /ledger-data -listen 0.0.0.0:8086
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	telemetry "github.com/TwoThousandAndTwo2002/Icarus/telemetry"
)

// build is the commit the binary was built from (-ldflags "-X main.build=<sha>").
var build = "dev"

func main() {
	spool := flag.String("spool", "/spool", "the root with one folder per sidecar (read-only)")
	dir := flag.String("dir", "/ledger-data", "where the merged log goes")
	listen := flag.String("listen", "127.0.0.1:8086", "address to listen on")
	every := flag.Duration("interval", 2*time.Second, "merge interval")
	flag.Parse()
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		log.Fatal(err)
	}
	l := &telemetry.Ledger{SpoolRoot: *spool, Dir: *dir}
	if err := l.Open(); err != nil {
		log.Fatal(err)
	}
	go l.Run(*every, make(chan struct{}))
	srv := &http.Server{Addr: *listen, Handler: l.Handler(build), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 60 * time.Second}
	log.Printf("v3rde-ledger listening on %s: %s -> %s every %s build=%s", *listen, *spool, *dir, *every, build)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
