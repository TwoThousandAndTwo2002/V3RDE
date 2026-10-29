//go:build litert

// Command gatebench scores a folder of photos with the gate exactly as the
// service does (Decode, TensorFor, Embed, Head.Verdict), one call at a time,
// and writes one JSON line per photo. It is the tool for the runtime parity
// check (the same photos on two LiteRT versions or two models) and for the
// speed bench on the box (-threads 1 against -threads 2).
//
//	gatebench -model m.tflite -head h.json -dir photos -out scores.jsonl \
//	          [-threads 1] [-emb] [-limit 0] [-warmup 3]
//	gatebench -compare a.jsonl,b.jsonl
//
// -compare reads two outputs and prints the verdict flips (by photo id), the
// largest |p_a - p_b| and, when both have -emb, the largest |dy| and the
// lowest cosine of the embeddings. It exits 1 when a verdict flips.
//
// The last line on stderr is the summary: count, verdicts, and the invoke
// time median / p90 / max in ms.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gate "github.com/TwoThousandAndTwo2002/Icarus/gate"
)

type line struct {
	ID       string    `json:"id"`
	Verdict  string    `json:"verdict"`
	P        float64   `json:"p"`
	Norm     float64   `json:"norm"`
	PrepMS   float64   `json:"prep_ms"`
	InvokeMS float64   `json:"invoke_ms"`
	Error    string    `json:"error,omitempty"`
	Emb      []float32 `json:"emb,omitempty"`
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func pct(v []float64, q float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	i := int(q * float64(len(s)-1))
	return s[i]
}

func main() {
	model := flag.String("model", "", "the gate .tflite")
	head := flag.String("head", "", "the gate head JSON")
	dir := flag.String("dir", "", "folder of photos (*.jpg, *.jpeg, *.png)")
	out := flag.String("out", "", "output JSONL (\"\" = stdout)")
	threads := flag.Int("threads", 1, "LiteRT CPU threads")
	emb := flag.Bool("emb", false, "write the embedding of each photo")
	limit := flag.Int("limit", 0, "score at most this many photos (0 = all)")
	warmup := flag.Int("warmup", 3, "untimed runs before the first photo")
	compare := flag.String("compare", "", "a.jsonl,b.jsonl: compare two outputs")
	flag.Parse()
	if *compare != "" {
		os.Exit(compareFiles(*compare))
	}
	if *model == "" || *head == "" || *dir == "" {
		log.Fatal("gatebench: -model, -head and -dir are required")
	}
	hb, err := os.ReadFile(*head)
	if err != nil {
		log.Fatal(err)
	}
	h, err := gate.ParseHead(hb)
	if err != nil {
		log.Fatal(err)
	}
	t := time.Now()
	m, err := gate.OpenModel(*model, *threads)
	if err != nil {
		log.Fatal(err)
	}
	loadMS := ms(time.Since(t))
	var files []string
	for _, pat := range []string{"*.jpg", "*.jpeg", "*.png"} {
		f, _ := filepath.Glob(filepath.Join(*dir, pat))
		files = append(files, f...)
	}
	sort.Strings(files)
	if *limit > 0 && len(files) > *limit {
		files = files[:*limit]
	}
	if len(files) == 0 {
		log.Fatal("gatebench: no photos in " + *dir)
	}
	w := bufio.NewWriter(os.Stdout)
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		w = bufio.NewWriter(f)
	}
	defer w.Flush()
	enc := json.NewEncoder(w)
	n := 1
	for _, d := range m.InputShape() {
		n *= d
	}
	warm := make([]float32, n)
	for i := 0; i < *warmup; i++ {
		if _, err := m.Embed(warm); err != nil {
			log.Fatal(err)
		}
	}
	var inv []float64
	counts := map[string]int{}
	for i, path := range files {
		l := line{ID: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))}
		b, err := os.ReadFile(path)
		if err == nil {
			var rgb []uint8
			var wd, ht int
			rgb, wd, ht, _, err = gate.Decode(b)
			if err == nil {
				t := time.Now()
				x := gate.TensorFor(rgb, wd, ht, h.InputSide, h.Spec())
				l.PrepMS = ms(time.Since(t))
				t = time.Now()
				var v []float32
				v, err = m.Embed(x)
				l.InvokeMS = ms(time.Since(t))
				if err == nil {
					l.Verdict, l.P, l.Norm, _ = h.Verdict(v)
					inv = append(inv, l.InvokeMS)
					if *emb {
						l.Emb = v
					}
				}
			}
		}
		if err != nil {
			l.Error = err.Error()
			l.Verdict = "ERROR"
		}
		counts[l.Verdict]++
		if err := enc.Encode(l); err != nil {
			log.Fatal(err)
		}
		if (i+1)%100 == 0 || i+1 == len(files) {
			fmt.Fprintf(os.Stderr, "\r[%-30s] %d/%d", strings.Repeat("#", (i+1)*30/len(files)), i+1, len(files))
		}
	}
	fmt.Fprintln(os.Stderr)
	sum, _ := json.Marshal(map[string]any{"photos": len(files), "verdicts": counts, "threads": *threads,
		"load_ms": loadMS, "invoke_ms_median": pct(inv, 0.5), "invoke_ms_p90": pct(inv, 0.9), "invoke_ms_max": pct(inv, 1)})
	fmt.Fprintln(os.Stderr, "SUMMARY "+string(sum))
}

