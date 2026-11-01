// Command push (WS1) is the box side of the API Gateway WebSocket API: the
// $connect and $disconnect integrations and the app's one-time ticket on
// TCP (through Caddy), the services' messages on a unix socket only.
//
//	push -listen 0.0.0.0:8092 -socket /sock/push.sock -auth http+unix://auth.sock \
//	     -secret-file /run/secrets/push_secret -origins https://icarus.thehungreeapp.com \
//	     -endpoint https://<api-id>.execute-api.<region>.amazonaws.com/<stage> -region <region> \
//	     -url wss://<websocket host>/<stage> -events /events
//
// With no -endpoint, -url or secret the service runs with push off: the
// ticket answers 404, so the app keeps its polling.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/TwoThousandAndTwo2002/Icarus/post"
	analyze "github.com/TwoThousandAndTwo2002/Icarus/server"
)

var build = "dev" // -ldflags "-X main.build=<git sha>"

func listenUnix(path string) (net.Listener, error) {
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, errors.New(path + " exists and is not a socket")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	return l, os.Chmod(path, 0o660)
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8092", "the public address (Caddy: /api/push/*)")
	socket := flag.String("socket", "/sock/push.sock", "the unix socket of POST /v1/push/notify (the services)")
	authURL := flag.String("auth", "http+unix://auth.sock", "auth base URL")
	secretFile := flag.String("secret-file", "/run/secrets/push_secret", "the shared secret of the API Gateway integrations (Authorization: Bearer <secret>); empty file = push off")
	origins := flag.String("origins", "https://icarus.thehungreeapp.com", "comma-separated browser origins that may open a WebSocket")
	endpoint := flag.String("endpoint", "", "the management API: https://<api-id>.execute-api.<region>.amazonaws.com/<stage>; empty = push off")
	region := flag.String("region", "us-east-1", "the AWS region of the WebSocket API")
	wsURL := flag.String("url", "", "the WebSocket URL the app opens (wss://...); empty = push off")
	imds := flag.String("imds", "http://169.254.169.254", "the instance metadata service (the box role's credentials)")
	events := flag.String("events", "/events", "directory for events-YYYYMMDD.jsonl (a volume)")
	recheck := flag.Duration("recheck", time.Minute, "how often each connection's session is asked again")
	flag.Parse()
	analyze.EnableUnixURLs(analyze.SockDir)
	if *recheck < 10*time.Second {
		log.Fatal("-recheck must be at least 10s")
	}

	secret := ""
	if b, err := os.ReadFile(*secretFile); err == nil {
		secret = strings.TrimSpace(string(b))
	} else if !os.IsNotExist(err) {
		log.Printf("push secret: %v (push off)", err)
	}
	or := map[string]bool{}
	for _, o := range strings.Split(*origins, ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" { // a browser's Origin has no end slash
			or[o] = true
		}
	}
	on := secret != "" && *endpoint != "" && *wsURL != ""
	cfg := &post.PushConfig{Session: &post.AuthClient{URL: *authURL}, Secret: secret, Origins: or,
		Region: *region, Recheck: *recheck, Events: post.NewEvents(*events), Build: build,
		Creds: &post.IMDSCreds{Base: *imds}}
	if on {
		cfg.Endpoint, cfg.URL = *endpoint, *wsURL
	}
	pub, in := post.NewPush(cfg)
	go func() {
		for range time.Tick(*recheck / 2) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			cfg.Sweep(ctx)
			cancel()
		}
	}()
	errc := make(chan error, 2)
	l, err := listenUnix(*socket)
	if err != nil {
		log.Fatal(err)
	}
	isrv := &http.Server{Handler: in, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second}
	go func() { errc <- isrv.Serve(l) }()
	psrv := &http.Server{Addr: *listen, Handler: pub, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	go func() { errc <- psrv.ListenAndServe() }()
	log.Printf("v3rde-push listening on %s socket=%s on=%v endpoint=%q url=%q origins=%d build=%s (secret %d bytes)",
		*listen, *socket, on, cfg.Endpoint, cfg.URL, len(or), build, len(secret))
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
