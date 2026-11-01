// Command touchup runs the photo touch-up service (X35a): Muse Image 1.0
// edits, Muse Spark 1.3 checks, the original is always kept. Secrets are
// files (read-only mounts), never flags or environment. Callers: the app's
// session (/api/touchup) and, X42e, the bearer token (/v1/touchup); the photo
// is always an analyze item photo (photo_id) of that caller.
//
//	touchup -listen 0.0.0.0:8088 -key-file /run/secrets/meta_key \
//	        -auth http://auth:8081 -item-photos /item-photos -dir /data \
//	        -events /events -cap-usd 2
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/TwoThousandAndTwo2002/Icarus/inference"
	"github.com/TwoThousandAndTwo2002/Icarus/post"
	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

var build = "dev" // -ldflags "-X main.build=<git sha>"

func main() {
	listen := flag.String("listen", "127.0.0.1:8088", "address to listen on")
	keyFile := flag.String("key-file", "/run/secrets/meta_key", "file holding the Meta Model API key")
	authURL := flag.String("auth", "", "auth base URL (the app's session cookie); required")
	tokenFile := flag.String("token-file", "", "file holding the bearer token (analyze's) for /v1/touchup; empty = no bearer route")
	base := flag.String("base", post.DefaultBase, "Meta Model API base URL")
	imageModel := flag.String("image-model", post.DefaultImage, "Muse Image model id")
	imageReasoning := flag.String("image-reasoning", "", "reasoning_strength for the edit (high|low); empty = API default")
	checkModel := flag.String("check-model", post.DefaultModel, "Muse Spark model id for the check")
	noCheck := flag.Bool("no-check", false, "skip the Muse Spark check (then every edit is kept_original)")
	itemPhotos := flag.String("item-photos", "", "the analyze service's photo folder, read-only; required")
	dir := flag.String("dir", "/data", "edited photos (a volume), item-photo format")
	events := flag.String("events", "/events", "directory for events-YYYYMMDD.jsonl (a volume)")
	capUSD := flag.Float64("cap-usd", 2, "daily spend cap in USD, UTC day (this service only)")
	callerCap := flag.Float64("caller-cap-usd", 0, "X44a: daily spend cap in USD for each caller (one user or one bearer address); 0 = off")
	estimate := flag.Float64("reserve-usd", 0.03, "spend reserved per touch-up while it runs")
	inFlight := flag.Int("max-in-flight", 2, "touch-ups at the same time; more get 503 busy")
	burst := flag.Int("rate-burst", 4, "requests per window per user")
	maxQueued := flag.Int("max-queued", 12, "X44c: batch jobs that may wait for a free slot (all callers)")
	queueWait := flag.Duration("queue-wait", 3*time.Minute, "X44c: the longest a batch job waits for a slot (then it fails, unpaid)")
	window := flag.Duration("rate-window", time.Minute, "rate-limit window")
	jobTimeout := flag.Duration("job-timeout", 90*time.Second, "one touch-up, both calls included")
	v2 := flag.Bool("v2", false, "X48o: the auto touch-up (Muse Image bigger, SSIM check, the scene rule's mask, studio blur); false = the X35a edit + Muse Spark check")
	photos := flag.String("photos", "", "X48o: the analyze upload folder, read-only (the item is cut again from it); empty = the item photo as it is")
	analyzeEvents := flag.String("analyze-events", "", "X48o: the analyze events folder, read-only (the scan's item names); empty = the general concepts")
	trustFwd := flag.Bool("trust-forwarded", false, "rate-limit by X-Forwarded-For (only behind Caddy, which sets it)")
	pushURL := flag.String("push", "", "WS1: the push service (http+unix://push.sock): a message to the app when a job ends; empty = off")
	flag.Parse()
	analyze.EnableUnixURLs(analyze.SockDir) // US: an http+unix://<name> URL is the socket /sock/<name>
	post.TrustForwarded = *trustFwd

	key, err := post.ReadSecret(*keyFile, "api key")
	if err != nil {
		log.Fatal(err)
	}
	if *authURL == "" || *itemPhotos == "" {
		log.Fatal("-auth and -item-photos are required (the only input is an analyze item photo)")
	}
	// Fail at start, not per job, when the edit folder cannot be written
	// (scratch image, user 65534: only /data and /events are its own).
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		log.Fatalf("-dir %s: %v", *dir, err)
	}
	probe := filepath.Join(*dir, ".write-check")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		log.Fatalf("-dir %s is not writable: %v", *dir, err)
	}
	_ = os.Remove(probe)
	var bearer *analyze.Bearer
	if *tokenFile != "" {
		token, err := post.ReadSecret(*tokenFile, "bearer token")
		if err != nil {
			log.Fatal(err)
		}
		bearer = analyze.NewBearer(token)
	}
	ev := post.NewEvents(*events)
	budget := post.NewBudget(*capUSD, *estimate, ev)
	budget.PerCaller = *callerCap
	hc := &http.Client{Timeout: *jobTimeout}
	cfg := &post.TouchupConfig{
		Push:        &post.PushClient{URL: *pushURL},
		Editor:      &post.ImageEditor{Base: *base, Key: key, Model: *imageModel, Reasoning: *imageReasoning, HTTP: hc},
		Session:     &post.AuthClient{URL: *authURL},
		Auth:        bearer,
		Limiter:     post.NewLimiter(*burst, *window),
		Budget:      budget,
		Events:      ev,
		Dir:         *dir,
		ItemPhotos:  *itemPhotos,
		MaxInFlight: *inFlight,
		MaxQueued:   *maxQueued,
		QueueWait:   *queueWait,
		JobTimeout:  *jobTimeout,
		Build:       build,
	}
	if *v2 {
		vc := &http.Client{Timeout: *jobTimeout, Transport: inference.MetaTransport}
		cfg.V2 = &inference.V2{API: &inference.Client{URL: *base + "/responses", Key: key, Effort: "low", HTTP: vc},
			Muse: inference.DefaultMuse, SAM: inference.DefaultSAM, ImageURL: *base, ImageModel: *imageModel, HTTP: vc}
		cfg.Photos, cfg.AnalyzeEvents = *photos, *analyzeEvents
	}
	if !*noCheck {
		cfg.Checker = &post.Muse{Base: *base, Key: key, Model: *checkModel, Effort: "low", HTTP: hc}
	}
	srv := &http.Server{Addr: *listen, Handler: post.NewTouchup(cfg), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("icarus-touchup listening on %s v2=%v photos=%q analyze-events=%q", *listen, *v2, *photos, *analyzeEvents)
	log.Printf("icarus-touchup listening on %s image=%s check=%v auth=%s bearer=%v item-photos=%q dir=%s cap=$%.2f spent_today=$%.4f build=%s (key %d bytes)",
		*listen, *imageModel, !*noCheck, *authURL, bearer != nil, *itemPhotos, *dir, *capUSD, budget.Spent(), build, len(key))
	log.Fatal(srv.ListenAndServe())
}
