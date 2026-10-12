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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// docker runs the docker CLI and returns stdout and stderr apart: a tool's
// report is on stdout.
func docker(ctx context.Context, args ...string) (stdout, stderr []byte, err error) {
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()

	return out.Bytes(), errOut.Bytes(), err
}

func dockerOK(ctx context.Context, args ...string) (string, error) {
	out, errOut, err := docker(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, errOut)
	}

	return strings.TrimSpace(string(out)), nil
}

// mountPath is a host path as docker -v wants it: absolute, with forward
// slashes, which Docker Desktop on Windows takes as C:/....
func mountPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}

	return filepath.ToSlash(abs), nil
}

// linuxBuild builds a static Linux binary of pkg into dir.
func linuxBuild(ctx context.Context, pkg, target string) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-o", target, pkg)
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build %s: %w: %s", pkg, err, b)
	}

	return nil
}

// installGhz puts a Linux ghz of the given version into dir/ghz, built by the
// Go image so no Go on the host has to cross-compile it.
func installGhz(ctx context.Context, goImage, version, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "ghz")); err == nil {
		return nil
	}
	mount, err := mountPath(dir)
	if err != nil {
		return err
	}
	_, err = dockerOK(ctx, "run", "--rm", "-e", "GOBIN=/out", "-e", "CGO_ENABLED=0", "-v", mount+":/out",
		goImage, "go", "install", "github.com/bojand/ghz/cmd/ghz@"+version)

	return err
}

// leftovers lists containers of this harness still around.
func leftovers(ctx context.Context, prefix string) ([]string, error) {
	out, err := dockerOK(ctx, "ps", "-a", "--filter", "name="+prefix, "--format", "{{.Names}}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	return strings.Split(out, "\n"), nil
}

// removeAll force-removes containers; it runs on a context of its own so an
// interrupt still cleans up.
func removeAll(names ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, n := range names {
		_, _, _ = docker(ctx, "rm", "-f", n)
	}
}

// runOut is everything one run left behind; it is written to result.json.
type runOut struct {
	Tool    string
	Setup   setup
	Dir     string
	Reports toolResult
	Stand   standSummary
	Verdict verdict
	Spacing *spacing
}

// execute does one run: a fresh stand, one tool at it, the stand stopped so it
// writes what it saw, then the numbers read from the stand's side. label names
// the run's folder under runs/.
func (h *harness) execute(ctx context.Context, tool string, s setup, label string) (res runOut, err error) {
	res = runOut{Tool: tool, Setup: s}

	dir := filepath.Join(h.out, "runs", label)
	err = os.MkdirAll(dir, 0o750)
	if err != nil {
		return res, err
	}
	// The k6 image runs as its own user and writes into this folder.
	err = os.Chmod(dir, 0o777)
	if err != nil {
		return res, err
	}
	res.Dir = dir
	data, err := mountPath(dir)
	if err != nil {
		return res, err
	}

	h.n++
	standName := fmt.Sprintf("%s-stand-%d", h.prefix, h.n)
	toolName := fmt.Sprintf("%s-%s-%d", h.prefix, tool, h.n)
	defer removeAll(standName, toolName)

	if _, err = dockerOK(ctx, standRunArgs(standName, h.network, s, h.binMount, data)...); err != nil {
		return res, err
	}
	err = waitReady(ctx, standName)
	if err != nil {
		return res, err
	}
	ip, err := dockerOK(ctx, "inspect", "-f",
		fmt.Sprintf(`{{(index .NetworkSettings.Networks %q).IPAddress}}`, h.network), standName)
	if err != nil {
		return res, err
	}
	if ip == "" {
		return res, errors.New("the stand has no address on the network")
	}

	stdout, err := h.runTool(ctx, tool, s, ip, dir, data, toolName)
	if err != nil {
		return res, err
	}

	standLog, err := stopStand(ctx, standName, dir)
	if err != nil {
		return res, err
	}
	if res.Stand, err = parseStandSummary(string(standLog)); err != nil {
		return res, err
	}
	if res.Reports, err = readReport(tool, dir, stdout); err != nil {
		return res, err
	}

	arrivalsPath := filepath.Join(dir, "arrivals.txt")
	offs, err := readArrivals(arrivalsPath)
	if err != nil {
		return res, err
	}
	if !h.keepArrivals {
		defer func() { _ = os.Remove(arrivalsPath) }()
	}
	res.Verdict = judge(offs, s.Rate, s.Duration)
	if sp, spErr := measureSpacing(offs, s.Rate, s.Duration); spErr == nil {
		res.Spacing = &sp
	}

	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return res, err
	}

	return res, os.WriteFile(filepath.Join(dir, "result.json"), b, 0o600)
}

