# scripts

Dev helpers for Creatorr.

## Compose (dev)

[`sync-dev-public-url.sh`](sync-dev-public-url.sh) detects this machine’s primary LAN IPv4 and writes `CREATORR_PUBLIC_BASE_URL=http://<ip>:8787` into project `.env` (gitignored), tagged with `# creatorr-dev-auto-public-base-url`. On boot Creatorr may seed Settings `external_base_url` from that env when Settings is empty. Prefer **Settings → Library → Streaming → External Creatorr URL** afterward. Re-running refreshes the IP when that marker is present; a hand-edited URL without the marker is left alone.

[`compose`](compose) runs the sync script, then `docker compose -f docker-compose.dev.yml` (that file **includes** production [`docker-compose.yml`](../docker-compose.yml)), plus [`docker-compose.override.dev.yml`](../docker-compose.override.dev.yml) when that file exists:

```bash
./scripts/compose up -d --build
./scripts/compose logs -f
```

Or:

```bash
./scripts/sync-dev-public-url.sh
docker compose -f docker-compose.dev.yml up -d --build
```

Production (image pull only): `docker compose up -d` with [`docker-compose.yml`](../docker-compose.yml) alone.

## yt-dlp plugins

Mount sibling extractor repos under `/yt-dlp-plugins/` (see compose comments, [`docker-compose.override.dev.example.yml`](../docker-compose.override.dev.example.yml), and [`docs/ytdlp.md`](../docs/ytdlp.md)):

```bash
# example: sibling checkout next to Creatorr/
# ../Creatorr-handler-example.com → /yt-dlp-plugins/example
```

Smoke: after boot, `yt-dlp --plugin-dirs … --list-extractors` should list the plugin extractors when those mounts are present.

**PO Token plugin (local Go):** `make pot-plugin` installs the bgutil provider zip under `var/yt-dlp-plugins/bgutil`. Compose already runs `creatorr-po-token`; set `CREATORR_POT_PROVIDER_URL` (default `http://creatorr-po-token:4416`).
