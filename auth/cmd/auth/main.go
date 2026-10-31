// Command auth runs the V3RDE sign-in service.
//
//	auth -listen 0.0.0.0:8081 -db /data/auth.json [-cookie-secure=false]
//	     [-smtp-addr smtp.gmail.com:465 -smtp-user-file F -smtp-pass-file F -reset-link-base https://...]
//
// X49o: the password reset mail is on when -smtp-addr is set and both files hold a value (the user file is the
// Google Workspace mailbox, also the From address; the pass file is its app password). Else the reset routes
// answer 503 and the service runs as before.
package main

import (
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
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
	smtpAddr := flag.String("smtp-addr", "", "X49o: mail relay host:port (465: TLS from the first byte, else STARTTLS); \"\" = no reset mail")
	smtpUserFile := flag.String("smtp-user-file", "/run/secrets/smtp_user", "file holding the relay user (the sender mailbox)")
	smtpPassFile := flag.String("smtp-pass-file", "/run/secrets/smtp_pass", "file holding the relay password (an app password)")
	smtpTimeout := flag.Duration("smtp-timeout", 20*time.Second, "the whole SMTP exchange of one mail")
	linkBase := flag.String("reset-link-base", "", "the public https address of the app; the link is <base>/reset#token=...")
	resetBurst := flag.Int("reset-burst", 3, "reset mails per window per email")
	resetWindow := flag.Duration("reset-window", 15*time.Minute, "the window of -reset-burst")
	resetIPBurst := flag.Int("reset-ip-burst", 5, "reset requests per hour per address")
	flag.Parse()

	st, err := auth.Open(*dbPath)
	if err != nil {
		log.Fatalf("open %s: %v", *dbPath, err)
	}
	defer st.Close()
	cfg := auth.Config{Store: st, Limiter: auth.NewLimiter(*burst, *window), CookieSecure: *secure,
		ResetLimiter: auth.NewLimiter(*resetBurst, *resetWindow), ResetIPLimiter: auth.NewLimiter(*resetIPBurst, time.Hour)}
	cfg.Mailer = resetMailer(st, *smtpAddr, *smtpUserFile, *smtpPassFile, *linkBase, *smtpTimeout)
	h := auth.New(cfg)
	if *events != "" {
		if err := os.MkdirAll(*events, 0o755); err != nil {
			log.Fatalf("events dir: %v", err)
		}
		h = auth.Logged("auth", build, auth.NewRecorder(*events, 4096), h)
	}
	srv := &http.Server{Addr: *listen, Handler: h, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("v3rde-auth listening on %s db=%s cookie-secure=%v events=%q reset-mail=%v build=%s", *listen, *dbPath, *secure, *events, cfg.Mailer != nil, build)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// resetMailer is the reset mail sender, or nil (reset off) when it is not set up. A missing or empty secret file
// is logged, not fatal: the service runs and the reset routes answer 503 (the box gets the files with x49p).
func resetMailer(st *auth.Store, addr, userFile, passFile, base string, timeout time.Duration) *auth.Mailer {
	if addr == "" {
		return nil
	}
	read := func(f string) string { // Google shows an app password in groups of 4: every space goes
		b, err := os.ReadFile(f)
		if err != nil {
			log.Printf("reset mail off: cannot read %s: %v", f, err)
			return ""
		}
		return strings.Join(strings.Fields(string(b)), "")
	}
	user, pass := read(userFile), read(passFile)
	if user == "" || pass == "" {
		log.Printf("reset mail off: no relay user or password yet")
		return nil
	}
	if !strings.HasPrefix(base, "https://") { // not fatal: sign-in must work without the reset mail
		log.Printf("reset mail off: -reset-link-base must be an https address")
		return nil
	}
	_, port, _ := net.SplitHostPort(addr)
	implicit := port == auth.ImplicitTLSPort // 465: TLS from the first byte; else STARTTLS
	log.Printf("reset mail on: relay %s (implicit TLS %v), password set", addr, implicit)
	return auth.NewMailer(st, user, base, auth.SMTP{Addr: addr, User: user, Pass: pass, Timeout: timeout, ImplicitTLS: implicit}.Send)
}
