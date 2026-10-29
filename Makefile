GOBIN := $(shell go env GOPATH)/bin
LINT  := $(GOBIN)/golangci-lint
# Keep in step with GOLANGCI_LINT_VERSION in .github/workflows/ci.yml.
LINT_VERSION := v2.14.0

# CI calls these targets rather than copying their commands: one definition,
# so a local run and CI cannot drift apart.

.PHONY: help vet test race stress stress-timing lint lint-install fmt check bench build demo stand run proto clean

# Recipes run in sh (Git Bash, CI) or in cmd.exe (an IDE on Windows, no sh
# in PATH): every one must mean the same in both. $(info) needs no shell.
help:
	$(info make vet           go vet)
	$(info make test          unit and measurement tests)
	$(info make race          tests under -race (needs gcc in PATH))
	$(info make stress        the nightly run: -race -count=20, takes long)
	$(info make stress-timing the tests stress skips: -count=20, no -race, go test -json)
	$(info make lint          golangci-lint, gofmt and goimports included)
	$(info make fmt           gofmt the tree)
	$(info make check         what must pass before a push: vet, test, lint)
	$(info make bench         the per-request benchmarks (plain, templated) and the export's bucket walk)
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
# SearchFeed_ASlowScreenKeepsTheLagUnderTwentyMilliseconds: under -race with a
# loaded CPU the host's start lag is 45-59ms with or without the screen
# (Docker, 4x yes), so there is nothing to check.
# Run_ASearchNamesTheStandsCapacity (14 of 120, 1 to 3 per job; held 125/0,
# broke 156/100) and Breakpoint_FindsTheStandsCapacity (8 of 120, 0 to 3 per
# job): the search names "the generator fell behind", which is the right
# verdict on a starved generator, so there is no capacity left to check.
# Breakpoint_ATargetThatDoesNotRecoverIsNamedSo (5 of 120, 0 to 2 per job) and
# Run_ASearchOnTheFakeTarget (1 of 120): the search gives up with the run limit
# on the same starved generator, at the first step for the second one.
# Run_OnceSearchStopsWhenTheFileRunsOut (run 40372477080, dataset_once_test.go:175):
# exit 0, want 3, after one run of "50 rps: sent 12 of 12" of the planned 15 —
# the search ends at the first step before the file can run out.
STRESS_SKIP := ^TestReport_SlotsHeldWithinTheAllowanceDoNotHitTheCap$$|^TestSearchFeed_ASlowScreenKeepsTheLagUnderTwentyMilliseconds$$|^TestRun_ASearchNamesTheStandsCapacity$$|^TestBreakpoint_FindsTheStandsCapacity$$|^TestBreakpoint_ATargetThatDoesNotRecoverIsNamedSo$$|^TestRun_ASearchOnTheFakeTarget$$|^TestRun_OnceSearchStopsWhenTheFileRunsOut$$

stress:
	go test -race -count=20 -timeout 90m -skip "$(STRESS_SKIP)" ./...

# The tests STRESS_SKIP leaves out, run the same 20 times without -race (the
# detector's slowdown is what they must not measure). Its stdout is read by
# `stressplan timing`, which colours the job red above 5 failed repeats of 20.
# Add a test to STRESS_SKIP only when its failed check compares a duration, a
# rate or a capacity; write the failing line and the run number above.
stress-timing:
	go test -json -count=20 -timeout 90m -run "$(STRESS_SKIP)" ./...

lint:
	$(LINT) run ./...

lint-install:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(LINT_VERSION)

fmt:
	gofmt -w .

# gofmt is checked by lint, as one of its formatters.
check:
	$(MAKE) vet test lint

# The cost of one request: the plain path (must stay at 0 allocs) and a templated
# one, and the fast template build against the naive one the reference test uses.
bench:
	go test -run NONE -bench "NewRequest|TemplateBuild" -benchmem -benchtime 200000x ./pkg/engine ./internal/cli
	go test -run NONE -bench BufferCumulative -benchmem -benchtime 2000x ./pkg/metrics

build:
	go build -o bin/leettest ./cmd/leettest

# demo and run draw the live view, which needs a terminal: an IDE Run window is
# not one and shows nothing. Run them from a terminal (make demo).
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
