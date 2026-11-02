package gate

import (
	"crypto/sha256"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeUnixAndProbe(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "s.sock")
	if err := os.WriteFile(sock, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListenUnix(sock); err == nil || !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("a plain file at the path: %v", err)
	}
	os.Remove(sock)
	// a socket left by an earlier run is removed
	l, err := ListenUnix(sock)
	if err != nil {
		t.Fatal(err)
	}
	l.(interface{ SetUnlinkOnClose(bool) }).SetUnlinkOnClose(false)
	l.Close()
	if _, err := os.Lstat(sock); err != nil {
		t.Fatalf("the stale socket must stay for this test: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ok":true}`)) })
	mux.HandleFunc("/down", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	srv := &http.Server{Handler: mux}
	go func() { _ = Serve(srv, "", sock) }()
	defer srv.Close()
	var body string
	for i := 0; i < 50; i++ {
		if body, err = ProbeUnix(sock, "/healthz", time.Second); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || body != `{"ok":true}` {
		t.Fatalf("probe %q %v", body, err)
	}
	if _, err := ProbeUnix(sock, "/down", time.Second); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("a 503 must be an error: %v", err)
	}
	fi, _ := os.Lstat(sock)
	if fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o660 {
		t.Fatalf("socket mode %v", fi.Mode())
	}
	if _, err := ProbeUnix(filepath.Join(dir, "none.sock"), "/healthz", time.Second); err == nil {
		t.Fatal("probe of no socket")
	}
	if err := Serve(&http.Server{}, "", ""); err == nil {
		t.Fatal("no listen and no socket")
	}
}

// sock.go is one file in four modules (the package line aside).
func TestSockFileSame(t *testing.T) {
	norm := func(p string) (string, bool) {
		b, err := os.ReadFile(p)
		if err != nil {
			return "", false
		}
		s := string(b)
		s = s[strings.Index(s, "\n"):]
		h := sha256.Sum256([]byte(s))
		return string(h[:]), true
	}
	mine, ok := norm("sock.go")
	if !ok {
		t.Fatal("no sock.go")
	}
	n := 0
	for _, m := range []string{"auth", "gate", "barcode", "telemetry"} {
		other, ok := norm(filepath.Join("..", m, "sock.go"))
		if !ok {
			continue // an image build has only this module
		}
		n++
		if other != mine {
			t.Errorf("../%s/sock.go is not the same as this sock.go", m)
		}
	}
	if n == 0 {
		t.Log("no other module here")
	}
}
