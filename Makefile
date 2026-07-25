.PHONY: generate test vet lint openapi-check build css sbom image

GO ?= go
OAPI_CODEGEN ?= $(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1
SYFT ?= syft
IMAGE ?= creatorr:local
VERSION ?= dev
REVISION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

generate:
	$(OAPI_CODEGEN) -config api/oapi-codegen.yaml api/openapi.yaml

test:
	$(GO) test -race -count=1 ./...

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

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

# Repo SBOM only (Go modules + committed tree). Needs syft on PATH.
sbom:
	$(SYFT) dir:. -o cyclonedx-json=sbom.cdx.json

image:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg REVISION=$(REVISION) \
		-t $(IMAGE) .
