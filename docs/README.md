# Creatorr product docs

Topic specs for product behavior. Terminology glossary: [domain-model.md](domain-model.md) § Terminology (agent naming pointers in [`AGENTS.md`](../AGENTS.md)).

| Doc | Covers |
| --- | --- |
| [domain-model.md](domain-model.md) | Terminology glossary, entities, video statuses, History, task statuses |
| [scan-and-queue.md](scan-and-queue.md) | Jobs vs tasks, gradual scan, task kinds, domain queue |
| [ytdlp.md](ytdlp.md) | In-tree yt-dlp, image binary, plugins |
| [settings.md](settings.md) | Settings keys, stats sampling, library bootstrap |
| [download-and-library.md](download-and-library.md) | Download pipeline, remux (video MKV / audio MKA + why), retention, file sync, series/domain gates |
| [ui.md](ui.md) | HTMX + daisyUI UI/CSS contract, shared patterns, setting Info/Hint |
| [`api/openapi.yaml`](../api/openapi.yaml) | REST contract (source of truth) |
| [`AGENTS.md`](../AGENTS.md) | Always-on agent contract (hard rules, architecture map, workflow) |

OpenAPI is not duplicated here. Out-of-OpenAPI product behavior lives in the topic docs above; AGENTS stays short - load topic docs (and `ui.md` for UI work) only when needed.
