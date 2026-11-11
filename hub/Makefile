REDOCLY := npx --yes @redocly/cli@2.76.0
SPEC := ../docs/api/openapi.yaml

.PHONY: test generate lint build docs

test:
	go test ./...

generate:
	sqlc generate

lint:
	go vet ./...
	sqlc diff
	$(REDOCLY) lint $(SPEC) --config ../docs/api/redocly.yaml

build:
	go build -o bin/plsremind ./cmd/plsremind

docs:
	$(REDOCLY) build-docs $(SPEC) --config ../docs/api/redocly.yaml --output ../docs/api/index.html
