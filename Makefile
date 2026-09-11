.PHONY: test test-web build run

VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
LDFLAGS := -X GoTodo/internal/version.Version=$(VERSION)

ifeq ($(OS),Windows_NT)
BIN := GoTodo.exe
else
BIN := GoTodo
endif

test:
	go test ./...
	npm --prefix web test

test-web:
	npm --prefix web test

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) .

run:
	go run -ldflags "$(LDFLAGS)" .
