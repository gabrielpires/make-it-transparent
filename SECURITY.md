# Security Policy

## Reporting a vulnerability

If you find a security issue, **please don't open a public issue**. Email me
directly:

> **eu@gabrielpires.com.br** — subject: `[security] make-it-transparent: …`

Include:

- A description of the issue and its impact.
- Reproduction steps or a proof of concept.
- Whether you're OK being credited in the fix announcement.

I aim to acknowledge within 72 hours and ship a fix within 14 days for anything
exploitable. If the issue is in `httpkit` (the HTTP layer), see its own
[SECURITY.md](https://github.com/gabrielpires/httpkit/blob/main/SECURITY.md).

## Scope

In scope:

- The Go server (`main.go`, `internal/`).
- The embedded frontend (`web/`).
- Anything served at `transparent.gabrielpires.com`.

Out of scope:

- Third-party services we link to (Google Fonts, Tailwind CDN, GitHub API).
  Report those upstream.
- Reports that boil down to "user uploaded a malicious image and saw an error"
  — that's the expected behaviour.

---

## Threat model

The server has exactly one job: receive a multipart upload, decode an image,
flip some pixels' alpha, and write back a PNG. Every defence below exists so
that this single job stays the *only* thing it can do.

### Attacker capabilities

We assume an attacker can:

- Send arbitrary HTTP requests to any reachable endpoint.
- Upload arbitrarily-crafted "image" files.
- Reach the server directly (not via reverse proxy) — i.e. all per-request
  enforcement happens in-app, not in the proxy. The proxy is treated as
  optional defence-in-depth, not load-bearing.

We do **not** assume:

- Authenticated sessions (there are none).
- Outbound network access from the server (we make no server-side HTTP calls).
- Persistent storage (we hold nothing across requests).

### Attack vectors and what stops them

| # | Attack | Defence |
|---|---|---|
| 1 | **Decompression bomb**: tiny PNG that expands to GBs. | `image.DecodeConfig` peeks dimensions before allocation; rejected with `413` if `width × height > MaxPixels` (16 MP). |
| 2 | **Format smuggling**: register a new decoder later, attacker uploads format we never planned to handle. | `allowedFormats` map is a hard whitelist of `png`/`jpeg`/`gif`. Any other format returned by `DecodeConfig` is rejected with `415`. |
| 3 | **Native-decoder RCE** (e.g. ImageTragick). | We use only Go-stdlib decoders — pure Go, no `os/exec`, no native library. `govulncheck` runs in CI. |
| 4 | **Filename header injection / path traversal** via `Content-Disposition`. | `downloadName` strips `/`, `\`, control chars, quotes, backslashes, normalises empty/`.`/`..`, caps at 80 chars. Unit-tested. |
| 5 | **XSS via uploaded image content**. | Re-encoded as PNG; response is `Content-Type: image/png` + `X-Content-Type-Options: nosniff`. Never echoed as HTML. Errors come back as JSON, not as the user-supplied bytes. |
| 6 | **Filesystem read via path traversal** in URL (e.g. `/../main.go`). | `staticHandler` serves an explicit allowlist (`/`, `/favicon.svg`, `/og.png`, `/robots.txt`, `/sitemap.xml`). Anything else is `404`. The embedded FS itself contains nothing else. |
| 7 | **Memory/CPU exhaustion** by holding many connections open. | `Concurrency(8, …)` middleware caps simultaneous decode jobs; excess gets `503` with `Retry-After`. Per-request timeouts are 30s read / 60s write. Per-request body cap is 25 MiB. |
| 8 | **Multipart-spool disk fill**. | `MaxBytesReader` caps the body at 25 MiB *before* it can spool. With concurrency cap of 8, peak temp-disk usage is ≈ 200 MiB. |
| 9 | **CSRF**. | No auth, no cookies, no privileged action — there's nothing for CSRF to escalate. |
| 10 | **Open redirect / SSRF**. | The server makes no outbound HTTP. The frontend's GitHub-stars fetch happens client-side from the browser's origin. |
| 11 | **Trusted proxy header spoofing** (`X-Forwarded-For`, `X-Real-IP`, etc.). | We don't trust any inbound header for security decisions. `X-Request-ID` is reused if present, but only as a logging label. |
| 12 | **Content-Security-Policy bypass**. | CSP applied to HTML responses: `default-src 'self'`, scripts limited to `cdn.tailwindcss.com`, fonts to `fonts.gstatic.com`, `connect-src` to `api.github.com`. `frame-ancestors 'none'`, `base-uri 'self'`, `form-action 'self'`. |
| 13 | **Clickjacking**. | `X-Frame-Options: DENY` + CSP `frame-ancestors 'none'`. |
| 14 | **MIME sniffing**. | `X-Content-Type-Options: nosniff` on every response. |
| 15 | **Information leak via referrer**. | `Referrer-Policy: strict-origin-when-cross-origin`. |
| 16 | **Unwanted permission prompts** (geolocation, mic, camera). | `Permissions-Policy` denies them all. |
| 17 | **Method confusion** on static handler. | Static handler accepts only `GET`/`HEAD`; everything else gets `405`. |

### Hardening already in place

- `http.MaxBytesReader` (25 MiB).
- `transparent.MaxPixels` decompression-bomb guard.
- `allowedFormats` hard whitelist.
- `Concurrency(8, …)` semaphore on the decode endpoint.
- Strong response headers (CSP, XCTO, XFO, Referrer-Policy, Permissions-Policy).
- Allowlisted static-file routing.
- Filename sanitisation.
- 30/60/120 second read/write/idle timeouts.
- No persistence, no telemetry, no outbound calls.
- CI runs `govulncheck` and `gosec` on every push.
- Tests assert that an ELF disguised as `.png` is rejected, that an HTML
  payload doesn't get echoed, and that the concurrency cap returns `503`.

### Out-of-band considerations

If you self-host:

- **Run as non-root.** A dedicated unprivileged user is enough.
- **Bind to localhost** behind a reverse proxy (Caddy, nginx, Cloudflare, …)
  unless you have a specific reason to expose the listener directly.
- **Terminate TLS** at the proxy or via `httpkit.WithTLS(cert, key)`.
- **Add a global rate limit at the edge** if you expect abuse — there's no
  per-IP rate limit in-app.
- If you put it behind a CDN that caches by URL, the responses for `/api/*`
  set `Cache-Control: no-store` so they will not be cached.
- The `X-Request-ID` echoes the value sent by the client (or generates one).
  If your environment treats request IDs as PII, strip them when shipping
  logs.

### Things explicitly NOT defended

- **Application-layer DDoS** beyond what timeouts + concurrency cap can absorb.
  That's the reverse proxy / CDN's job.
- **Compromised dependency tree.** Go's module-sum file (`go.sum`) catches
  tampering of pinned versions, but a malicious upstream release would not be
  caught here without an SBOM/signing pipeline.
- **Side-channels** (timing, cache). Not a meaningful threat for a stateless
  no-secret-handling tool, so we don't try.
