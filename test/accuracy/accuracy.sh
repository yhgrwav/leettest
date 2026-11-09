#!/bin/sh
# Measures LeetTest and ghz (synchronous mode, its default) against the reference
# stand holding every answer a constant D, at one rate, and prints one table.
# Nothing is asserted: it is a measurement. Run from the repository root; `make
# accuracy` runs it in a golang container, which has the Linux Go toolchain the
# three binaries need. It takes (number of delays) x REPEATS x 2 x (RUN_FOR plus a
# few seconds): 6 to 7 minutes with the defaults.
#
# Every run gets a fresh stand. The stand's own count of arrivals, from its exit
# summary, is the exact number of calls that reached the server; the tool's own
# count is beside it. The order of the two tools alternates with the repeat, so
# neither always runs first.
set -eu

GHZ_VERSION=${GHZ_VERSION:-v0.121.0}
DELAYS=${DELAYS:-"20ms 100ms"}
REPEATS=${REPEATS:-3}
RATE=${RATE:-100}
RUN_FOR=${RUN_FOR:-30}
# ghz's own default; it is written out. At 100 calls a second and 100 ms a call
# about 10 calls are in flight, so 50 workers never limit the rate.
GHZ_C=${GHZ_C:-50}

PORT=50051
METHOD=grpc.health.v1.Health/Check
BIN=$(mktemp -d)
WORK=$(mktemp -d)
trap 'rm -rf "$BIN" "$WORK"' EXIT

if ! command -v jq >/dev/null 2>&1; then
	export DEBIAN_FRONTEND=noninteractive
	apt-get -qq update >/dev/null
	apt-get -qq install -y jq >/dev/null
fi

echo "building ghz $GHZ_VERSION, the stand and leettest" >&2
GOBIN=$BIN CGO_ENABLED=0 go install "github.com/bojand/ghz/cmd/ghz@$GHZ_VERSION"
CGO_ENABLED=0 go build -o "$BIN/stand" ./test/stand/cmd/stand
CGO_ENABLED=0 go build -o "$BIN/leettest" ./cmd/leettest

# ms prints nanoseconds as milliseconds with two decimals.
ms() { awk -v ns="$1" 'BEGIN { printf "%.2f", ns / 1e6 }'; }

# stand_start D starts a stand and waits for it to listen; its pid is in $STAND.
stand_start() {
	rm -f "$WORK/arrivals" "$WORK/stand.out" "$WORK/stand.err"
	"$BIN/stand" -addr 127.0.0.1:$PORT -delay "$1" -arrivals "$WORK/arrivals" \
		>"$WORK/stand.out" 2>"$WORK/stand.err" &
	STAND=$!
	n=0
	while ! grep -q "^stand on" "$WORK/stand.err"; do
		n=$((n + 1))
		if [ "$n" -gt 200 ]; then
			echo "the stand did not start" >&2
			exit 1
		fi
		sleep 0.1
	done
}

# stand_stop stops it, so it writes its summary, and prints the arrivals count.
stand_stop() {
	kill -INT "$STAND"
	wait "$STAND" || true
	sed -n 's/^arrivals \([0-9]*\), answered \([0-9]*\)$/\1/p' "$WORK/stand.out"
}

run_ghz() {
	"$BIN/ghz" --insecure --call "$METHOD" -d '{}' --rps "$RATE" -z "${RUN_FOR}s" -t 5s \
		--duration-stop wait --connections 1 -c "$GHZ_C" -O json -o "$WORK/ghz.json" \
		127.0.0.1:$PORT >/dev/null 2>"$WORK/ghz.err" || {
		cat "$WORK/ghz.err" >&2
		return 1
	}
	count=$(jq '.count' "$WORK/ghz.json")
	for p in 50 90 99; do
		v=$(jq ".latencyDistribution[] | select(.percentage == $p) | .latency" "$WORK/ghz.json")
		eval "p$p=\$(ms \$v)"
	done
}

run_leettest() {
	cat >"$WORK/leettest.yaml" <<EOF
app:
  target:
    ip: 127.0.0.1
    port: $PORT
  tls: false
  connections: 1

load:
  calls:
    - method: $METHOD
      rps: $RATE
      duration: ${RUN_FOR}s
      timeout: 5s
EOF
	# A run that ends invalid exits non-zero and still prints its numbers.
	status=0
	"$BIN/leettest" -c "$WORK/leettest.yaml" -output json >"$WORK/leettest.json" 2>"$WORK/leettest.err" || status=$?
	if ! jq -e '.methods[0].latency.p50.us' "$WORK/leettest.json" >/dev/null 2>&1; then
		cat "$WORK/leettest.err" >&2
		return 1
	fi
	count=$(jq '.methods[0].sent' "$WORK/leettest.json")
	for p in 50 90 99; do
		us=$(jq ".methods[0].latency.p$p.us" "$WORK/leettest.json")
		eval "p$p=\$(ms \$((us * 1000)))"
	done
	outcome=$(jq -r '.outcome' "$WORK/leettest.json")
	note="exit $status, $outcome"
}

row() { # tool D repeat count p50 p90 p99 arrived note
	printf '%-9s %-6s %-6s %6s %9s %9s %9s %9s  %s\n' "$@"
}

measure() { # tool D repeat
	stand_start "$2"
	count=0 p50=0 p90=0 p99=0 note=""
	ok=1
	if [ "$1" = ghz ]; then run_ghz || ok=0; else run_leettest || ok=0; fi
	arrived=$(stand_stop)
	if [ "$ok" = 0 ]; then
		row "$1" "$2" "$3" - - - - "${arrived:-?}" "the run failed"
		return
	fi
	row "$1" "$2" "$3" "$count" "$p50" "$p90" "$p99" "${arrived:-?}" "$note"
}

echo "ghz $GHZ_VERSION, stand delay D, $RATE calls/s for ${RUN_FOR}s, one connection, ghz -c $GHZ_C" >&2
echo
row tool D repeat count p50_ms p90_ms p99_ms at_stand note
for d in $DELAYS; do
	r=1
	while [ "$r" -le "$REPEATS" ]; do
		if [ $((r % 2)) -eq 1 ]; then
			measure ghz "$d" "$r"
			measure leettest "$d" "$r"
		else
			measure leettest "$d" "$r"
			measure ghz "$d" "$r"
		fi
		r=$((r + 1))
	done
done
