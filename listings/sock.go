package listings

// US (T9 2026-11-01: "the unix sockets within the box"): a base URL
// "http+unix://<name>" reaches the unix socket <SockDir>/<name> (the named
// volume v3rde_sock at /sock), for example -auth http+unix://auth.sock.
// EnableUnixURLs adds the scheme to http.DefaultTransport, which the clients
// of the other services use; a plain http:// URL works as before. This file
// is the same in server and listings (TestClientSockFileSame).

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// UnixScheme is the URL scheme of a unix socket base URL.
const UnixScheme = "http+unix"

// SockDir is the folder of the sockets in the containers (the volume v3rde_sock).
const SockDir = "/sock"

type unixRT struct {
	dir string
	mu  sync.Mutex
	ts  map[string]*http.Transport
}

func (u *unixRT) transport(path string) *http.Transport {
	u.mu.Lock()
	defer u.mu.Unlock()
	if t, ok := u.ts[path]; ok {
		return t
	}
	t := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
		MaxIdleConns: 16, MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second,
	}
	u.ts[path] = t
	return t
}

func (u *unixRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Response != nil { // a redirect (from any host) never leads to a socket
		return nil, fmt.Errorf("%s: no redirect to a socket", UnixScheme)
	}
	name := r.URL.Host
	if name == "" || strings.ContainsAny(name, `/\:`) || name == "." || name == ".." {
		return nil, fmt.Errorf("%s: bad socket name %q", UnixScheme, name)
	}
	r2 := r.Clone(r.Context())
	r2.URL.Scheme, r2.URL.Host, r2.Host = "http", "unix", "unix"
	return u.transport(filepath.Join(u.dir, name)).RoundTrip(r2)
}

// UnixTransport is a RoundTripper for http+unix URLs whose sockets are in dir.
func UnixTransport(dir string) http.RoundTripper {
	return &unixRT{dir: dir, ts: map[string]*http.Transport{}}
}

var enableOnce sync.Once

// EnableUnixURLs adds http+unix (sockets in dir) to http.DefaultTransport.
// Call it once in main, before any client is made.
func EnableUnixURLs(dir string) {
	enableOnce.Do(func() {
		http.DefaultTransport.(*http.Transport).RegisterProtocol(UnixScheme, UnixTransport(dir))
	})
}
