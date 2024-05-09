.PHONY: all
all: fmt build run

.PHONY: fmt
fmt: 
	@go fmt ./...

.PHONY: build
build:
	@go build -o ./bin/sandbox ./cmd/sandbox

.PHONY: run
run:
	@./bin/sandbox

.PHONY: test
test:
	@go test -v ./...

.PHONY: clean
clean:
	@go clean && rm -rf ./bin/*
