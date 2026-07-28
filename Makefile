.PHONY: generate test vet lint openapi-check build css sbom image pot-plugin hooks

GO ?= go
OAPI_CODEGEN ?= $(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1
SYFT ?= syft
IMAGE ?= creatorr:local
VERSION ?= dev
REVISION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BGUTIL_POT_VERSION ?= 1.3.1
# Keep in sync with .github/workflows/ci.yml golangci-lint-action version.
GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT_IMAGE ?= golangci/golangci-lint:$(GOLANGCI_LINT_VERSION)

generate:
	$(OAPI_CODEGEN) -config api/oapi-codegen.yaml api/openapi.yaml

test:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

lint:
	docker run --rm -v "$(CURDIR):/app" -w /app $(GOLANGCI_LINT_IMAGE) golangci-lint run ./...

# Point this clone at versioned hooks under .githooks/ (repo-local git config).
hooks:
	git config core.hooksPath .githooks
	@chmod +x .githooks/pre-commit
	@echo "Enabled .githooks (pre-commit runs make lint + make test). Skip: SKIP_GITHOOKS=1"
openapi-check: generate
	@git diff --exit-code -- api/openapi.yaml internal/api/gen/ || (echo "OpenAPI generated code out of date; run make generate" && exit 1)

# Rebuild embedded daisyUI/Tailwind CSS and copy vendor JS (ECharts) from npm.
# Requires Node/npm. Commits should include updated internal/web/static/app.css
# and internal/web/static/vendor/echarts.min.js when those change.
css:
	npm --prefix internal/web/ui install
	npm --prefix internal/web/ui run build

build:
	$(GO) build -o bin/creatorr ./cmd/creatorr

# Install bgutil POT provider plugin for local Go (matches Docker bake; GPL-3.0).
# Compose sidecar: creatorr-po-token. Optional when not using POT.
pot-plugin:
	mkdir -p var/yt-dlp-plugins/bgutil
	curl -fsSL -o /tmp/bgutil-ytdlp-pot-provider.zip \
	  "https://github.com/Brainicism/bgutil-ytdlp-pot-provider/releases/download/$(BGUTIL_POT_VERSION)/bgutil-ytdlp-pot-provider.zip"
	unzip -qo /tmp/bgutil-ytdlp-pot-provider.zip -d var/yt-dlp-plugins/bgutil
	@echo "Installed POT plugin under var/yt-dlp-plugins/bgutil (set CREATORR_POT_PROVIDER_URL to your provider)."

# Repo SBOM only (Go modules + committed tree). Needs syft on PATH.
sbom:
	$(SYFT) dir:. -o cyclonedx-json=sbom.cdx.json

image:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg REVISION=$(REVISION) \
		--build-arg BGUTIL_POT_VERSION=$(BGUTIL_POT_VERSION) \
		-t $(IMAGE) .
