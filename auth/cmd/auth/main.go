// Command auth runs the V3RDE sign-in service.
//
//	auth -listen 0.0.0.0:8081 -db /data/auth.json [-cookie-secure=false]
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	auth "github.com/TwoThousandAndTwo2002/Icarus/auth"
)

// build is the commit the binary was built from (-ldflags "-X main.build=<sha>"); events have it.
var build = "dev"

func main() {
	listen := flag.String("listen", "127.0.0.1:8081", "address to listen on")
	dbPath := flag.String("db", "./auth.json", "the data file (on a mounted volume in the container)")
	secure := flag.Bool("cookie-secure", true, "set Secure on the session cookie (false only for plain http without Caddy)")
	burst := flag.Int("rate-burst", 20, "signup + login tries per window per address")
	window := flag.Duration("rate-window", time.Minute, "rate-limit window")
	events := flag.String("events", "", "folder for events-YYYYMMDD.jsonl; \"\" = no events log")
	flag.Parse()

	st, err := auth.Open(*dbPath)
	if err != nil {
		log.Fatalf("open %s: %v", *dbPath, err)
	}
	defer st.Close()
	h := auth.New(auth.Config{Store: st, Limiter: auth.NewLimiter(*burst, *window), CookieSecure: *secure})
	if *events != "" {
		if err := os.MkdirAll(*events, 0o755); err != nil {
			log.Fatalf("events dir: %v", err)
		}
		h = auth.Logged("auth", build, auth.NewRecorder(*events, 4096), h)
	}
	srv := &http.Server{Addr: *listen, Handler: h, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("v3rde-auth listening on %s db=%s cookie-secure=%v events=%q build=%s", *listen, *dbPath, *secure, *events, build)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
