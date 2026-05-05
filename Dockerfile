# syntax=docker/dockerfile:1.7
# ---- builder ----------------------------------------------------------------
ARG GO_VERSION=1.25

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src

# Layer-cache deps: changes here invalidate only the next RUN.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Cross-compile via Go's native GOOS/GOARCH. When invoked through `docker
# buildx build --platform linux/amd64,linux/arm64 .` BuildKit injects
# TARGETOS/TARGETARCH automatically; with the legacy `docker build` they're
# unset and we fall back to linux/amd64.
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/make-it-transparent \
      .

# ---- runtime ----------------------------------------------------------------
# Distroless static gives us:
#   - a nonroot user (uid 65532) baked in
#   - ca-certificates, /tmp, /etc/passwd
#   - no shell, no package manager — minimal attack surface
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="make-it-transparent"
LABEL org.opencontainers.image.description="Pick a color in any image and make it transparent. Lossless PNG out, no signup, runs as a single static binary."
LABEL org.opencontainers.image.url="https://transparent.gabrielpires.com"
LABEL org.opencontainers.image.source="https://github.com/gabrielpires/make-it-transparent"
LABEL org.opencontainers.image.licenses="PolyForm-Noncommercial-1.0.0"
LABEL org.opencontainers.image.authors="Gabriel Pires <eu@gabrielpires.com.br>"

COPY --from=builder /out/make-it-transparent /usr/local/bin/make-it-transparent

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/make-it-transparent"]
CMD ["-port=:8080"]
