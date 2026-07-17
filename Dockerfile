FROM golang:1.26.5-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/plex-orphan-watcher ./cmd/plex-orphan-watcher

FROM alpine:3.23

LABEL org.opencontainers.image.source="https://github.com/IamStubborN/plex-orphan-watcher" \
      org.opencontainers.image.description="Safely remove orphaned Plex movie and TV sidecars" \
      org.opencontainers.image.licenses="MIT"

RUN addgroup -g 1000 watcher \
    && adduser -D -H -u 1000 -G watcher watcher
COPY --from=build /out/plex-orphan-watcher /usr/local/bin/plex-orphan-watcher

USER watcher
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/plex-orphan-watcher"]
