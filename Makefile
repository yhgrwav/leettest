GOBIN := $(shell go env GOPATH)/bin
LINT  := $(GOBIN)/golangci-lint
# Keep in step with GOLANGCI_LINT_VERSION in .github/workflows/ci.yml.
LINT_VERSION := v2.14.0

# CI calls these targets rather than copying their commands: one definition,
# so a local run and CI cannot drift apart.

.PHONY: help vet test race stress stress-timing fuzz lint lint-install fmt check bench build demo stand run proto clean

# Recipes run in sh (Git Bash, CI) or in cmd.exe (an IDE on Windows, no sh
# in PATH): every one must mean the same in both. $(info) needs no shell.
help:
	$(info make vet           go vet)
	$(info make test          unit and measurement tests)
	$(info make race          tests under -race (needs gcc in PATH))
	$(info make stress        the nightly run: -race -count=20, takes long)
	$(info make stress-timing the tests stress skips: -count=20, no -race, go test -json)
	$(info make fuzz          the fuzz targets, 30 s each (FUZZTIME=2m to change it))
	$(info make release-test what release.yml runs: -race minus the STRESS_SKIP tests, then those without -race)
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
# Each STRESS_SKIP test is left out of the -race stress run for the reason below,
# in three groups; each runs without -race in stress-timing and release-test-plain.
#
# 1. Timing by design: the test's own check compares a start lag or a duration.
# SlotsHeldWithinTheAllowanceDoNotHitTheCap: a starved scheduler releases the
# slots 120-160ms past their deadline, beyond the 100ms allowance, so the cap
# is right to fire and there is nothing to check.
# SearchFeed_ASlowScreenKeepsTheLagUnderTwentyMilliseconds: under -race with a
# loaded CPU the host's start lag is 45-59ms with or without the screen
# (Docker, 4x yes), so there is nothing to check.
# Report_AnOnTimeStarterIsNotLate (run 41830566342, generator_late_test.go:215,
# start lag p99 518.399us): a starved runner makes one call of the p99 tail
# start over the wait floor, and the rule calls that generator_late (reproduced
# 5 of 600 under load in Docker, branch Moved with 200 observations).
# EarlyWarning_AnOnTimeStarterIsNotWarnedOf (run 42183791170,
# early_warning_test.go:95, "start lag p99 1.275903ms, want under the wait
# floor 1ms"): a premise on a duration under -race.
#
# 2. Needs a non-loaded stand: a breaking-point search names the target's
# capacity only if the generator keeps its schedule.
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
# Run_ASearchStartsWithItsRamp (run 43181561698, search_ramp_test.go:119, "the
# search has 1 runs, want 2 or more"): the search ends at the first step on the
# same starved generator.
# Run_ASearchWithoutARampIsTodays (run 43462369661, search_ramp_test.go:193, the
# same message): the same cause.
#
# 3. Covered by the no-race pass: under -race the run ends invalid (exit 2) or
# warns generator_late, the right verdict on a starved generator, so there is no
# start lag left to check; the pass without -race runs the test.
# Run_NoLagNoWarning (runs 41535965010, 41599248531, 41658167545 and
# 41709124092, lagwatch_test.go:247: "stderr has a warning on a 50 rps run",
# 2 to 26 of 149 calls started more than 1ms late),
# Run_JSONCarriesTheGeneratorCPUOnTheGridOfTheRun (the same four runs,
# generator_cpu_test.go:64), Run_AHealthyRunUnderALimitRunsToTheEnd
# (41535965010, errorspike_test.go:364), Run_ARampWithoutAWarmUpNamesTheRampAlone
# (41658167545, ramp_test.go:90), Run_LocalNeverAsks (41658167545,
# confirm_test.go:374), ExportRun_AScrapeStormDoesNotChangeTheReportAndRendersOncePerSecond
# (41599248531, 41658167545 and 41709124092, export_test.go:753 and :809) and
# ExportRun_AScrapeMidRunSeesThePlannedRateAndGrowingCalls (41709124092,
# export_test.go:354): all seven end "the run is invalid" (exit 2) or warn that
# the generator is behind its schedule (generator_late) on a 50 to 4000 rps run
# of a fake target.
STRESS_SKIP := ^TestReport_SlotsHeldWithinTheAllowanceDoNotHitTheCap$$|^TestSearchFeed_ASlowScreenKeepsTheLagUnderTwentyMilliseconds$$|^TestRun_ASearchNamesTheStandsCapacity$$|^TestBreakpoint_FindsTheStandsCapacity$$|^TestBreakpoint_ATargetThatDoesNotRecoverIsNamedSo$$|^TestRun_ASearchOnTheFakeTarget$$|^TestRun_OnceSearchStopsWhenTheFileRunsOut$$|^TestRun_ASearchStartsWithItsRamp$$|^TestRun_ASearchWithoutARampIsTodays$$|^TestRun_NoLagNoWarning$$|^TestRun_JSONCarriesTheGeneratorCPUOnTheGridOfTheRun$$|^TestExportRun_AScrapeStormDoesNotChangeTheReportAndRendersOncePerSecond$$|^TestRun_AHealthyRunUnderALimitRunsToTheEnd$$|^TestRun_ARampWithoutAWarmUpNamesTheRampAlone$$|^TestRun_LocalNeverAsks$$|^TestExportRun_AScrapeMidRunSeesThePlannedRateAndGrowingCalls$$|^TestReport_AnOnTimeStarterIsNotLate$$|^TestEarlyWarning_AnOnTimeStarterIsNotWarnedOf$$

