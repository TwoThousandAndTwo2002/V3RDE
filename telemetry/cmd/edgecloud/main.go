// Command edgecloud prints how long each part of the pipeline takes, edge
// (on the box) and cloud (the Meta Model API), from the ledger (X44d, F5).
//
//	edgecloud -dir /ledger-data [-days 20261029,20261030] [-json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	telemetry "github.com/TwoThousandAndTwo2002/Icarus/telemetry"
)

func main() {
	dir := flag.String("dir", "/ledger-data", "the ledger folder (ledger-YYYYMMDD.jsonl, samples-YYYYMMDD.jsonl)")
	days := flag.String("days", "", "comma-separated YYYYMMDD days; empty = every day in the folder")
	asJSON := flag.Bool("json", false, "print the report as JSON (default: a Markdown table)")
	flag.Parse()
	var d []string
	for _, x := range strings.Split(*days, ",") {
		if x = strings.TrimSpace(x); x != "" {
			d = append(d, x)
		}
	}
	rep, err := telemetry.EdgeCloud(*dir, d)
	if err != nil {
		log.Fatal(err)
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(rep)
		return
	}
	fmt.Print(rep.Markdown())
}
