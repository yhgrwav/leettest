// Copyright 2026 yhgrwav
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/yhgrwav/leettest/pkg/breakpoint"
	"github.com/yhgrwav/leettest/pkg/share"
	"github.com/yhgrwav/leettest/pkg/threshold"
)

var (
	ErrInvalidIP           = errors.New("got empty IP")
	ErrInvalidPort         = errors.New("got invalid port")
	ErrNoCalls             = errors.New("no calls configured")
	ErrInvalidMethod       = errors.New("method must look like package.Service/Method")
	ErrInvalidRPS          = errors.New("rps must be positive")
	ErrInvalidDuration     = errors.New("duration must be positive")
	ErrInvalidWarmup       = errors.New("warmup must not be negative")
	ErrInvalidRamp         = errors.New("ramp must not be negative")
	ErrEmptyName           = errors.New("name must not be empty: leave it out to use the service or file name")
	ErrDuplicateMethod     = errors.New("method appears in more than one call: the report is per method, keep one call for it")
	ErrFractionalRPS       = errors.New("rps must be a whole number of requests")
	ErrWarmupCoversTheCall = errors.New("warmup must be shorter than the call: otherwise nothing of it is measured")
	ErrInvalidMetadata     = errors.New("invalid metadata")
	ErrTLSFilesWithoutTLS  = errors.New("ca, cert, key and server_name need TLS: remove them or set app.tls: true")
	ErrCertWithoutKey      = errors.New("cert and key go together: set both or neither")
	ErrInvalidTimeout      = errors.New("timeout must be positive: without one, requests to a hung target pile up until the in-flight cap ends the run")
	ErrDataAndDataset      = errors.New("data and dataset are both set; use one")
	ErrEmptyDatasetPath    = errors.New("dataset must be the path of a file")
	// ErrDatasetOrder is a dataset_order that is neither file nor random.
	ErrDatasetOrder = errors.New("dataset_order must be file or random")
	// ErrOrderWithoutDataset is dataset_order or dataset_seed on a call without a dataset.
	ErrOrderWithoutDataset = errors.New("dataset_order and dataset_seed need a dataset")
	// ErrSeedWithoutRandom is dataset_seed without dataset_order: random.
	ErrSeedWithoutRandom = errors.New("dataset_seed needs dataset_order: random")
	// ErrDatasetSeed is a dataset_seed outside 0 to 2^53-1.
	ErrDatasetSeed = errors.New("dataset_seed must be a whole number from 0 to 9007199254740991")
	// ErrDatasetReuse is a dataset_reuse that is not true or false.
	ErrDatasetReuse = errors.New("dataset_reuse must be true or false")
	// ErrReuseWithoutDataset is dataset_reuse on a call without a dataset.
	ErrReuseWithoutDataset = errors.New("dataset_reuse needs a dataset")
	// ErrDuplicateAnchor is a YAML anchor name defined twice in the config.
	ErrDuplicateAnchor = errors.New("anchor is already defined")
	// ErrBreakpoint is a breakpoint section that cannot run as written.
	ErrBreakpoint = errors.New("invalid breakpoint")
	// ErrThreshold is a pass section that cannot be judged as written.
	ErrThreshold              = errors.New("invalid pass")
	ErrInvalidMaxResponseSize = errors.New("max_response_size must be a positive size below 2GiB with a unit: B, KB, MB, GB, KiB, MiB or GiB")
	// ErrInvalidConnections is app.connections outside 1 to 256 or not a whole
	// number.
	ErrInvalidConnections = errors.New("app.connections must be a whole number from 1 to 256")
	// ErrInvalidCompression is app.compression with a value but gzip.
	ErrInvalidCompression = errors.New("app.compression must be gzip, or left out")
	// ErrProtoSource is app.proto or app.protoset that cannot be used as written.
	ErrProtoSource = errors.New("invalid proto source")
	// ErrInvalidLabel is a label that breaks the rules of CheckLabel or is not a string.
	ErrInvalidLabel = errors.New("invalid label")
	// ErrInvalidMode is app.mode with a value but local or real, or with none.
	ErrInvalidMode = errors.New("app.mode must be local or real, or left out")
	// ErrRealNeedsRamp is app.mode: real without load.ramp.
	ErrRealNeedsRamp = errors.New("app.mode: real needs load.ramp (a ramp from 0 to rps), e.g. load.ramp: 30s")
)

