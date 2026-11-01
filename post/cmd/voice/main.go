// Command voice runs the voice posting service (X35d): Muse Voice Transcribe
// 1.0 turns one spoken WAV into text, Muse Spark 1.3 maps it to changes of
// the review list. Secrets are files (read-only mounts):
//
//	voice -listen 0.0.0.0:8091 -key-file /run/secrets/meta_key \
//	      -token-file /run/secrets/analyze_token -auth http://auth:8081 \
//	      -events /events -cap-usd 1
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
	listen := flag.String("listen", "127.0.0.1:8091", "address to listen on")
	keyFile := flag.String("key-file", "/run/secrets/meta_key", "file holding the Meta Model API key")
	tokenFile := flag.String("token-file", "/run/secrets/analyze_token", "file holding the bearer token (analyze's)")
	authURL := flag.String("auth", "", "auth base URL (the app's session cookie); empty = bearer only")
	asrURL := flag.String("asr-url", post.DefaultASRURL, "Muse Voice Transcribe batch endpoint")
	asrModel := flag.String("asr-model", post.DefaultASRModel, "speech model id")
	base := flag.String("base", post.DefaultBase, "Meta Model API base URL")
	model := flag.String("model", post.DefaultModel, "Muse Spark model id")
	events := flag.String("events", "/events", "directory for events-YYYYMMDD.jsonl (a volume)")
	capUSD := flag.Float64("cap-usd", 1, "daily spend cap in USD, UTC day (this service only)")
	callerCap := flag.Float64("caller-cap-usd", 0, "X44a: daily spend cap in USD for each caller (one user or one bearer address); 0 = off")
	estimate := flag.Float64("reserve-usd", 0.01, "spend reserved per request while it runs")
	maxAudio := flag.Duration("max-audio", 60*time.Second, "the longest utterance")
	maxBytes := flag.Int64("max-bytes", 12<<20, "the largest upload")
	burst := flag.Int("rate-burst", 10, "requests per window per user (bearer: per client IP)")
	inFlight := flag.Int("max-in-flight", 2, "requests at the same time; more get 503 busy (memory)")
	window := flag.Duration("rate-window", time.Minute, "rate-limit window")
	timeout := flag.Duration("timeout", 25*time.Second, "per request, both calls included (under the 30 s gateway)")
	trustFwd := flag.Bool("trust-forwarded", false, "rate-limit by X-Forwarded-For (only behind Caddy, which sets it)")
	flag.Parse()
	analyze.EnableUnixURLs(analyze.SockDir) // US: an http+unix://<name> URL is the socket /sock/<name>
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
	budget.PerCaller = *callerCap
	hc := &http.Client{Timeout: *timeout}
	cfg := &post.VoiceConfig{
		ASR:      &post.Transcriber{URL: *asrURL, Key: key, Model: *asrModel, HTTP: hc},
		Muse:     &post.Muse{Base: *base, Key: key, Model: *model, Effort: "low", HTTP: hc},
		Auth:     analyze.NewBearer(token),
		Limiter:  post.NewLimiter(*burst, *window),
		Budget:   budget,
		Events:   ev,
		MaxAudio: *maxAudio,
		MaxBytes: *maxBytes,
		Timeout:  *timeout,
		Build:    build,

		MaxInFlight: *inFlight,
	}
	if *authURL != "" {
		cfg.Session = &post.AuthClient{URL: *authURL}
	}
	srv := &http.Server{Addr: *listen, Handler: post.NewVoice(cfg), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: *timeout + 10*time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("icarus-voice listening on %s asr=%s model=%s auth=%s max_audio=%s cap=$%.2f spent_today=$%.4f build=%s (key %d bytes, token %d bytes)",
		*listen, *asrModel, *model, *authURL, *maxAudio, *capUSD, budget.Spent(), build, len(key), len(token))
	log.Fatal(srv.ListenAndServe())
}
