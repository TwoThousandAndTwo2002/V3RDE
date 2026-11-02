// Command learn (LR2/LR3, I1) reads the analyze events (the scans and the
// donors' labels) and writes the self-improvement report of the last days:
// by prompt version (P1), by how the item was made, the fields the donors
// change, the servings bias and the calibration of X44g. It changes nothing
// in the service; the box runs it once a day (deploy/learn).
//
//	learn -events /events [-last 7] [-today] [-out /learn] [-json]
//
// It reads the -last whole UTC days before today (with -today: today too).
// With -out it writes report-YYYYMMDD.json and report-YYYYMMDD.md (the last
// day read), each through a temporary file and a rename, and keeps the newest
// -keep reports. Without -out it prints the report.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/TwoThousandAndTwo2002/Icarus/inference"
)

// Stored is the JSON file of one run.
type Stored struct {
	Day       string                `json:"day"`
	Generated string                `json:"generated"`
	Days      []string              `json:"days"` // the events days read
	Report    inference.LearnReport `json:"report"`
}

// lastDays is the n UTC days that end with end (oldest first).
func lastDays(end time.Time, n int) []string {
	var d []string
	for i := n - 1; i >= 0; i-- {
		d = append(d, end.AddDate(0, 0, -i).Format("20060102"))
	}
	return d
}

func write(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// prune keeps the newest keep days of reports in dir.
func prune(dir string, keep int) error {
	files, err := filepath.Glob(filepath.Join(dir, "report-????????.json"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for len(files) > keep {
		day := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(files[0]), "report-"), ".json")
		for _, ext := range []string{".json", ".md"} {
			if err := os.Remove(filepath.Join(dir, "report-"+day+ext)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		files = files[1:]
	}
	return nil
}

func main() {
	dir := flag.String("events", "/events", "the analyze events folder (events-YYYYMMDD.jsonl)")
	last := flag.Int("last", 7, "the number of whole UTC days before today to read (0 = every day in the folder)")
	today := flag.Bool("today", false, "end with today (the day so far), not with yesterday")
	out := flag.String("out", "", "the report folder; empty = print the report")
	keep := flag.Int("keep", 90, "with -out: the number of daily reports to keep")
	confident := flag.Float64("confident-px", 96, "the engine's ConfidentPx now (inference Engine default)")
	asJSON := flag.Bool("json", false, "without -out: print JSON (default: Markdown)")
	flag.Parse()
	if *keep < 1 {
		log.Fatal("-keep must be at least 1 (the report just written)")
	}
	now := time.Now().UTC()
	end := now.AddDate(0, 0, -1)
	if *today {
		end = now
	}
	var days []string
	if *last > 0 {
		days = lastDays(end, *last)
	}
	lines, found, err := inference.ReadEvents(*dir, days)
	if err != nil {
		log.Fatal(err)
	}
	st := Stored{Day: end.Format("20060102"), Generated: now.Format(time.RFC3339), Days: found,
		Report: inference.Learn(lines, *confident)}
	if found == nil {
		st.Days = []string{}
	}
	md := fmt.Sprintf("# Learn report %s\n\nEvents days: %s.\n\n%s", st.Day, strings.Join(found, ", "), st.Report.Markdown())
	if *out == "" {
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(st)
			return
		}
		fmt.Print(md)
		return
	}
	b, err := json.MarshalIndent(st, "", " ")
	if err != nil {
		log.Fatal(err)
	}
	if err := write(filepath.Join(*out, "report-"+st.Day+".json"), append(b, '\n')); err != nil {
		log.Fatal(err)
	}
	if err := write(filepath.Join(*out, "report-"+st.Day+".md"), []byte(md)); err != nil {
		log.Fatal(err)
	}
	if err := prune(*out, *keep); err != nil {
		log.Fatal(err)
	}
	log.Printf("learn: report-%s (%d events days, %d analyze lines, %d label sets)", st.Day, len(found), st.Report.AnalyzeLines, st.Report.LabelSets)
}