func readLines(path string) (map[string]line, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	m := map[string]line{}
	var ids []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var l line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			return nil, nil, err
		}
		if _, ok := m[l.ID]; !ok {
			ids = append(ids, l.ID)
		}
		m[l.ID] = l
	}
	return m, ids, sc.Err()
}

func compareFiles(arg string) int {
	parts := strings.Split(arg, ",")
	if len(parts) != 2 {
		log.Fatal("gatebench: -compare wants a.jsonl,b.jsonl")
	}
	a, ids, err := readLines(parts[0])
	if err != nil {
		log.Fatal(err)
	}
	b, _, err := readLines(parts[1])
	if err != nil {
		log.Fatal(err)
	}
	var flips []string
	both, missing := 0, 0
	maxDP, maxDY, minCos := 0.0, 0.0, 1.0
	for _, id := range ids {
		la, lb := a[id], b[id]
		if _, ok := b[id]; !ok {
			missing++
			continue
		}
		both++
		if la.Verdict != lb.Verdict {
			flips = append(flips, fmt.Sprintf("%s %s(%.4f)->%s(%.4f)", id, la.Verdict, la.P, lb.Verdict, lb.P))
		}
		if d := la.P - lb.P; d > maxDP || -d > maxDP {
			if d < 0 {
				d = -d
			}
			maxDP = d
		}
		if len(la.Emb) > 0 && len(la.Emb) == len(lb.Emb) {
			var dot, na, nb float64
			for i := range la.Emb {
				x, y := float64(la.Emb[i]), float64(lb.Emb[i])
				if d := x - y; d > maxDY || -d > maxDY {
					if d < 0 {
						d = -d
					}
					maxDY = d
				}
				dot, na, nb = dot+x*y, na+x*x, nb+y*y
			}
			if na > 0 && nb > 0 {
				if c := dot / (sqrt(na) * sqrt(nb)); c < minCos {
					minCos = c
				}
			}
		}
	}
	out, _ := json.Marshal(map[string]any{"photos": both, "missing_in_b": missing, "flips": len(flips),
		"max_abs_dp": maxDP, "max_abs_dy": maxDY, "min_cosine": minCos})
	fmt.Println("COMPARE " + string(out))
	for _, f := range flips {
		fmt.Println("FLIP " + f)
	}
	if len(flips) > 0 || missing > 0 {
		return 1
	}
	return 0
}

func sqrt(x float64) float64 { return math.Sqrt(x) }
