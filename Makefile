.PHONY: all build test lint fmt clean

all: lint test build

build:
	go build -o bin/scheduler ./cmd/scheduler
	go build -o bin/worker ./cmd/worker

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .
	goimports -w -local github.com/chicogong/dtask-scheduler .

clean:
	rm -rf bin/
