// Command barcode runs the V3RDE barcode lookup.
//
//	barcode -listen 0.0.0.0:8083 -catalog /assets/products.sqlite -events /events
//
// It refuses a catalog whose sha256 is not the pinned one, or whose row
// count is not 374,760. A catalog that does not load leaves the service up,
// and it answers UNAVAILABLE: it fails closed.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	barcode "github.com/TwoThousandAndTwo2002/Icarus/barcode"
)

// build is the commit the binary was built from (-ldflags "-X main.build=<sha>"); events have it.
var build = "dev"

// pinCatalog is the sha256 of the catalog file (data/products.sqlite.sha256
// of the app before the Hackathon; the file on the box is that file).
const pinCatalog = "2e01d42471760eb5ae196ae7e2d6755d927a85426011f1140f642e46bc12897b"

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// load checks the catalog's sha256 (when pin is set), then opens it.
func load(path, pin string, rows int) (barcode.Store, string, error) {
	sum, err := fileSHA256(path)
	if err != nil {
		return nil, "", err
	}
	if pin != "" && sum != pin {
		return nil, "", fmt.Errorf("catalog sha256 %s is not the pin %s", sum[:12], pin[:min(12, len(pin))])
	}
	st, err := barcode.OpenStore(path, rows)
	if err != nil {
		return nil, "", err
	}
	return st, sum, nil
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8083", "address to listen on")
	catalog := flag.String("catalog", "/assets/products.sqlite", "the catalog SQLite file")
	pin := flag.String("catalog-sha256", pinCatalog, "the catalog's required sha256 (\"\" = no check)")
	rows := flag.Int("rows", barcode.ExpectedRows, "the required row count (0 = no check)")
	events := flag.String("events", "/events", "folder for events-YYYYMMDD.jsonl (a volume)")
	flag.Parse()

	if err := os.MkdirAll(*events, 0o755); err != nil {
		log.Fatalf("events dir: %v", err)
	}
	c := barcode.Config{Build: build, Recorder: barcode.NewRecorder(*events, 4096)}
	start := time.Now()
	if st, sum, err := load(*catalog, *pin, *rows); err != nil {
		c.LoadError = err.Error()
		log.Printf("BARCODE UNAVAILABLE (fails closed): %v", err)
	} else {
		c.Store, c.CatalogSHA256 = st, sum
		log.Printf("barcode ready in %s: %d rows, catalog=%s build=%s",
			time.Since(start).Round(time.Millisecond), st.Rows(), sum[:12], build)
	}
	srv := &http.Server{Addr: *listen, Handler: barcode.New(c), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("v3rde-barcode listening on %s", *listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
