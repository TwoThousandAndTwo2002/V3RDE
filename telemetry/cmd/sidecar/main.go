// Command sidecar ships the events files of one service and samples its
// process.
//
//	sidecar -service gate -events /events -spool /spool -build <commit>
//
// Run it in the service's PID namespace (compose: pid: "service:gate"), so
// the service is PID 1 under /proc.
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	telemetry "github.com/TwoThousandAndTwo2002/Icarus/telemetry"
)

func main() {
	svc := flag.String("service", "", "the service name")
	events := flag.String("events", "/events", "the service's events files (read-only)")
	spool := flag.String("spool", "/spool", "this sidecar's output")
	build := flag.String("build", "dev", "the service's build (the short commit)")
	pid := flag.Int("pid", 1, "the service's PID in the shared namespace")
	every := flag.Duration("interval", time.Second, "sample + ship interval")
	flag.Parse()
	if *svc == "" {
		log.Fatal("-service required")
	}
	if err := os.MkdirAll(*spool, 0o755); err != nil {
		log.Fatal(err)
	}
	s := &telemetry.Sidecar{Service: *svc, Build: *build, EventsDir: *events, SpoolDir: *spool, ProcRoot: "/proc", PID: *pid}
	if err := s.Open(); err != nil {
		log.Fatal(err)
	}
	stop := make(chan struct{})
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
		<-c
		close(stop)
	}()
	log.Printf("sidecar for %s (build %s): events %s -> spool %s every %s, pid %d", *svc, *build, *events, *spool, *every, *pid)
	s.Run(*every, stop)
	s.Close()
}
