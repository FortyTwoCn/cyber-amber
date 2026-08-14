# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.26.6-bookworm AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/cyber-amber ./cmd/cyber-amber

FROM debian:bookworm-slim
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates ffmpeg fontconfig fonts-noto-cjk libass9 tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && fc-cache -f \
    && groupadd --system --gid 10001 cyberamber \
    && useradd --system --uid 10001 --gid cyberamber --home-dir /nonexistent --shell /usr/sbin/nologin cyberamber \
    && install -d -o cyberamber -g cyberamber -m 0700 /config /data /cache /tmp/cyber-amber
COPY --from=build --chmod=0555 /out/cyber-amber /usr/local/bin/cyber-amber
USER 10001:10001
VOLUME ["/config", "/data", "/cache"]
EXPOSE 8080
ENV APP_ADDR=:8080 DATA_DIR=/data CACHE_DIR=/cache TEMP_DIR=/tmp/cyber-amber DATABASE_PATH=/data/cyber-amber.db
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["cyber-amber", "-config", "/config/config.yaml", "-healthcheck"]
STOPSIGNAL SIGTERM
ENTRYPOINT ["cyber-amber"]
CMD ["-config", "/config/config.yaml"]
