package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/yhgrwav/leettest/pkg/descriptor"
)

// -no-reflection reaches the stand it starts: the same lookup resolves with the flag off and
// finds no reflection with it on, and the flag composes with another one.
func TestRun_NoReflectionFlagReachesTheStand(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		bare bool
	}{
		{"default serves reflection", nil, false},
		{"flag drops it", []string{"-no-reflection"}, true},
		{"with another flag", []string{"-no-reflection", "-delay", "1ms"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			stop := make(chan struct{})
			addr := make(chan string, 1)
			done := make(chan error, 1)

			go func() {
				done <- run(append([]string{"-addr", "127.0.0.1:0"}, tt.args...), &out, &errOut, stop, func(a string) { addr <- a })
			}()

			var target string
			select {
			case target = <-addr:
			case <-time.After(5 * time.Second):
				t.Fatal("stand did not start")
			}

			conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer conn.Close()

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			_, err = descriptor.NewReflectionResolver(conn).Resolve(ctx, "wallet.v1.WalletService/GetBalance")
			switch {
			case tt.bare && !errors.Is(err, descriptor.ErrReflectionUnsupported):
				t.Errorf("Resolve err = %v, want ErrReflectionUnsupported", err)
			case !tt.bare && err != nil:
				t.Errorf("Resolve: %v", err)
			}

			close(stop)
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("run: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("run did not return after stop")
			}
		})
	}
}

func TestParse_RejectsTwoBehaviorsAtOnce(t *testing.T) {
	_, err := parse([]string{"-freeze-for", "1s", "-hang-from", "8s"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-freeze-for") || !strings.Contains(err.Error(), "-hang-from") {
		t.Fatalf("err = %v, want one naming both flags", err)
	}
}

func TestParse_CapacityIsABehaviorOfItsOwn(t *testing.T) {
	if _, err := parse([]string{"-capacity", "300", "-delay", "20ms"}, io.Discard); err != nil {
		t.Fatalf("capacity with a delay: %v", err)
	}
	_, err := parse([]string{"-capacity", "300", "-fail-every", "10"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "-capacity") {
		t.Fatalf("err = %v, want one naming -capacity", err)
	}
	if _, err := parse([]string{"-capacity", "-1"}, io.Discard); err == nil {
		t.Fatal("want an error for a negative capacity")
	}
}

func TestParse_RejectsFreezeAtWithoutLength(t *testing.T) {
	if _, err := parse([]string{"-freeze-at", "5s"}, io.Discard); err == nil {
		t.Fatal("want an error: a freeze start without a length freezes nothing")
	}
}

func TestParse_RejectsNegativeValues(t *testing.T) {
	for _, args := range [][]string{
		{"-delay", "-1ms"},
		{"-freeze-at", "-1s", "-freeze-for", "1s"},
		{"-hang-from", "-1s"},
		{"-fail-every", "-2"},
		{"-life", "-1s"},
	} {
		if _, err := parse(args, io.Discard); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestRun_CountsWhatArrivedAndWasAnswered(t *testing.T) {
	var out, errOut bytes.Buffer
	stop := make(chan struct{})
	addr := make(chan string, 1)
	done := make(chan error, 1)

	go func() {
		done <- run([]string{"-addr", "127.0.0.1:0"}, &out, &errOut, stop, func(a string) { addr <- a })
	}()

	var target string
	select {
	case target = <-addr:
	case <-time.After(5 * time.Second):
		t.Fatal("stand did not start")
	}

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := grpc_health_v1.NewHealthClient(conn).Check(t.Context(), &grpc_health_v1.HealthCheckRequest{}); err != nil {
		t.Fatalf("check: %v", err)
	}

	close(stop)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after stop")
	}

	for _, want := range []string{"arrivals 1, answered 1", "\n  0  1\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, out.String())
		}
	}
}

// -max-streams 0 must leave the option out rather than pass 0: grpc-go's server then announces
// nothing, which is "no limit", while a 0 on the wire would be "no streams at all".
func TestParse_MaxStreamsBecomesAServerOptionUnlessZero(t *testing.T) {
	tests := []struct {
		args    []string
		options int
	}{
		{[]string{"-max-streams", "1"}, 1},
		{[]string{"-max-streams", "0"}, 0},
		{nil, 0},
		// A property of the connection, not a way to answer: it goes with any behavior.
		{[]string{"-max-streams", "2", "-hang-from", "1s"}, 1},
	}

	for _, tt := range tests {
		o, err := parse(tt.args, io.Discard)
		if err != nil {
			t.Errorf("%v: %v", tt.args, err)

			continue
		}

		if got := len(o.serverOptions()); got != tt.options {
			t.Errorf("%v: %d server options, want %d", tt.args, got, tt.options)
		}
	}
}

func TestParse_RejectsNegativeMaxStreams(t *testing.T) {
	if _, err := parse([]string{"-max-streams", "-1"}, io.Discard); err == nil {
		t.Error("-max-streams -1 accepted")
	}
}

func TestWriteArrivals_OneOffsetFromTheFirstArrivalPerLine(t *testing.T) {
	t0 := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "arrivals.txt")

	// The third moment is earlier than the second: arrival order is written
	// as taken, sorting is the reader's.
	err := writeArrivals(path, []time.Time{t0, t0.Add(3 * time.Millisecond), t0.Add(time.Millisecond)})
	if err != nil {
		t.Fatalf("writeArrivals: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "0\n3000000\n1000000\n"; string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestRun_WritesTheArrivalsFileOnExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arrivals.txt")
	target, finish := startForArrivals(t, "-arrivals", path)

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	for range 3 {
		if _, err = grpc_health_v1.NewHealthClient(conn).Check(t.Context(), &grpc_health_v1.HealthCheckRequest{}); err != nil {
			t.Fatalf("check: %v", err)
		}
	}

	if err = finish(); err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	lines := strings.Fields(string(data))
	if len(lines) != 3 {
		t.Fatalf("%d lines, want one per call:\n%s", len(lines), data)
	}
	if lines[0] != "0" {
		t.Errorf("first line = %q, want 0: offsets count from the first arrival", lines[0])
	}
	var prev int64
	for _, l := range lines {
		ns, err := strconv.ParseInt(l, 10, 64)
		if err != nil || ns < prev {
			t.Fatalf("line %q after %d: want integers that do not go back (sequential calls)", l, prev)
		}
		prev = ns
	}
}

func TestRun_AnUnwritableArrivalsFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "arrivals.txt")
	_, finish := startForArrivals(t, "-arrivals", path)

	err := finish()
	if err == nil || !strings.Contains(err.Error(), "-arrivals") {
		t.Fatalf("err = %v, want one naming -arrivals: a run whose record is lost must not look fine", err)
	}
}

// startForArrivals starts the stand with args and returns its address and a
// function that stops it and returns run's error.
func startForArrivals(t *testing.T, args ...string) (target string, finish func() error) {
	t.Helper()

	stop := make(chan struct{})
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(append([]string{"-addr", "127.0.0.1:0"}, args...), io.Discard, io.Discard, stop,
			func(a string) { addr <- a })
	}()

	select {
	case target = <-addr:
	case <-time.After(5 * time.Second):
		t.Fatal("stand did not start")
	}

	return target, func() error {
		close(stop)
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			t.Fatal("run did not return after stop")

			return nil
		}
	}
}
