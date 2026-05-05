# Contributing

Thanks for your interest. This is a small, single-purpose tool. Contributions
should keep it that way: simple, fast, and obvious.

## Ground rules

- **Single purpose**: this app turns one (or a few) chosen color(s) transparent.
  Features that don't directly serve that — background removal AI, full image
  editors, format converters, etc. — belong elsewhere.
- **Lossless out**: the output is always PNG with `png.NoCompression`. Don't
  introduce a path that re-encodes lossy or compresses the colour data.
- **No telemetry, no accounts, no upload retention**: requests are processed in
  memory and discarded. Any change that adds state has to clear a high bar.

## License of contributions

By submitting a PR you agree that your contribution is licensed under the same
terms as the project: [PolyForm Noncommercial 1.0.0](LICENSE). Don't paste in
code under a more restrictive (or commercial-only) license.

## Local setup

```bash
git clone <repo>
cd make-it-transparent
go mod download
go run .          # http://localhost:8080
```

## Before submitting

```bash
go test -race ./...
go vet ./...
gofmt -l . | tee /dev/stderr | (! read)   # fail if anything's not gofmt'd
```

If you have it installed:

```bash
golangci-lint run
govulncheck ./...
```

## Style

- Follow [Effective Go](https://go.dev/doc/effective_go) and `gofmt`.
- Small files, small functions, small interfaces.
- Wrap errors with `fmt.Errorf("operation: %w", err)`.
- Tests next to the code (`_test.go`); table-driven where it fits.

## Reporting bugs

Open an issue with:

1. What you did (curl / screenshot / steps).
2. What you expected.
3. What happened, including the request ID from the response (look for
   `X-Request-Id` in the dev tools network tab — it shows up in server logs).
4. Image format & dimensions if relevant.

For security issues, follow [SECURITY.md](SECURITY.md) instead.
