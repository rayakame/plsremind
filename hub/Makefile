.PHONY: test generate lint build

test:
	go test ./...

generate:
	sqlc generate

lint:
	go vet ./...
	sqlc diff

build:
	go build -o bin/plsremind ./cmd/plsremind