// The values of app.mode. Left out, a config is local.
const (
	ModeLocal = "local"
	ModeReal  = "real"
)

type MasterConfig struct {
	// Name is what the header calls the run; nil leaves it to the service or file.
	Name *string `yaml:"name"`
	// RawLabels is labels as written. A value that is not a string is an error
	// and not a string made of it: 1.10 would come out as 1.1.
	RawLabels map[string]any `yaml:"labels"`
	// Labels are the checked labels; empty without any.
	Labels map[string]string `yaml:"-"`
	// SHA256 is the sha256 of the config's bytes as Parse got them, hex.
	SHA256 string `yaml:"-"`
	App    App    `yaml:"app"`
	Load   Load   `yaml:"load"`
}

type App struct {
	Target  ConnectionStringTarget `yaml:"target"`
	Address ConnectionString       `yaml:"-"`
	TLS     *bool                  `yaml:"tls"`
	UseTLS  bool                   `yaml:"-"`
	// CA is a PEM file of certificates that verify the target instead of the
	// system pool. Cert and Key are a PEM client certificate and its key for a
	// target that asks for one. LoadFile makes relative paths relative to the
	// config file; Parse keeps them as written.
	CA   string `yaml:"ca"`
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
	// ServerName is the name the target's certificate is checked against and
	// sent as SNI, for a target reached by an address its certificate does not
	// name. It changes nothing else: :authority stays the address.
	ServerName string `yaml:"server_name"`
	// RawMetadata is the field as written. Metadata is what every call
	// carries: keys lowercased, ${NAME} replaced from the environment; nil
	// when there is none.
	RawMetadata map[string]string `yaml:"metadata"`
	// RawMaxResponseSize is the field as written, such as 16MiB; the unit is
	// required. MaxResponseBytes is it in bytes, 0 when left out: the
	// transport's own limit of 4 MiB.
	RawMaxResponseSize *string           `yaml:"max_response_size"`
	MaxResponseBytes   int               `yaml:"-"`
	Metadata           map[string]string `yaml:"-"`
	// RawConnections is the value as written, nil when it was left out.
	// Connections is how many connections the run opens to the target, 1 to
	// 256; 1 when the file leaves it out.
	RawConnections *ConnectionCount `yaml:"connections"`
	Connections    int              `yaml:"-"`
	// RawCompression is the value as written, nil when it was left out.
	// Compression is how request bodies are compressed: "gzip", or empty
	// when the file leaves it out.
	RawCompression *string `yaml:"compression"`
	Compression    string  `yaml:"-"`
	// Proto names the .proto files the methods come from instead of the
	// target's reflection; nil when app.proto is not set. RawProtoset is
	// app.protoset as written, a FileDescriptorSet file, nil when not set;
	// Protoset is its path, relative to the config file once LoadFile has read it.
	Proto       *ProtoSource `yaml:"proto"`
	RawProtoset *string      `yaml:"protoset"`
	Protoset    string       `yaml:"-"`
	// RawMode is app.mode as written, nil when it was left out. Mode is
	// ModeLocal or ModeReal once the config is read.
	RawMode *string `yaml:"mode"`
	Mode    string  `yaml:"-"`
}

// ProtoSource is app.proto: entry files, and where their imports are looked
// for. LoadFile makes both relative to the config file; Parse keeps them as
// written.
type ProtoSource struct {
	Files       []string `yaml:"files"`
	ImportPaths []string `yaml:"import_paths"`
}

type ConnectionStringTarget struct {
	IP   string `yaml:"ip"`
	Port int    `yaml:"port"`
}

type ConnectionString string

type Load struct {
	// Ramp raises every call from 0 to its rps over this long before its
	// duration starts; 0 is no ramp.
	Ramp   time.Duration `yaml:"ramp"`
	Warmup time.Duration `yaml:"warmup"`
	Calls  []Call        `yaml:"calls"`
	// Breakpoint searches for the rate the one call's target breaks at,
	// instead of loading the calls at their rps; nil runs them as configured.
	Breakpoint *Breakpoint `yaml:"breakpoint"`
}

