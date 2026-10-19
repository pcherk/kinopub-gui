# KinoPub GUI — reproducible Synology build from the maintained source fork.
# No in-build Go source patches, sed substitutions or local auth overlays.
FROM golang:1.26.3-bookworm AS builder

ARG SOURCE_REF=synology-v1.1.0-6
RUN apt-get update && \
    apt-get install -y --no-install-recommends git ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git clone -q --depth 1 --branch "${SOURCE_REF}" \
    https://github.com/pcherk/kinopub-gui.git .

RUN CGO_ENABLED=0 go test ./internal/gui ./internal/services/kinopubapi
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${SOURCE_REF}" \
    -o /out/kinopub-gui ./cmd/kinopub-gui

FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates ffmpeg curl tini && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/kinopub-gui /usr/local/bin/kinopub-gui

# Config/download volumes may be mounted from Synology and MUST be writable by UID 1026.
RUN mkdir -p /data/Downloads/kinopub && \
    useradd --uid 1026 --gid 100 --home-dir /data \
      --no-create-home --shell /usr/sbin/nologin kinopub && \
    chown -R 1026:100 /data

ENV HOME=/data \
    XDG_CONFIG_HOME=/data/.config \
    KINOPUB_REMOTE_USER=kinopub \
    KINOPUB_REMOTE_PASSWORD_FILE=/run/secrets/kinopub_password

WORKDIR /data
USER 1026:100
EXPOSE 8765

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl --fail --silent http://127.0.0.1:8765/api/health >/dev/null || exit 1

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/kinopub-gui"]
CMD ["-addr", "0.0.0.0:8765", "-no-open"]
