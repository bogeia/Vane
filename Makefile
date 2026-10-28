.PHONY: init build test generate

init:
	go install github.com/google/wire/cmd/wire@v0.7.0
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2

build:
	mkdir -p bin/ && go build -o ./bin/vane ./cmd/vane

test:
	go test -timeout 30m ./...

generate:
	go generate ./cmd/vane
	go mod tidy
	golangci-lint fmt
