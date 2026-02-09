package connectutil

import (
	"net/http"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// H2CHandler wraps an http.Handler with h2c support for unencrypted HTTP/2.
// This is required for Connect RPC bidi streaming without TLS.
func H2CHandler(handler http.Handler) http.Handler {
	return h2c.NewHandler(handler, &http2.Server{
		MaxConcurrentStreams: 250,
		MaxReadFrameSize:    1 << 20,
	})
}
