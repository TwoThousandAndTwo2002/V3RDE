// Command touchup runs the photo touch-up service (X35a): Muse Image 1.0
// edits, Muse Spark 1.3 checks, the original is always kept. Secrets are
// files (read-only mounts), never flags or environment:
//
//	touchup -listen 0.0.0.0:8088 -key-file /run/secrets/meta_key \
//	        -token-file /run/secrets/analyze_token -auth http://auth:8081 \
//	        -item-photos /item-photos -dir /touchups -events /events -cap-usd 2
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/TwoThousandAndTwo2002/Icarus/post"
	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

var build = "dev" // -ldflags "-X main.build=<git sha>"

func main() {
	listen := flag.String("listen", "127.0.0.1:8088", "address to listen on")
	keyFile := flag.String("key-file", "/run/secrets/meta_key", "file holding the Meta Model API key")
	tokenFile := flag.String("token-file", "/run/secrets/analyze_token", "file holding the bearer token (analyze's)")
	authURL := flag.String("auth", "", "auth base URL (the app's session cookie); empty = bearer only")
	base := flag.String("base", post.DefaultBase, "Meta Model API base URL")
	imageModel := flag.String("image-model", post.DefaultImage, "Muse Image model id")
	imageReasoning := flag.String("image-reasoning", "", "reasoning_strength for the edit (high|low); empty = API default")
	checkModel := flag.String("check-model", post.DefaultModel, "Muse Spark model id for the check")
	noCheck := flag.Bool("no-check", false, "skip the Muse Spark check (then every edit is kept_original)")
	itemPhotos := flag.String("item-photos", "", "the analyze service's photo folder, read-only; empty = no photo_id input")
	dir := flag.String("dir", "/touchups", "edited photos (a volume), item-photo format")
	events := flag.String("events", "/events", "directory for events-YYYYMMDD.jsonl (a volume)")
	capUSD := flag.Float64("cap-usd", 2, "daily spend cap in USD, UTC day (this service only)")
	estimate := flag.Float64("reserve-usd", 0.03, "spend reserved per touch-up while it runs")
	inFlight := flag.Int("max-in-flight", 2, "touch-ups at the same time; more get 503 busy")
	burst := flag.Int("rate-burst", 4, "requests per window per user (bearer: per client IP)")
	window := flag.Duration("rate-window", time.Minute, "rate-limit window")
	jobTimeout := flag.Duration("job-timeout", 90*time.Second, "one touch-up, both calls included")
	trustFwd := flag.Bool("trust-forwarded", false, "rate-limit by X-Forwarded-For (only behind Caddy, which sets it)")
	flag.Parse()
	post.TrustForwarded = *trustFwd

	key, err := post.ReadSecret(*keyFile, "api key")
	if err != nil {
		log.Fatal(err)
	}
	token, err := post.ReadSecret(*tokenFile, "bearer token")
	if err != nil {
		log.Fatal(err)
	}
	ev := post.NewEvents(*events)
	budget := post.NewBudget(*capUSD, *estimate, ev)
	hc := &http.Client{Timeout: *jobTimeout}
	cfg := &post.TouchupConfig{
		Editor:      &post.ImageEditor{Base: *base, Key: key, Model: *imageModel, Reasoning: *imageReasoning, HTTP: hc},
		Auth:        analyze.NewBearer(token),
		Limiter:     post.NewLimiter(*burst, *window),
		Budget:      budget,
		Events:      ev,
		Dir:         *dir,
		ItemPhotos:  *itemPhotos,
		MaxInFlight: *inFlight,
		JobTimeout:  *jobTimeout,
		Build:       build,
	}
	if !*noCheck {
		cfg.Checker = &post.Muse{Base: *base, Key: key, Model: *checkModel, Effort: "low", HTTP: hc}
	}
	if *authURL != "" {
		cfg.Session = &post.AuthClient{URL: *authURL}
	}
	srv := &http.Server{Addr: *listen, Handler: post.NewTouchup(cfg), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("icarus-touchup listening on %s image=%s check=%v auth=%s item-photos=%q dir=%s cap=$%.2f spent_today=$%.4f build=%s (key %d bytes, token %d bytes)",
		*listen, *imageModel, !*noCheck, *authURL, *itemPhotos, *dir, *capUSD, budget.Spent(), build, len(key), len(token))
	log.Fatal(srv.ListenAndServe())
}
