GOBIN := $(shell go env GOPATH)/bin
LINT  := $(GOBIN)/golangci-lint
# Keep in step with GOLANGCI_LINT_VERSION in .github/workflows/ci.yml.
LINT_VERSION := v2.14.0

.PHONY: help test race stress lint lint-install fmt check build demo stand run clean

help:
	@echo "make test          unit and measurement tests"
	@echo "make race          tests under -race (needs gcc in PATH)"
	@echo "make stress        the nightly run: -race -count=20, takes long"
	@echo "make lint          golangci-lint"
	@echo "make fmt           gofmt the tree"
	@echo "make check         what must pass before a push: gofmt, vet, test, lint"
	@echo "make build         bin/leettest"
	@echo "make demo          a run against the built-in fake target, no server needed"
	@echo "make stand         start the reference stand on 127.0.0.1:50051 (keep it running)"
	@echo "make run           a run against the stand, from a second terminal"

test:
	go test ./...

race:
	go test -race ./...

stress:
	go test -race -count=20 -timeout 90m ./...

lint:
	$(LINT) run ./...

lint-install:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VERSION)

fmt:
	gofmt -w .

check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	go vet ./...
	go test ./...
	$(LINT) run ./...

build:
	go build -o bin/leettest ./cmd/leettest

demo:
	go run ./cmd/leettest -c examples/stand.yaml -fake

stand:
	go run ./test/stand/cmd/stand -delay 20ms -freeze-at 8s -freeze-for 1s

run:
	go run ./cmd/leettest -c examples/stand.yaml

clean:
	rm -rf bin dist reports
