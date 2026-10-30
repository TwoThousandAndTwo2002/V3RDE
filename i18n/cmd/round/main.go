// Command round runs one translation round after an app update (package
// round). The deploy script starts it as a one-shot job when the app text
// changed:
//
//	round -repo /repo -state /state -out /out -qe local
//
// It reads the app text of the checkout at its HEAD, rebuilds the text graph,
// compares it with the last round in -state, writes rounds/<tag>.json, and
// translates, for each language, only the added, changed and reached texts
// (the batch method and one fix round, MetricX-24 QE, the checks and both
// judges). Passed texts go live; a text that fails twice keeps its last good
// translation and goes to review/<tag>.json. The live files are published to
// -out (each file written to a temp file and renamed).
//
// Exit status: 0 done (or nothing changed), 1 the round failed, 3 some
// languages failed (the others are published; the failed ones keep their live file).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"v3rde/i18n/judge"
	"v3rde/i18n/meta"
	"v3rde/i18n/muse"
	"v3rde/i18n/round"
	"v3rde/i18n/rung"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	var o round.Options
	flag.StringVar(&o.Repo, "repo", "", "the V3RDE git checkout at the deployed commit (read only; required)")
	flag.StringVar(&o.State, "state", "", "the persistent state directory (required)")
	flag.StringVar(&o.Out, "out", "", "the directory of the published files (required)")
	flag.StringVar(&o.Langs, "langs", "", "languages.json (default: <repo>/i18n/languages.json)")
	flag.StringVar(&o.Seed, "seed", "", "first run only: a directory of earlier results (<lang>/<lang>.json or <lang>.json)")
	flag.BoolVar(&o.DryRun, "dry-run", false, "do everything but the model calls: list what would be translated; write nothing")
	flag.IntVar(&o.Batch, "batch", 12, "texts of one screen in one call (L4k)")
	flag.IntVar(&o.Workers, "workers", 8, "translation calls at the same time")
	flag.IntVar(&o.JWorkers, "judge-workers", 4, "calls at the same time for each judge")
	flag.Float64Var(&o.QEMax, "qe-max", 0, "flag a text whose MetricX QE score is above this (0: off; the scores are recorded)")
	basetenKey := flag.String("baseten-key-file", muse.KeyFile(), "file with the Baseten key (mode 0600; never printed)")
	metaKey := flag.String("meta-key-file", meta.KeyFile(), "file with the Meta Model API key (mode 0600; never printed)")
	judges := flag.String("judges", "open,meta", "judges: open (Muse Spark Open, Baseten) and/or meta (Muse Spark 1.3, Meta Model API)")
	effort := flag.String("judge-effort", "low", "reasoning effort of the judges")
	rpm := flag.Int("rpm", 0, "Baseten requests per minute (0: the limit that Baseten's answers give)")
	qe := flag.String("qe", env("V3RDE_QE", "docker"), "MetricX: local (python3 /opt/qe/run_predict.py in this container) or docker (qe/score.sh); env V3RDE_QE")
	qeImage := flag.String("qe-image", env("QE_IMAGE", "v3rde-qe:L2"), "docker: the MetricX image")
	qeDir := flag.String("qe-dir", env("METRICX_DIR", "/opt/metricx"), "local: the MetricX code directory")
	qeScript := flag.String("qe-script", "/opt/qe/run_predict.py", "local: the scorer script")
	flag.Parse()
	if o.Repo == "" || o.State == "" || o.Out == "" {
		fmt.Fprintln(os.Stderr, "round: -repo, -state and -out are required")
		os.Exit(2)
	}
	open := func() (round.Models, error) {
		var m round.Models
		c, err := muse.Load(*basetenKey)
		if err != nil {
			return m, err
		}
		c.Pace.SetRPM(*rpm)
		m.Translator, m.Settings = c, c.Settings
		for _, name := range strings.Split(*judges, ",") {
			switch strings.TrimSpace(name) {
			case "open":
				jc, err := muse.Load(*basetenKey)
				if err != nil {
					return m, err
				}
				jc.Settings.ReasoningEffort = *effort
				jc.Pace = c.Pace // one rate limit for one Baseten key
				m.Judges = append(m.Judges, judge.Baseten{C: jc})
			case "meta":
				mc, err := meta.Load(*metaKey)
				if err != nil {
					return m, err
				}
				mc.Effort = *effort
				m.Judges = append(m.Judges, mc)
			case "":
			default:
				return m, fmt.Errorf("-judges: %q is not open or meta", name)
			}
		}
		switch *qe {
		case "local":
			m.Scorer = rung.Local{Dir: *qeDir, Script: *qeScript, Log: os.Stderr}
		case "docker":
			m.Scorer = rung.Docker{Script: filepath.Join(o.Repo, "qe", "score.sh"), Image: *qeImage, Log: os.Stderr}
		default:
			return m, fmt.Errorf("-qe: %q is not local or docker", *qe)
		}
		return m, nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sum, err := round.Run(ctx, o, round.Deps{Open: open, Log: os.Stdout})
	if err != nil {
		fmt.Fprintln(os.Stderr, "round:", err)
		os.Exit(1)
	}
	if len(sum.Failed) > 0 {
		fmt.Fprintf(os.Stderr, "round: %s: languages failed: %s (they keep their live file)\n", sum.Tag, strings.Join(sum.Failed, ", "))
		os.Exit(3)
	}
}
