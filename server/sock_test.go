package analyze

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixTransport(t *testing.T) {
	dir := t.TempDir()
	l, err := net.Listen("unix", filepath.Join(dir, "x.sock"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Method+" "+r.URL.Path+" "+r.Header.Get("Cookie"))
	})}
	go func() { _ = srv.Serve(l) }()
	defer srv.Close()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.RegisterProtocol(UnixScheme, UnixTransport(dir))
	c := &http.Client{Transport: tr}
	req, _ := http.NewRequest(http.MethodGet, "http+unix://x.sock/api/auth/me", nil)
	req.Header.Set("Cookie", "s=1")
	for i := 0; i < 3; i++ { // the connection is used again
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(b) != "GET /api/auth/me s=1" {
			t.Fatalf("answer %q", b)
		}
	}
	for _, bad := range []string{"http+unix://../api", "http+unix://none.sock/x"} {
		if resp, err := c.Get(bad); err == nil {
			resp.Body.Close()
			t.Errorf("%s: no error", bad)
		}
	}
	// a redirect to a socket is refused
	redir := httptest.NewServer(http.RedirectHandler("http+unix://x.sock/api/auth/me", http.StatusFound))
	defer redir.Close()
	if resp, err := c.Get(redir.URL); err == nil {
		resp.Body.Close()
		t.Error("a redirect reached the socket")
	} else if !strings.Contains(err.Error(), "no redirect to a socket") {
		t.Errorf("redirect: %v", err)
	}
	// a plain http URL is not changed
	if _, err := c.Get("http://127.0.0.1:1/x"); err == nil || strings.Contains(err.Error(), UnixScheme) {
		t.Errorf("plain http: %v", err)
	}
}
