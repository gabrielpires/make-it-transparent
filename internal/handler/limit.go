package handler

import "net/http"

// Concurrency caps the number of in-flight requests for the wrapped handler.
// Excess requests get 503 with Retry-After: 1 instead of piling on memory and
// goroutines. Use it on expensive endpoints (image decoding) to prevent a
// single client from exhausting the server's RAM.
func Concurrency(maxInFlight int, next http.Handler) http.Handler {
	if maxInFlight <= 0 {
		return next
	}
	sem := make(chan struct{}, maxInFlight)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "server busy, please retry")
		}
	})
}
