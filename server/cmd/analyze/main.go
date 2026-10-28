// Command analyze runs the V3RDE analyze server. The secrets are files
// (mounted read-only into the container), never flags or environment
// variables:
//
//	analyze -listen 0.0.0.0:8080 -key-file /run/secrets/meta_key \
//	        -token-file /run/secrets/analyze_token -metrics-dir /metrics \
//	        -origin http://localhost:3000
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

// secret reads one secret file; an empty file is an error.
func secret(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return s, nil
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "address to listen on")
	keyFile := flag.String("key-file", "", "file with the Meta Model API key (required)")
	tokenFile := flag.String("token-file", "", "file with the bearer token the app sends (required)")
	metricsDir := flag.String("metrics-dir", "./metrics", "folder for metrics-YYYYMMDD.jsonl")
	origin := flag.String("origin", "http://localhost:3000", "CORS Access-Control-Allow-Origin; empty = off")
	model := flag.String("model", analyze.DefaultModel, "Meta model id")
	upstream := flag.String("upstream", analyze.DefaultUpstream, "Meta responses endpoint")
	effort := flag.String("reasoning-effort", "", "reasoning.effort to send (low|medium|high); empty = the API default")
	burst := flag.Int("rate-burst", 10, "requests per window per client address")
	window := flag.Duration("rate-window", time.Minute, "rate-limit window")
	maxImage := flag.Int64("max-image-bytes", 5<<20, "largest photo accepted")
	timeout := flag.Duration("timeout", 75*time.Second, "time for one request, the Meta call included")
	flag.Parse()
	if *keyFile == "" || *tokenFile == "" {
		log.Fatal("-key-file and -token-file are required")
	}
	key, err := secret(*keyFile)
	if err != nil {
		log.Fatalf("api key: %v", err)
	}
	token, err := secret(*tokenFile)
	if err != nil {
		log.Fatalf("bearer token: %v", err)
	}

	h := analyze.New(analyze.Config{
		Upstream: &analyze.Upstream{URL: *upstream, Key: key, Model: *model, Effort: *effort,
			HTTP: &http.Client{Timeout: *timeout}},
		Auth:     analyze.NewBearer(token),
		Limiter:  analyze.NewLimiter(*burst, *window),
		Recorder: analyze.NewRecorder(*metricsDir),
		Origin:   *origin,
		MaxImage: *maxImage,
		Timeout:  *timeout,
	})
	srv := &http.Server{Addr: *listen, Handler: h,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: *timeout + 5*time.Second, IdleTimeout: 60 * time.Second}
	// The log has the lengths of the secrets, never the secrets.
	log.Printf("v3rde-analyze listening on %s model=%s origin=%q metrics=%s (key %d bytes, token %d bytes)",
		*listen, *model, *origin, *metricsDir, len(key), len(token))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