// Breakpoint is the search's profile: rates from From to To, each step Factor
// times the last or Step more, each held for Hold, its first Settle out of the
// verdict. P99Limit, when set, replaces the knee.
type Breakpoint struct {
	From     Rate          `yaml:"from"`
	To       Rate          `yaml:"to"`
	Factor   float64       `yaml:"factor"`
	Step     Rate          `yaml:"step"`
	Settle   time.Duration `yaml:"settle"`
	Hold     time.Duration `yaml:"hold"`
	P99Limit time.Duration `yaml:"p99_limit"`
	// RawErrorsLimit is errors_limit as written, nil when left out (a key
	// written with no value is refused before). ErrorsLimit is it read; zero
	// when left out, and the search then holds share.Default.
	RawErrorsLimit any           `yaml:"errors_limit"`
	ErrorsLimit    share.Percent `yaml:"-"`
	// RawPass is pass as written, nil when left out. Pass is it resolved.
	RawPass *BreakpointPass       `yaml:"pass"`
	Pass    threshold.SearchLimit `yaml:"-"`
}

// Plan is the search's plan for a call with the given timeout and in-flight
// cap (0 sizes the cap per step).
func (b Breakpoint) Plan(timeout time.Duration, maxInFlight int) breakpoint.Plan {
	return breakpoint.Plan{
		From: int(b.From), To: int(b.To), Factor: b.Factor, Step: int(b.Step),
		Settle: b.Settle, Hold: b.Hold, Timeout: timeout, MaxInFlight: maxInFlight, P99Limit: b.P99Limit,
		ErrorsLimit: b.ErrorsLimit,
	}
}

// resolveErrorsLimit reads errors_limit by the grammar of pass.errors.
func (b *Breakpoint) resolveErrorsLimit() error {
	if b.RawErrorsLimit == nil {
		return nil
	}

	limit, err := percentLimit(b.RawErrorsLimit, "breakpoint.errors_limit")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBreakpoint, err)
	}
	b.ErrorsLimit = limit

	return nil
}

// validate refuses a section breakpoint.Plan.Rates refuses, named by the key
// its check reads first.
func (b Breakpoint) validate() error {
	if b.P99Limit < 0 {
		return fmt.Errorf("%w: breakpoint.p99_limit %s, want positive or left out", ErrBreakpoint, b.P99Limit)
	}
	_, err := b.Plan(time.Second, 0).Rates()
	if err == nil {
		return nil
	}
	key := "settle"
	switch {
	case b.From < 1:
		key = "from"
	case b.To < b.From:
		key = "to"
	case b.Factor != 0:
		key = "factor"
	case b.Step < 0:
		key = "step"
	case b.Hold <= 0:
		key = "hold"
	}

	return fmt.Errorf("%w: breakpoint.%s: %w", ErrBreakpoint, key, err)
}

// validateSearch refuses what a search cannot run with: more than one call,
// a warm-up or a ramp of its own, the rate or length a search sets per step.
func (l Load) validateSearch() []error {
	var errs []error
	if len(l.Calls) > 1 {
		errs = append(errs, fmt.Errorf("%w: load.breakpoint searches one call, got %d", ErrBreakpoint, len(l.Calls)))
	}
	if l.Warmup != 0 {
		errs = append(errs, fmt.Errorf("%w: load.warmup with load.breakpoint: each step has its breakpoint.settle", ErrBreakpoint))
	}
	if l.Ramp > 0 {
		errs = append(errs, fmt.Errorf("%w: load.ramp with load.breakpoint: the search sets the rates", ErrBreakpoint))
	}
	for i := range l.Calls {
		call := &l.Calls[i]
		if call.RPS != 0 {
			errs = append(errs, fmt.Errorf("%w: %s: rps with load.breakpoint: the search sets it", ErrBreakpoint, call.where(i)))
		}
		if call.Duration != 0 {
			errs = append(errs, fmt.Errorf("%w: %s: duration with load.breakpoint: the search sets it", ErrBreakpoint, call.where(i)))
		}
	}
	if err := l.Breakpoint.validate(); err != nil {
		errs = append(errs, err)
	}

	return errs
}

