REDOCLY := npx --yes @redocly/cli@2.76.0
SPEC := ../docs/api/openapi.yaml

.PHONY: test test-race generate lint build web docs

test:
	go test ./...

test-race:
	go test -race ./...

generate:
	sqlc generate

lint:
	go vet ./...
	sqlc diff
	$(REDOCLY) lint $(SPEC) --config ../docs/api/redocly.yaml

build:
	go build -o bin/plsremind ./cmd/plsremind

# Builds the dashboard into the place the binary embeds it from. Needs "npm ci" at the root.
web:
	npm run build --prefix ../web
	find internal/server/webdist -mindepth 1 ! -name .gitkeep -delete
	cp -R ../web/dist/. internal/server/webdist/
	rm -rf internal/server/webdist/.vite

docs:
	$(REDOCLY) build-docs $(SPEC) --config ../docs/api/redocly.yaml --output ../docs/api/index.html
