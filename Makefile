.PHONY: all build test lint clean docker-build

BINARY_NAME=argolum
IMAGE_NAME=ghcr.io/aleksandrsumkin/argolum:latest

all: test build

build:
	CGO_ENABLED=0 go build -o bin/$(BINARY_NAME) ./cmd/argolum

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o bin/$(BINARY_NAME) ./cmd/argolum

test:
	go test -v -race ./...

clean:
	rm -rf bin/

docker-build:
	docker build -t $(IMAGE_NAME) .