// Rate is a whole number of requests per second. A fractional one is refused
// rather than truncated: the run would drive a load nobody asked for.
type Rate int

func (r *Rate) UnmarshalYAML(raw []byte) error {
	text := strings.Trim(strings.TrimSpace(string(raw)), `"'`)

	n, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrFractionalRPS, text)
	}
	*r = Rate(n)

	return nil
}

// ConnectionCount is app.connections as written, quotes at the ends removed.
// The number is read in resolveConnections, so a refusal can print the text.
type ConnectionCount string

func (c *ConnectionCount) UnmarshalYAML(raw []byte) error {
	*c = ConnectionCount(strings.Trim(strings.TrimSpace(string(raw)), `"'`))

	return nil
}

// The values of dataset_order, and the largest dataset_seed: 2^53-1, the
// largest integer a JSON reader on float64 holds exactly.
const (
	DatasetOrderFile   = "file"
	DatasetOrderRandom = "random"
	MaxDatasetSeed     = 1<<53 - 1
)

type Call struct {
	Method   string        `yaml:"method"`
	RPS      Rate          `yaml:"rps"`
	Duration time.Duration `yaml:"duration"`
	// Data is the request body as written, built into the method's message
	// before the run; nil sends an empty message.
	Data any `yaml:"data"`
	// RawDataset is the field as written; nil means it was left out. Dataset is
	// the path as written, kept for what prints it. Records are the file's
	// requests, read by LoadFile; Parse reads no file and leaves them empty.
	RawDataset *string         `yaml:"dataset"`
	Dataset    string          `yaml:"-"`
	Records    []DatasetRecord `yaml:"-"`
	// DatasetSHA256 is the sha256 of the dataset file, hex, as it is on disk.
	DatasetSHA256 string `yaml:"-"`
	// DatasetOrder is file or random; DatasetSeed the seed of random. Nil when left out.
	DatasetOrder *string `yaml:"dataset_order"`
	DatasetSeed  *int64  `yaml:"dataset_seed"`
	// DatasetReuse is dataset_reuse; nil when left out, which is true.
	DatasetReuse *bool `yaml:"dataset_reuse"`
	// RawTimeout is the field as written; nil means it was left out.
	RawTimeout *time.Duration `yaml:"timeout"`
	Timeout    time.Duration  `yaml:"-"`
	// RawPass is pass as written, nil when left out. Pass is it resolved: the
	// limits the run is judged by, empty when RawPass is nil.
	RawPass *Pass            `yaml:"pass"`
	Pass    threshold.Limits `yaml:"-"`
}

// Pass is a call's pass: section as written, key to value, so that a wrong key
// or a wrong kind of value is refused with the key named. resolvePass reads it.
type Pass map[string]any

// BreakpointPass is breakpoint.pass as written.
type BreakpointPass map[string]any

// DefaultTimeout applies to a call that sets none. It is short on purpose: in an
// open model a hung target holds rps × timeout requests in flight, and 2s keeps
// 2500 RPS under the default cap of 5000.
const DefaultTimeout = 2 * time.Second

// ResolveTimeout fills Timeout from the field or the default.
func (c *Call) ResolveTimeout() {
	if c.RawTimeout == nil {
		c.Timeout = DefaultTimeout

		return
	}
	c.Timeout = *c.RawTimeout
}

func (c ConnectionStringTarget) CreateConnectionString() (ConnectionString, error) {
	if c.IP == "" {
		return "", ErrInvalidIP
	}
	if c.Port < 1 || c.Port > 65535 {
		return "", fmt.Errorf("%w: %d", ErrInvalidPort, c.Port)
	}
	return ConnectionString(net.JoinHostPort(c.IP, strconv.Itoa(c.Port))), nil
}

func (a *App) ResolveTLS() {
	if a.TLS == nil {
		a.UseTLS = true
		return
	}
	a.UseTLS = *a.TLS
}

