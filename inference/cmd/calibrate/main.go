// Command calibrate reads the analyze events (the scans and the donors'
// labels) and prints how often the donors kept the model's names and
// servings, by box size and by day, with a suggested ConfidentPx when there
// are enough labels (X44g, F6). It changes nothing.
//
//	calibrate -events /events [-days 20261029,20261030] [-json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/TwoThousandAndTwo2002/Icarus/inference"
)

func main() {
	dir := flag.String("events", "/events", "the analyze events folder (events-YYYYMMDD.jsonl)")
	days := flag.String("days", "", "comma-separated YYYYMMDD days; empty = every day in the folder")
	confident := flag.Float64("confident-px", 96, "the engine's ConfidentPx now (inference Engine default)")
	asJSON := flag.Bool("json", false, "print the report as JSON (default: Markdown)")
	flag.Parse()
	var d []string
	for _, x := range strings.Split(*days, ",") {
		if x = strings.TrimSpace(x); x != "" {
			d = append(d, x)
		}
	}
	lines, found, err := inference.ReadEvents(*dir, d)
	if err != nil {
		log.Fatal(err)
	}
	rep := inference.Calibrate(lines, *confident)
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"days": found, "report": rep})
		return
	}
	fmt.Printf("Events days %s.\n\n%s", strings.Join(found, ", "), rep.Markdown())
}
