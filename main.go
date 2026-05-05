// Command make-it-transparent serves the colour-to-alpha web app: a
// httpkit-backed HTTP server with the frontend embedded via go:embed.
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gabrielpires/httpkit"
	"github.com/gabrielpires/make-it-transparent/internal/handler"
)

//go:embed web/*
var webFS embed.FS

func main() {
	port := flag.String("port", ":8080", "listen address, e.g. :8080")
	flag.Parse()

	staticFS, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("embed sub fs: %v", err)
	}

	s, err := httpkit.NewServer(
		httpkit.WithPort(*port),
		httpkit.WithReadTimeout(30*time.Second),
		httpkit.WithWriteTimeout(60*time.Second),
		httpkit.WithIdleTimeout(120*time.Second),
	)
	if err != nil {
		log.Fatalf("server: %v", err)
	}

	s.Middleware(httpkit.RequestID)
	s.Middleware(loggingMiddleware)
	s.Middleware(securityHeaders)

	// Cap concurrent in-flight decode jobs. Each decode peaks ~150 MB at the
	// edge of MaxPixels, so 8 in flight ≈ 1.2 GB worst-case. Excess gets 503.
	s.Handle("/api/transparent", handler.Concurrency(8, handler.Transparent(handler.DefaultLimits())))
	s.Handle("/", staticHandler(staticFS))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log.Printf("make-it-transparent listening on %s", *port)
	if err := s.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("start: %v", err)
	}
}

// loggingMiddleware logs method, path, status, and duration with the request ID.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		// %q on path/method neutralises CR/LF and other control bytes that
		// would otherwise let an attacker forge log lines (CWE-117). gosec's
		// taint analysis can't see that the formatter already escapes.
		// #nosec G706 -- %q escapes control chars in user-controlled fields.
		log.Printf("%q %q %d %s id=%s",
			r.Method, r.URL.Path, sw.status, time.Since(start),
			httpkit.RequestIDFromContext(r.Context()),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// staticHandler serves an explicit allowlist of paths from the embedded FS.
// Anything not on the list returns 404. This keeps a future accidental commit
// of a sensitive file under web/ from being served, and makes the routing
// surface trivial to audit.
func staticHandler(staticFS fs.FS) http.Handler {
	allowed := map[string]struct{}{
		"/":            {},
		"/favicon.svg": {},
		"/og.png":      {},
		"/robots.txt":  {},
		"/sitemap.xml": {},
	}
	fileServer := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, ok := allowed[r.URL.Path]; !ok {
			http.NotFound(w, r)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, ".svg"), strings.HasSuffix(r.URL.Path, ".png"):
			w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
		case r.URL.Path == "/":
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// securityHeaders sets defence-in-depth HTTP headers. CSP is applied only to
// HTML so that the JSON/PNG endpoints aren't constrained. Tailwind Play CDN
// and Google Fonts are explicitly allowed because the embedded UI loads them.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "geolocation=(), camera=(), microphone=(), payment=()")
		// Apply CSP only to the document responses; images/JSON don't need it
		// and a tight CSP on those breaks nothing useful.
		htmlMethod := r.Method == http.MethodGet || r.Method == http.MethodHead
		if htmlMethod && (r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html")) {
			h.Set("Content-Security-Policy",
				"default-src 'self'; "+
					"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com; "+
					"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
					"font-src 'self' https://fonts.gstatic.com; "+
					"img-src 'self' data: blob:; "+
					"connect-src 'self' https://api.github.com; "+
					"frame-ancestors 'none'; "+
					"base-uri 'self'; "+
					"form-action 'self'")
		}
		next.ServeHTTP(w, r)
	})
}