// runTool writes the tool's inputs, runs its container to the end and keeps
// what it printed; it returns stdout, where LeetTest's report is.
func (h *harness) runTool(ctx context.Context, tool string, s setup, ip, dir, data, name string) ([]byte, error) {
	if err := writeInputs(tool, s, ip, dir); err != nil {
		return nil, err
	}
	args, err := toolRunArgs(name, h.network, tool, s, ip, h.binMount, data)
	if err != nil {
		return nil, err
	}
	err = os.WriteFile(filepath.Join(dir, "command.txt"), []byte("docker "+strings.Join(args, " ")+"\n"), 0o600)
	if err != nil {
		return nil, err
	}

	toolCtx, cancel := context.WithTimeout(ctx, s.Duration+2*time.Minute)
	stdout, stderr, runErr := docker(toolCtx, args...)
	cancel()
	err = errors.Join(
		os.WriteFile(filepath.Join(dir, "tool.stdout"), stdout, 0o600),
		os.WriteFile(filepath.Join(dir, "tool.stderr"), stderr, 0o600),
	)
	if err != nil {
		return nil, err
	}
	// LeetTest ends with 1, 2 or 3 for a run it judges; its report is still there.
	var exit *exec.ExitError
	if runErr != nil && (tool != leettest || !errors.As(runErr, &exit)) {
		return nil, fmt.Errorf("%s: %w: %s", tool, runErr, stderr)
	}

	return stdout, nil
}

// stopStand stops the stand so it writes the arrivals file, and returns what
// it printed. A stand that did not exit by itself wrote nothing.
func stopStand(ctx context.Context, name, dir string) ([]byte, error) {
	if _, err := dockerOK(ctx, "stop", "-t", "120", name); err != nil {
		return nil, err
	}
	logs, errLogs, err := docker(ctx, "logs", name)
	if err != nil {
		return nil, err
	}
	text := slices.Concat(logs, errLogs)
	err = os.WriteFile(filepath.Join(dir, "stand.log"), text, 0o600)
	if err != nil {
		return nil, err
	}
	code, err := dockerOK(ctx, "inspect", "-f", "{{.State.ExitCode}}", name)
	if err != nil {
		return nil, err
	}
	if code != "0" {
		return nil, fmt.Errorf("the stand ended with exit code %s, its arrivals file may be missing", code)
	}

	return text, nil
}

// waitReady waits for the stand's line saying it listens.
func waitReady(ctx context.Context, name string) error {
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		out, errOut, _ := docker(ctx, "logs", name)
		if standIsReady(string(out) + string(errOut)) {
			return nil
		}
		time.Sleep(100 * time.Millisecond) // polling a log nothing signals
	}

	return fmt.Errorf("%s did not print %q in 20 s", name, readyLine)
}

func standIsReady(logs string) bool { return strings.Contains(logs, readyLine) }

func writeInputs(tool string, s setup, ip, dir string) error {
	switch tool {
	case leettest:
		return os.WriteFile(filepath.Join(dir, "leettest.yaml"), []byte(leettestConfig(s, ip)), 0o600)
	case k6:
		return os.WriteFile(filepath.Join(dir, "k6.js"), []byte(k6Script(s, ip)), 0o600)
	}

	return nil
}

func readReport(tool, dir string, stdout []byte) (toolResult, error) {
	switch tool {
	case leettest:
		return parseLeetTestJSON(stdout)
	case ghz:
		b, err := os.ReadFile(filepath.Join(dir, "ghz.json"))
		if err != nil {
			return toolResult{}, err
		}

		return parseGhzJSON(b)
	case k6:
		b, err := os.ReadFile(filepath.Join(dir, "k6-summary.json"))
		if err != nil {
			return toolResult{}, err
		}

		return parseK6Summary(b)
	}

	return toolResult{}, fmt.Errorf("unknown tool %q", tool)
}
