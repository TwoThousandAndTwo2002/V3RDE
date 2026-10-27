// Command inventory runs POST /v1/inventory (X31): the multi-item scan.
// Secrets are files (mounted read-only), never flags or environment:
//
//	inventory -listen 0.0.0.0:8087 -key-file /run/secrets/meta_key \
//	          -token-file /run/secrets/analyze_token -gate http://gate:8084 \
//	          -events /events -cap-usd 20
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/TwoThousandAndTwo2002/Icarus/inference"
	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

var build = "dev" // -ldflags "-X main.build=<git sha>"

func readSecret(path, what string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("%s: %v", what, err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		log.Fatalf("%s: %s is empty", what, path)
	}
	return s
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8087", "address to listen on")
	keyFile := flag.String("key-file", "/run/secrets/meta_key", "file holding the Meta Model API key")
	tokenFile := flag.String("token-file", "/run/secrets/analyze_token", "file holding the bearer token callers send (analyze's)")
	gate := flag.String("gate", "http://127.0.0.1:8084", "food gate base URL")
	events := flag.String("events", "/events", "directory for events-YYYYMMDD.jsonl (a volume)")
	upstream := flag.String("upstream", inference.DefaultUpstream, "Meta responses endpoint")
	effort := flag.String("reasoning-effort", "low", "Muse reasoning effort (X30: low)")
	split := flag.Int("split", 3, "Muse calls at the same time for 12+ named boxes (D21)")
	maxNamed := flag.Int("max-named", 24, "boxes named per photo (D14)")
	capUSD := flag.Float64("cap-usd", 20, "daily spend cap in USD, UTC day (T9, 2026-10-26)")
	estimate := flag.Float64("reserve-usd", 0.10, "spend reserved per photo while it runs")
	inFlight := flag.Int("max-in-flight", 3, "photos at the same time; more get 503 busy")
	burst := flag.Int("rate-burst", 6, "requests per window per client IP")
	window := flag.Duration("rate-window", time.Minute, "rate-limit window")
	maxImage := flag.Int64("max-image-bytes", 8<<20, "largest accepted image")
	// X31c (T9, 2026-10-27): 55 s, under the load balancer's 60 s idle
	// timeout, so a slow photo gets a clear 504 from Icarus, not one from the LB.
	timeout := flag.Duration("timeout", 55*time.Second, "per photo, every call included")
	origin := flag.String("origin", "", "CORS Access-Control-Allow-Origin; empty disables")
	flag.Parse()

	key := readSecret(*keyFile, "api key")
	token := readSecret(*tokenFile, "bearer token")
	if err := os.MkdirAll(*events, 0o755); err != nil {
		log.Fatalf("%s: %v", *events, err)
	}
	ev := inference.NewEvents(*events)
	budget := inference.NewBudget(*capUSD, *estimate, ev)
	eng := &inference.Engine{
		API:            &inference.Client{URL: *upstream, Key: key, Effort: *effort, HTTP: &http.Client{Timeout: *timeout}},
		Marks:          true, // marks + 0-1000 coordinates (X30: marks:low:split3)
		MaxNamed:       *maxNamed,
		PackageConcept: "package",
		TrayConcept:    "food tray",
		Split:          *split,
	}
	h := inference.NewService(inference.ServiceConfig{Engine: eng, Auth: analyze.NewBearer(token),
		Limiter: analyze.NewLimiter(*burst, *window), Budget: budget, Events: ev, GateURL: *gate,
		MaxImage: *maxImage, MaxInFlight: *inFlight, Timeout: *timeout, Origin: *origin, Build: build})
	srv := &http.Server{Addr: *listen, Handler: h, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 60 * time.Second, WriteTimeout: *timeout + 10*time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("icarus-inventory listening on %s gate=%s cap=$%.2f spent_today=$%.4f in_flight=%d build=%s (key %d bytes, token %d bytes)",
		*listen, *gate, *capUSD, budget.Spent(), *inFlight, build, len(key), len(token))
	log.Fatal(srv.ListenAndServe())
}
