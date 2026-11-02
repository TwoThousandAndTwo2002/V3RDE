// Command fanout runs the Facebook Page fan-out service (X35c): Muse Spark
// 1.3 writes the post, the Pages API publishes it with the listing photo,
// and a claim (or the timer) edits it to [CLAIMED] / [UNAVAILABLE]. Secrets are
// files (read-only mounts):
//
//	fanout -listen 0.0.0.0:8090 -key-file /run/secrets/meta_key \
//	       -status-token-file /run/secrets/fanout_status_token -page-token-file /run/secrets/fb_page_token \
//	       -page-id <id> -auth http://auth:8081 -listings http://listings:8082 \
//	       -data /data -events /events -cap-usd 1
//
// With no -page-id or no Page token file, the service starts with posting
// off (503 disabled), so it can be deployed before the Page exists.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/TwoThousandAndTwo2002/Icarus/post"
	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

var build = "dev" // -ldflags "-X main.build=<git sha>"

func main() {
	listen := flag.String("listen", "127.0.0.1:8090", "address to listen on")
	keyFile := flag.String("key-file", "/run/secrets/meta_key", "file holding the Meta Model API key")
	tokenFile := flag.String("status-token-file", "/run/secrets/fanout_status_token", "file holding the internal token for /v1/fanout/status (not the analyze token: that one is public)")
	pageTokenFile := flag.String("page-token-file", "/run/secrets/fb_page_token", "file holding the Facebook Page access token")
	pageID := flag.String("page-id", "", "the Facebook Page id; empty = posting off")
	graph := flag.String("graph", "https://graph.facebook.com", "Graph API base URL")
	version := flag.String("graph-version", "v25.0", "Graph API version")
	authURL := flag.String("auth", "http://127.0.0.1:8081", "auth base URL (the app's session cookie)")
	listingsURL := flag.String("listings", "http://127.0.0.1:8082", "listings base URL")
	base := flag.String("base", post.DefaultBase, "Meta Model API base URL")
	model := flag.String("model", post.DefaultModel, "Muse Spark model id")
	linkBase := flag.String("link-base", "", "claim link prefix + listing id (e.g. https://v3rde.thehungreeapp.com/l/); empty = no link")
	data := flag.String("data", "/data", "directory for posts.json (a volume)")
	events := flag.String("events", "/events", "directory for events-YYYYMMDD.jsonl (a volume)")
	capUSD := flag.Float64("cap-usd", 1, "daily spend cap in USD, UTC day (this service only)")
	callerCap := flag.Float64("caller-cap-usd", 0, "X44a: daily spend cap in USD for each caller (one user or one bearer address); 0 = off")
	estimate := flag.Float64("reserve-usd", 0.02, "spend reserved per post while it runs")
	burst := flag.Int("rate-burst", 6, "requests per window per user")
	window := flag.Duration("rate-window", time.Minute, "rate-limit window")
	timeout := flag.Duration("timeout", 25*time.Second, "per request, every call included (under the 30 s gateway)")
	sweep := flag.Duration("sweep", 0, "how often to mark expired posts [UNAVAILABLE]; 0 = off (needs the claim hook first)")
	trustFwd := flag.Bool("trust-forwarded", false, "rate-limit by X-Forwarded-For (only behind Caddy, which sets it)")
	flag.Parse()
	analyze.EnableUnixURLs(analyze.SockDir) // US: an http+unix://<name> URL is the socket /sock/<name>
	post.TrustForwarded = *trustFwd

	key, err := post.ReadSecret(*keyFile, "api key")
	if err != nil {
		log.Fatal(err)
	}
	token, err := post.ReadSecret(*tokenFile, "status token")
	if err != nil {
		log.Fatal(err)
	}
	st, err := post.OpenPosts(*data)
	if err != nil {
		log.Fatalf("open %s: %v", *data, err)
	}
	ev := post.NewEvents(*events)
	budget := post.NewBudget(*capUSD, *estimate, ev)
	budget.PerCaller = *callerCap
	cfg := &post.FanoutConfig{
		Muse:          &post.Muse{Base: *base, Key: key, Model: *model, Effort: "low", HTTP: &http.Client{Timeout: *timeout}},
		Listings:      &post.ListingsClient{URL: *listingsURL, HTTP: &http.Client{Timeout: 10 * time.Second}},
		Session:       &post.AuthClient{URL: *authURL},
		StatusAuth:    analyze.NewBearer(token),
		StatusLimiter: post.NewLimiter(60, time.Minute),
		Limiter:       post.NewLimiter(*burst, *window),
		Budget:        budget,
		Events:        ev,
		Store:         st,
		LinkBase:      *linkBase,
		Timeout:       *timeout,
		Build:         build,
	}
	if *pageID != "" {
		pt, err := post.ReadSecret(*pageTokenFile, "page token")
		if err != nil {
			log.Printf("posting off: %v", err)
		} else {
			// No client Timeout: each Graph call has its own context
			// (PublishTimeout for the upload, -timeout for an edit), and a
			// client Timeout would cut the upload short (X36a).
			cfg.Page = &post.Page{Graph: *graph, Version: *version, PageID: *pageID, Token: pt,
				HTTP: &http.Client{}}
		}
	}
	// X36a: a post that Facebook took but the file did not is written again
	// within a minute, also with -sweep 0 (the default).
	go func() {
		for range time.Tick(time.Minute) {
			cfg.FlushStore()
		}
	}()
	if *sweep > 0 {
		go func() {
			for range time.Tick(*sweep) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				if n := cfg.Sweep(ctx); n > 0 {
					log.Printf("sweep: %d posts marked expired", n)
				}
				cancel()
			}
		}()
	}
	srv := &http.Server{Addr: *listen, Handler: post.NewFanout(cfg), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: *timeout + 10*time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("icarus-fanout listening on %s model=%s page=%v graph=%s/%s sweep=%s cap=$%.2f spent_today=$%.4f build=%s (key %d bytes, token %d bytes)",
		*listen, *model, cfg.Page != nil, *graph, *version, *sweep, *capUSD, budget.Spent(), build, len(key), len(token))
	log.Fatal(srv.ListenAndServe())
}
