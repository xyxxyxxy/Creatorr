# scripts

Dev helpers for Creatorr.

## Compose (dev)

[`compose`](compose) wraps `docker compose -f docker-compose.dev.yml` (that file **includes** production [`docker-compose.yml`](../docker-compose.yml)), plus [`docker-compose.override.dev.yml`](../docker-compose.override.dev.yml) when that file exists:

```bash
./scripts/compose up -d --build
./scripts/compose logs -f
```

Or directly:

```bash
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

**FlareSolverr:** Compose runs `creatorr-flaresolverr` (Chrome; notable RAM). Set `CREATORR_FLARESOLVERR_URL` (default `http://creatorr-flaresolverr:8191`); enable **Use FlareSolverr** per host under Settings → Queue / Domains.
