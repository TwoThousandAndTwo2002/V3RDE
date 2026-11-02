package gate

// US (T9 2026-11-01: "the unix sockets within the box"): the service can
// also answer on a unix socket in a folder that only the containers of the
// stack mount (the named volume v3rde_sock at /sock). This file is the same
// in auth, gate, barcode and telemetry (TestSockFileSame in each module
// checks its sha256 against the others' through the repo).

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// ListenUnix opens a unix socket at path, mode 0660. A socket file left by an
// earlier run is removed first; any other file at path is an error.
func ListenUnix(path string) (net.Listener, error) {
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o660); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// ProbeUnix is GET urlPath (the health route) over the socket at path (the
// stack check runs the binary with -probe-socket in its own container): the
// body, or an error when there is no answer or it is not 200.
func ProbeUnix(path, urlPath string, timeout time.Duration) (string, error) {
	c := &http.Client{Timeout: timeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}}}
	resp, err := c.Get("http://unix" + urlPath)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return string(b), fmt.Errorf("healthz: HTTP %d", resp.StatusCode)
	}
	return string(b), nil
}

// Serve runs srv on the TCP address (when not "") and on the unix socket
// (when not ""), and returns the first error. Neither is an error.
func Serve(srv *http.Server, listen, socket string) error {
	if listen == "" && socket == "" {
		return fmt.Errorf("no -listen address and no -socket")
	}
	errc := make(chan error, 2)
	if socket != "" {
		l, err := ListenUnix(socket)
		if err != nil {
			return err
		}
		go func() { errc <- srv.Serve(l) }()
	}
	if listen != "" {
		srv.Addr = listen
		go func() { errc <- srv.ListenAndServe() }()
	}
	return <-errc
}