# LEETTEST_STRESS=1 lets an end-to-end test in cmd/leettest skip itself when its
# run ended invalid for generator_late or clock_step alone (skipIfStarved): the
# detector starves the generator on a shared runner, and that verdict is not
# about the code. Every skip appends a line to stress-skips.txt (delete the file
# before a local run); the stress job prints the count per test. Plain `make
# test`, `make race` and the CI of a PR do not set it. Target-specific export:
# it means the same in sh and cmd.
stress: export LEETTEST_STRESS = 1
stress: export LEETTEST_STRESS_SKIPS = $(CURDIR)/stress-skips.txt
stress:
	go test -race -count=20 -timeout 90m -skip "$(STRESS_SKIP)" ./...

# The tests STRESS_SKIP leaves out, run the same 20 times without -race (the
# detector's slowdown is what they must not measure). Its stdout is read by
# `stressplan timing`, which colours the job red above 5 failed repeats of 20.
# Add a test to STRESS_SKIP only when its failed check compares a duration, a
# rate or a capacity; write the failing line and the run number above.
stress-timing:
	go test -json -count=20 -timeout 90m -run "$(STRESS_SKIP)" ./...

# What release.yml runs as its test step. The same split as stress: every test
# but the STRESS_SKIP ones under -race (with LEETTEST_STRESS, so skipIfStarved
# applies), then the STRESS_SKIP ones once without -race and without
# LEETTEST_STRESS. Either part red makes the target red; the second part runs
# only after the first passed. The export is on release-test-race alone: a
# target-specific export does not reach its sibling prerequisite.
.PHONY: release-test release-test-race release-test-plain
release-test: release-test-race release-test-plain

release-test-race: export LEETTEST_STRESS = 1
release-test-race: export LEETTEST_STRESS_SKIPS = $(CURDIR)/stress-skips.txt
release-test-race:
	go test -race -count=1 -timeout 30m -skip "$(STRESS_SKIP)" ./...

release-test-plain: | release-test-race
	go test -count=1 -timeout 30m -run "$(STRESS_SKIP)" ./...

# The fuzz targets read untrusted input (config, template text, dataset file):
# 30 s each, mutating their seeds; `go test` alone replays the seeds. go test
# -fuzz takes one target at a time, hence one line each. A crash is written to
# testdata/fuzz next to the target; commit it.
FUZZTIME := 30s
fuzz:
	go test ./pkg/config -run NONE -fuzz "^FuzzParse$$" -fuzztime $(FUZZTIME)
	go test ./pkg/config -run NONE -fuzz "^FuzzParseTemplate$$" -fuzztime $(FUZZTIME)
	go test ./pkg/config -run NONE -fuzz "^FuzzExpandEnvRefs$$" -fuzztime $(FUZZTIME)
	go test ./pkg/config -run NONE -fuzz "^FuzzReadDataset$$" -fuzztime $(FUZZTIME)

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

# The measurement in test/accuracy/accuracy.sh: LeetTest and ghz at the stand, one
# table, nothing asserted. It runs in a Linux container (about 7 minutes). The cd
# is inside the command, as in proto.
.PHONY: accuracy
accuracy:
	docker run --rm -v "$(CURDIR):/src" golang:1.27.1 sh -c "cd /src && sh test/accuracy/accuracy.sh"
