// Command ledger merges every sidecar spool into one log per day and answers
// GET /v1/trace/{id} (internal only).
//
//	ledger -spool /spool -dir /ledger-data -listen 0.0.0.0:8086
package main

import (
	"errors"
	"flag"
	"fmt"
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
	// US (T9 2026-11-01: "the unix sockets within the box"): -listen "" with -socket = the socket only
	socket := flag.String("socket", "", "US: also answer on this unix socket (the shared volume /sock); \"\" = no socket")
	probe := flag.String("probe-socket", "", "US: GET /healthz over this unix socket, print the answer and exit (the stack check)")
	every := flag.Duration("interval", 2*time.Second, "merge interval")
	flag.Parse()
	if *probe != "" {
		body, err := telemetry.ProbeUnix(*probe, "/healthz", 5*time.Second)
		fmt.Println(body)
		if err != nil {
			log.Fatal(err)
		}
		return
	}
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
	log.Printf("v3rde-ledger listening on %q socket=%q: %s -> %s every %s build=%s", *listen, *socket, *spool, *dir, *every, build)
	if err := telemetry.Serve(srv, *listen, *socket); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
