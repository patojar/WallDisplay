BINARY ?= musicDisplay
GOOS   ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0

.PHONY: all build test clean build-arm build-amd

all: build

build:
	@mkdir -p bin
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) go build -o bin/$(BINARY) ./

build-arm:
	@mkdir -p bin
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o bin/$(BINARY)-arm64 ./

build-amd:
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/$(BINARY)-amd64 ./

test:
	go test ./...

clean:
	rm -rf bin
