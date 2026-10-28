// Command rescue runs the rescue service (X35b): a listing that nobody
// claimed before its timer ran out -> Muse Spark 1.3 drafts an inquiry, the
// Feed America directory gives the nearest food banks or meal programs, the
// donor sends it. Secrets are files (read-only mounts):
//
//	rescue -listen 0.0.0.0:8089 -key-file /run/secrets/meta_key \
//	       -auth http://auth:8081 -listings http://listings:8082 \
//	       -data /data -events /events -cap-usd 1
//
// The mail relay is off unless -smtp-addr is set (then -smtp-pass-file and
// -from are needed).
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/TwoThousandAndTwo2002/Icarus/post"
)

var build = "dev" // -ldflags "-X main.build=<git sha>"

func main() {
	listen := flag.String("listen", "127.0.0.1:8089", "address to listen on")
	keyFile := flag.String("key-file", "/run/secrets/meta_key", "file holding the Meta Model API key")
	authURL := flag.String("auth", "http://127.0.0.1:8081", "auth base URL (the app's session cookie)")
	listingsURL := flag.String("listings", "http://127.0.0.1:8082", "listings base URL")
	directory := flag.String("directory", "https://feedam.org", "Feed America API base URL")
	base := flag.String("base", post.DefaultBase, "Meta Model API base URL")
	model := flag.String("model", post.DefaultModel, "Muse Spark model id")
	data := flag.String("data", "/data", "directory for rescues.json (a volume)")
	events := flag.String("events", "/events", "directory for events-YYYYMMDD.jsonl (a volume)")
	capUSD := flag.Float64("cap-usd", 1, "daily spend cap in USD, UTC day (this service only)")
	estimate := flag.Float64("reserve-usd", 0.02, "spend reserved per rescue while it runs")
	minServings := flag.Int("min-servings", 1, "the bulk rule: a listing needs this many servings (T9 decides; 1 = every listing)")
	places := flag.Int("places", 3, "places in the answer")
	sendPerDay := flag.Int("send-per-day", 3, "emails per donor per UTC day (relay only)")
	burst := flag.Int("rate-burst", 6, "requests per window per user")
	window := flag.Duration("rate-window", time.Minute, "rate-limit window")
	timeout := flag.Duration("timeout", 25*time.Second, "per request, every call included (under the 30 s gateway)")
	smtpAddr := flag.String("smtp-addr", "", "mail relay host:port (STARTTLS); empty = no relay, mailto links only")
	smtpUser := flag.String("smtp-user", "", "mail relay user")
	smtpPassFile := flag.String("smtp-pass-file", "/run/secrets/smtp_pass", "file holding the mail relay password")
	from := flag.String("from", "", "sender address for the relay")
	smtpTimeout := flag.Duration("smtp-timeout", 20*time.Second, "the whole SMTP exchange (dial, TLS, auth, data)")
	trustFwd := flag.Bool("trust-forwarded", false, "rate-limit by X-Forwarded-For (only behind Caddy, which sets it)")
	flag.Parse()
	post.TrustForwarded = *trustFwd

	key, err := post.ReadSecret(*keyFile, "api key")
	if err != nil {
		log.Fatal(err)
	}
	st, err := post.OpenRescues(*data)
	if err != nil {
		log.Fatalf("open %s: %v", *data, err)
	}
	ev := post.NewEvents(*events)
	budget := post.NewBudget(*capUSD, *estimate, ev)
	cfg := &post.RescueConfig{
		Muse:        &post.Muse{Base: *base, Key: key, Model: *model, Effort: "low", HTTP: &http.Client{Timeout: *timeout}},
		Directory:   &post.Directory{Base: *directory, HTTP: &http.Client{Timeout: 10 * time.Second}},
		Listings:    &post.ListingsClient{URL: *listingsURL, HTTP: &http.Client{Timeout: 10 * time.Second}},
		Session:     &post.AuthClient{URL: *authURL},
		Limiter:     post.NewLimiter(*burst, *window),
		Budget:      budget,
		Events:      ev,
		Store:       st,
		MinServings: *minServings,
		Places:      *places,
		SendPerDay:  *sendPerDay,
		Timeout:     *timeout,
		Build:       build,
	}
	if *smtpAddr != "" {
		if *from == "" {
			log.Fatal("-from is required with -smtp-addr")
		}
		pass := ""
		if *smtpUser != "" {
			if pass, err = post.ReadSecret(*smtpPassFile, "smtp password"); err != nil {
				log.Fatal(err)
			}
		}
		cfg.Mailer = &post.Mailer{Addr: *smtpAddr, User: *smtpUser, Pass: pass, From: *from, Timeout: *smtpTimeout}
	}
	srv := &http.Server{Addr: *listen, Handler: post.NewRescue(cfg), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: *timeout + 10*time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("icarus-rescue listening on %s model=%s directory=%s listings=%s relay=%v min_servings=%d cap=$%.2f spent_today=$%.4f build=%s (key %d bytes)",
		*listen, *model, *directory, *listingsURL, cfg.Mailer != nil, *minServings, *capUSD, budget.Spent(), build, len(key))
	log.Fatal(srv.ListenAndServe())
}
