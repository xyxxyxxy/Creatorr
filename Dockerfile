# Creatorr - Go runtime (yt-dlp, ffmpeg, Deno as sidecars in-image)
# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY api ./api
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/creatorr ./cmd/creatorr

# Fetch arch-specific sidecars; unzip/curl stay out of the final image.
FROM debian:bookworm-slim AS tools
ARG TARGETARCH
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl unzip \
  && mkdir -p /out \
  && case "$TARGETARCH" in \
       amd64) YTDLP=yt-dlp_linux; DENO=deno-x86_64-unknown-linux-gnu.zip ;; \
       arm64) YTDLP=yt-dlp_linux_aarch64; DENO=deno-aarch64-unknown-linux-gnu.zip ;; \
       *) echo "unsupported TARGETARCH=$TARGETARCH (need amd64 or arm64)" >&2; exit 1 ;; \
     esac \
  && curl -fsSL -o /out/yt-dlp \
       "https://github.com/yt-dlp/yt-dlp/releases/latest/download/${YTDLP}" \
  && chmod a+rx /out/yt-dlp \
  && curl -fsSL -o /tmp/deno.zip \
       "https://github.com/denoland/deno/releases/latest/download/${DENO}" \
  && unzip -j -d /out /tmp/deno.zip deno \
  && chmod a+rx /out/deno \
  && rm -rf /var/lib/apt/lists/* /tmp/*

FROM debian:bookworm-slim AS runtime

ARG VERSION=dev
ARG REVISION=unknown

# curl: HEALTHCHECK + compose healthcheck. ca-certificates: HTTPS for yt-dlp / handlers.
# ffmpeg: remux/merge. No python - yt-dlp_linux is a standalone binary.
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    ffmpeg \
  && rm -rf /var/lib/apt/lists/* \
  && groupadd --gid 1000 creatorr \
  && useradd --uid 1000 --gid 1000 --home-dir /app --no-create-home --shell /usr/sbin/nologin creatorr \
  && mkdir -p /data /cache /media/library /media/import /yt-dlp-plugins \
  && chown -R creatorr:creatorr /data /cache /media /yt-dlp-plugins

COPY --from=build /out/creatorr /usr/local/bin/creatorr
COPY --from=tools /out/yt-dlp /usr/local/bin/yt-dlp
COPY --from=tools /out/deno /usr/local/bin/deno

LABEL org.opencontainers.image.title="Creatorr" \
      org.opencontainers.image.description="Sonarr-shaped creator VOD daemon" \
      org.opencontainers.image.source="https://github.com/xyxxyxxy/Creatorr" \
      org.opencontainers.image.url="https://github.com/xyxxyxxy/Creatorr" \
      org.opencontainers.image.licenses="Unlicense" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

# Paths are fixed in-process when /data exists (see internal/config). No path env vars.
ENV CREATORR_PORT=8787

WORKDIR /app
USER 1000:1000
EXPOSE 8787

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8787/api/health >/dev/null || exit 1

CMD ["creatorr"]
