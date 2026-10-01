#!/bin/sh
# Start lag and CPU of leettest before and after, against the reference stand.
# Run in golang:1.27.1 with /before and /after source trees and /bench mounted.
set -e
cd /bench/cpu && go mod init cpu >/dev/null 2>&1 || true; go build -o /tmp/cpu .
for v in before after; do
  cd /$v && go build -o /tmp/lt-$v ./cmd/leettest && go build -o /tmp/stand ./test/stand/cmd/stand
done
/tmp/stand -delay 20ms -life 1h >/dev/null 2>&1 &
sleep 1

one() { # version rps
  cat > /tmp/c.yaml <<EOF
name: bench
app:
  target: {ip: 127.0.0.1, port: 50051}
  tls: false
load:
  calls:
    - method: grpc.health.v1.Health/Check
      rps: $2
      duration: 15s
      timeout: 500ms
EOF
  /tmp/cpu /tmp/lt-$1 -c /tmp/c.yaml -output json -max-in-flight 5000 2>/tmp/cpu.txt >/tmp/r.json || true
  printf '%s %5s rps  ' "$1" "$2"
  grep -o '"start_lag":{"p99":{"us":[0-9]*' /tmp/r.json | sed 's/.*://; s/$/us p99 lag  /' | tr -d '\n'
  grep -o '"p50":{"us":[0-9]*' /tmp/r.json | head -1 | sed 's/.*://; s/$/us p50  /' | tr -d '\n'
  grep CPU /tmp/cpu.txt
}

three() { # version: three fake methods at 1000 rps each
  cat > /tmp/c3.yaml <<EOF
name: bench3
app:
  target: {ip: 127.0.0.1, port: 50051}
  tls: false
load:
  calls:
    - {method: a.B/One, rps: 1000, duration: 15s, timeout: 500ms}
    - {method: a.B/Two, rps: 1000, duration: 15s, timeout: 500ms}
    - {method: a.B/Three, rps: 1000, duration: 15s, timeout: 500ms}
EOF
  /tmp/cpu /tmp/lt-$1 -c /tmp/c3.yaml -fake -fake-delay 20ms -fake-jitter 0s -output json -max-in-flight 5000 2>/tmp/cpu.txt >/tmp/r.json || true
  printf '%s 3x1000 fake  ' "$1"
  grep -o '"start_lag":{"p99":{"us":[0-9]*' /tmp/r.json | sed 's/.*://; s/$/us p99 lag  /' | tr -d '\n'
  grep CPU /tmp/cpu.txt
}

for r in ${RATES:-100 300 1000 3000}; do one before $r; one after $r; done
if [ -z "$NO3" ]; then three before; three after; fi
