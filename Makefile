GOBIN := $(shell go env GOPATH)/bin
LINT  := $(GOBIN)/golangci-lint
# Keep in step with GOLANGCI_LINT_VERSION in .github/workflows/ci.yml.
LINT_VERSION := v2.14.0

# CI calls these targets rather than copying their commands: one definition,
# so a local run and CI cannot drift apart.

.PHONY: help vet test race stress lint lint-install fmt check build demo stand run proto clean

# Recipes run in sh (Git Bash, CI) or in cmd.exe (an IDE on Windows, no sh
# in PATH): every one must mean the same in both. $(info) needs no shell.
help:
	$(info make vet           go vet)
	$(info make test          unit and measurement tests)
	$(info make race          tests under -race (needs gcc in PATH))
	$(info make stress        the nightly run: -race -count=20, takes long)
	$(info make lint          golangci-lint, gofmt and goimports included)
	$(info make fmt           gofmt the tree)
	$(info make check         what must pass before a push: vet, test, lint)
	$(info make build         bin/leettest)
	$(info make demo          a run against the built-in fake target, no server needed)
	$(info make stand         start the reference stand on 127.0.0.1:50051 (keep it running))
	$(info make run           a run against the stand, from a second terminal)
	$(info make proto         regenerate the stand's code from wallet.proto, in Docker)
	@:

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

# Tests whose premise needs an unloaded machine; ordinary CI runs them as is.
# SlotsHeldWithinTheAllowanceDoNotHitTheCap: a starved scheduler releases the
# slots 120-160ms past their deadline, beyond the 100ms allowance, so the cap
# is right to fire and there is nothing to check.
STRESS_SKIP := ^TestReport_SlotsHeldWithinTheAllowanceDoNotHitTheCap$$

stress:
	go test -race -count=20 -timeout 90m -skip "$(STRESS_SKIP)" ./...

lint:
	$(LINT) run ./...

lint-install:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VERSION)

fmt:
	gofmt -w .

# gofmt is checked by lint, as one of its formatters.
check:
	$(MAKE) vet test lint

build:
	go build -o bin/leettest ./cmd/leettest

demo:
	go run ./cmd/leettest -c examples/stand.yaml -fake

stand:
	go run ./test/stand/cmd/stand -delay 20ms -freeze-at 8s -freeze-for 1s

run:
	go run ./cmd/leettest -c examples/stand.yaml

# The pinned tools are Linux builds, so the script runs in a Linux container.
# The cd is inside the command: Git Bash rewrites a bare /src argument into a
# Windows path.
proto:
	docker run --rm -v "$(CURDIR):/src" golang:1.27.1 sh -c "cd /src && apt-get -qq update && apt-get -qq install -y unzip && sh test/stand/proto/gen.sh"

# git, not rm: cmd.exe has no rm. -X removes only ignored files there.
clean:
	git clean -fdX -- bin dist reports
