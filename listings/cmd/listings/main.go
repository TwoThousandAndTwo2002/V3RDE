// Command listings runs the V3RDE listings service.
//
//	listings -listen 0.0.0.0:8082 -data /data -auth http://auth:8081
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	listings "github.com/TwoThousandAndTwo2002/Icarus/listings"
)

// build is the commit the binary was built from (-ldflags "-X main.build=<sha>"); events have it.
var build = "dev"

func main() {
	listen := flag.String("listen", "127.0.0.1:8082", "address to listen on")
	data := flag.String("data", "./data", "folder for listings.json and photos/ (a mounted volume)")
	authURL := flag.String("auth", "http://127.0.0.1:8081", "auth service base URL")
	ttl := flag.Duration("ttl", listings.DefaultTTL, "how long a listing stays active")
	events := flag.String("events", "", "folder for events-YYYYMMDD.jsonl; \"\" = no events log")
	itemPhotos := flag.String("item-photos", "", "the analyze service's item photo folder, read-only (photo_id); \"\" = off")
	touchupPhotos := flag.String("touchup-photos", "", "the touch-up service's edited photos, read-only (photo_id); \"\" = off")
	fanoutURL := flag.String("fanout", "", "fan-out status route for the claim hook, e.g. http://fanout:8090/v1/fanout/status; \"\" = off")
	fanoutTokenFile := flag.String("fanout-token-file", "/run/secrets/fanout_status_token", "the claim hook's own secret (never the analyze token)")
	testEmails := flag.String("test-ttl-emails", "", "X44h: comma-separated test account emails that may send ttl_seconds (30 s up to the TTL); \"\" = none")
	flag.Parse()
	testTTL := map[string]bool{}
	for _, e := range strings.Split(*testEmails, ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			testTTL[e] = true
		}
	}

	st, err := listings.Open(*data, *ttl)
	if err != nil {
		log.Fatalf("open %s: %v", *data, err)
	}
	var claim *listings.ClaimHook
	if *fanoutURL != "" {
		tok, err := listings.ReadToken(*fanoutTokenFile)
		if err != nil {
			log.Fatalf("claim hook: %v", err)
		}
		claim = &listings.ClaimHook{URL: *fanoutURL, Token: tok, HTTP: &http.Client{Timeout: 30 * time.Second}}
	}
	var h http.Handler = listings.New(listings.Config{Store: st,
		Auth:       &listings.AuthClient{URL: *authURL, HTTP: &http.Client{Timeout: 5 * time.Second}},
		ItemPhotos: *itemPhotos, TouchupPhotos: *touchupPhotos, Claim: claim, TestTTLEmails: testTTL})
	if *events != "" {
		if err := os.MkdirAll(*events, 0o755); err != nil {
			log.Fatalf("events dir: %v", err)
		}
		h = listings.Logged("listings", build, listings.NewRecorder(*events, 4096), h)
	}
	srv := &http.Server{Addr: *listen, Handler: h, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("v3rde-listings listening on %s data=%s auth=%s ttl=%s events=%q item-photos=%q touchup-photos=%q claim-hook=%q test-ttl-accounts=%d build=%s",
		*listen, *data, *authURL, *ttl, *events, *itemPhotos, *touchupPhotos, *fanoutURL, len(testTTL), build)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