func (l Load) Validate() error {
	var errs []error

	if l.Warmup < 0 {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidWarmup, l.Warmup))
	}
	if l.Ramp < 0 {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidRamp, l.Ramp))
	}
	if len(l.Calls) == 0 {
		errs = append(errs, ErrNoCalls)
	}
	if l.Breakpoint != nil {
		errs = append(errs, l.validateSearch()...)
	}
	first := make(map[string]int, len(l.Calls))
	for i := range l.Calls {
		call := &l.Calls[i]
		checked := *call
		if l.Breakpoint != nil {
			// The search sets rps and duration; the rest is checked as always.
			checked.RPS, checked.Duration = 1, time.Second
		}
		if err := checked.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", call.where(i), err))
		}
		if call.Duration > 0 && l.Warmup >= call.Duration {
			errs = append(errs, fmt.Errorf("%s: %w: warmup %s, duration %s",
				call.where(i), ErrWarmupCoversTheCall, l.Warmup, call.Duration))
		}
		if j, ok := first[call.Method]; ok && call.Method != "" {
			errs = append(errs, fmt.Errorf("%s: %w: %q, as call %d", call.where(i), ErrDuplicateMethod, call.Method, j))
			continue
		}
		first[call.Method] = i
	}

	return errors.Join(errs...)
}

// where names a call the way the user can find it in the config: by its
// method when there is one, and by its place in the list either way.
func (c Call) where(i int) string {
	if c.Method == "" {
		return fmt.Sprintf("call %d", i)
	}

	return fmt.Sprintf("call %d (%s)", i, c.Method)
}

func (c Call) Validate() error {
	var errs []error

	if !isMethodName(c.Method) {
		errs = append(errs, fmt.Errorf("%w: %q", ErrInvalidMethod, c.Method))
	}
	if c.RPS < 1 {
		errs = append(errs, fmt.Errorf("%w: %d", ErrInvalidRPS, c.RPS))
	}
	if c.Duration <= 0 {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidDuration, c.Duration))
	}
	if c.RawTimeout != nil && *c.RawTimeout <= 0 {
		errs = append(errs, fmt.Errorf("%w: %s", ErrInvalidTimeout, *c.RawTimeout))
	}
	if c.RawDataset != nil && strings.TrimSpace(*c.RawDataset) == "" {
		errs = append(errs, ErrEmptyDatasetPath)
	}
	if c.RawDataset != nil && c.Data != nil {
		errs = append(errs, ErrDataAndDataset)
	}
	errs = append(errs, c.validateDatasetOrder()...)

	return errors.Join(errs...)
}

// validateDatasetOrder checks dataset_order and dataset_seed: the values, and
// that they stand with a dataset and, for the seed, with the random order.
func (c Call) validateDatasetOrder() []error {
	var errs []error

	random := false

	if c.DatasetOrder != nil {
		switch *c.DatasetOrder {
		case DatasetOrderFile:
		case DatasetOrderRandom:
			random = true
		default:
			errs = append(errs, fmt.Errorf("%w: %q", ErrDatasetOrder, *c.DatasetOrder))
		}
	}
	if (c.DatasetOrder != nil || c.DatasetSeed != nil) && c.RawDataset == nil {
		errs = append(errs, ErrOrderWithoutDataset)
	}
	if c.DatasetReuse != nil && c.RawDataset == nil {
		errs = append(errs, ErrReuseWithoutDataset)
	}
	if c.DatasetSeed != nil {
		if *c.DatasetSeed < 0 || *c.DatasetSeed > MaxDatasetSeed {
			errs = append(errs, fmt.Errorf("%w: %d", ErrDatasetSeed, *c.DatasetSeed))
		}
		if !random && (c.DatasetOrder == nil || *c.DatasetOrder == DatasetOrderFile) {
			errs = append(errs, ErrSeedWithoutRandom)
		}
	}

	return errs
}

func isMethodName(method string) bool {
	service, name, found := strings.Cut(method, "/")

	return found &&
		name != "" &&
		strings.Contains(service, ".") &&
		!strings.HasPrefix(service, ".") &&
		!strings.HasSuffix(service, ".")
}
