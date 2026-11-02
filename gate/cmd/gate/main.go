// Command gate runs the V3RDE food gate on LiteRT (CPU).
//
//	gate -listen 0.0.0.0:8084 -model /assets/gate_pe_s16_f16w_384.tflite \
//	     -head /assets/gate_head_pe_s16_384_deploy.json -events /events
//
// It refuses a model or a head whose sha256 is not the pinned one, or whose
// shapes do not agree. A gate that does not load leaves the service up, and
// it answers UNAVAILABLE: it fails closed, never open.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	gate "github.com/TwoThousandAndTwo2002/Icarus/gate"
)

// build is the commit the binary was built from (-ldflags "-X main.build=<sha>"); events have it.
var build = "dev"

// The pins of the PE-Core-S16-384 gate (Meta, Apache-2.0): the fp16-weight
// .tflite and its deploy head, the files accepted in tools/hackathon/results
// (entries 2 and 3) and pinned in the compose file since entry 7.
const (
	pinModel = "c0618012abe6a22664285b85ae0d7820e3013342965cb74aa6767404a5ff649e"
	pinHead  = "7ba764ea8b562bef0948678dba5a1d103b0772ce2df46f3959d181d093c507a4"
)

// short is the first 12 characters of a pin, for an error text.
func short(s string) string { return s[:min(12, len(s))] }

func fileSHA256(path string) (string, []byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), b, nil
}

// open loads one gate and checks it: pinned files, a head that parses, and a
// model whose input and output agree with the head.
func open(modelPath, headPath, modelPin, headPin string, threads int) (gate.Embedder, *gate.Head, string, string, error) {
	headSum, headBytes, err := fileSHA256(headPath)
	if err != nil {
		return nil, nil, "", "", err
	}
	if headPin != "" && headSum != headPin {
		return nil, nil, "", "", fmt.Errorf("head sha256 %s is not the pin %s", short(headSum), short(headPin))
	}
	h, err := gate.ParseHead(headBytes)
	if err != nil {
		return nil, nil, "", "", err
	}
	modelSum, _, err := fileSHA256(modelPath)
	if err != nil {
		return nil, nil, "", "", err
	}
	if modelPin != "" && modelSum != modelPin {
		return nil, nil, "", "", fmt.Errorf("model sha256 %s is not the pin %s", short(modelSum), short(modelPin))
	}
	m, err := gate.OpenModel(modelPath, threads)
	if err != nil {
		return nil, nil, "", "", err
	}
	if err := agree(m.InputShape(), m.OutputShape(), h); err != nil {
		if c, ok := m.(interface{ Close() }); ok {
			c.Close() // the X38g review: a model that did not agree was never closed
		}
		return nil, nil, "", "", err
	}
	return m, h, modelSum, headSum, nil
}

// agree checks the model shapes against the head: input [1, side, side, 3]
// (NHWC) and an output whose last dimension is the head's.
func agree(in, out []int, h *gate.Head) error {
	if len(in) != 4 || in[1] != h.InputSide || in[2] != h.InputSide || in[3] != 3 {
		return fmt.Errorf("model wants %v, head is %dpx", in, h.InputSide)
	}
	if len(out) == 0 {
		return fmt.Errorf("model has no output dims, head is %d", h.Dim)
	}
	if out[len(out)-1] != h.Dim {
		return fmt.Errorf("model emits %d dims, head is %d", out[len(out)-1], h.Dim)
	}
	return nil
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8084", "address to listen on")
	// US (T9 2026-11-01: "the unix sockets within the box"): -listen "" with -socket = the socket only
	socket := flag.String("socket", "", "US: also answer on this unix socket (the shared volume /sock); \"\" = no socket")
	probe := flag.String("probe-socket", "", "US: GET /healthz over this unix socket, print the answer and exit (the stack check)")
	modelPath := flag.String("model", "/assets/gate_pe_s16_f16w_384.tflite", "the gate .tflite")
	headPath := flag.String("head", "/assets/gate_head_pe_s16_384_deploy.json", "the gate head JSON")
	modelPin := flag.String("model-sha256", pinModel, "the model's required sha256 (\"\" = no check)")
	headPin := flag.String("head-sha256", pinHead, "the head's required sha256 (\"\" = no check)")
	threads := flag.Int("threads", 1, "LiteRT CPU threads (1; more load = more replicas)")
	events := flag.String("events", "/events", "folder for events-YYYYMMDD.jsonl (a volume)")
	queue := flag.Int("queue", 8, "the most calls that wait for the model before 503 busy")
	maxImage := flag.Int64("max-image", 5<<20, "the largest photo in bytes")
	shadowModel := flag.String("shadow-model", "", "a shadow gate .tflite (\"\" = no shadow)")
	shadowHead := flag.String("shadow-head", "", "the shadow gate head JSON")
	shadowModelPin := flag.String("shadow-model-sha256", "", "the shadow model's required sha256 (required with -shadow-model)")
	shadowHeadPin := flag.String("shadow-head-sha256", "", "the shadow head's required sha256 (required with -shadow-model)")
	flag.Parse()
	if *probe != "" {
		body, err := gate.ProbeUnix(*probe, "/healthz", 5*time.Second)
		fmt.Println(body)
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := os.MkdirAll(*events, 0o755); err != nil {
		log.Fatalf("events dir: %v", err)
	}
	c := gate.Config{Build: build, MaxImage: *maxImage, Queue: *queue, Recorder: gate.NewRecorder(*events, 4096)}
	start := time.Now()
	if m, h, ms, hs, err := open(*modelPath, *headPath, *modelPin, *headPin, *threads); err != nil {
		c.LoadError = err.Error()
		log.Printf("GATE UNAVAILABLE (fails closed): %v", err)
	} else {
		c.Model, c.Head, c.ModelSHA256, c.HeadSHA256 = m, h, ms, hs
		log.Printf("gate ready in %s: %s %s %dpx dim=%d thr=%v threads=%d model=%s head=%s build=%s",
			time.Since(start).Round(time.Millisecond), h.Encoder, h.Precision, h.InputSide, h.Dim, h.Threshold,
			*threads, short(ms), short(hs), build)
	}
	if *shadowModel != "" {
		c.Shadow = openShadow(*shadowModel, *shadowHead, *shadowModelPin, *shadowHeadPin)
	}
	srv := &http.Server{Addr: *listen, Handler: gate.New(c), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("v3rde-gate listening on %q socket=%q", *listen, *socket)
	if err := gate.Serve(srv, *listen, *socket); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// openShadow loads the shadow gate. It never changes the answer: a failure
// is logged and shown in /healthz. A shadow needs both pins.
func openShadow(model, head, modelPin, headPin string) *gate.Shadow {
	s := &gate.Shadow{}
	if modelPin == "" || headPin == "" {
		s.LoadError = "a shadow gate needs -shadow-model-sha256 and -shadow-head-sha256"
		log.Printf("SHADOW GATE DISABLED: %s", s.LoadError)
		return s
	}
	m, h, ms, hs, err := open(model, head, modelPin, headPin, 1)
	if err != nil {
		s.LoadError = err.Error()
		log.Printf("SHADOW GATE DISABLED: %v", err)
		return s
	}
	s.Model, s.Head, s.ModelSHA256, s.HeadSHA256 = m, h, ms, hs
	log.Printf("shadow gate ready: %s %s %dpx dim=%d thr=%v resample=%s model=%s head=%s",
		h.Encoder, h.Precision, h.InputSide, h.Dim, h.Threshold, h.Spec().Resample, short(ms), short(hs))
	return s
}
