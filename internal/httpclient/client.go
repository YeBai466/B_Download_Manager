// Package httpclient builds proxy-aware *http.Client instances tuned for
// long-running segmented downloads.
package httpclient

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/yebai/b-download-manager/internal/proxy"
)

// New returns an http.Client configured with the given proxy settings. The
// client has no overall timeout (downloads can be large); instead it relies on
// dial/idle timeouts and request-context cancellation for control.
//
// HTTP/2 is deliberately DISABLED. A segmented download opens many parallel
// range requests; over HTTP/2 they would all multiplex onto a single TCP
// connection and share one congestion window, so the transfer is throttled by
// one connection's slow-start and any per-connection server limit. Forcing
// HTTP/1.1 gives each segment its own TCP connection — the multi-connection
// model IDM/aria2 use to ramp up fast and saturate the link.
func New(p proxy.Settings) (*http.Client, error) {
	tr := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2: false,
		// A non-nil empty map disables HTTP/2 upgrade via ALPN; servers fall
		// back to HTTP/1.1 (which every HTTP server supports).
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64, // allow many keep-alive conns to one host (up to 32 segments + reuse)
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		// Larger socket buffers cut syscall overhead and improve throughput on
		// high bandwidth-delay-product links.
		WriteBufferSize: 64 * 1024,
		ReadBufferSize:  64 * 1024,
	}
	if err := proxy.Apply(tr, p); err != nil {
		return nil, err
	}
	return &http.Client{Transport: tr}, nil
}
