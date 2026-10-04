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
	"crypto/tls"
	"crypto/x509"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/yhgrwav/leettest/test/stand"
)

// serve runs the stand with args until the test ends and returns its address.
func serve(t *testing.T, args ...string) string {
	t.Helper()

	stop := make(chan struct{})
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(append([]string{"-addr", "127.0.0.1:0"}, args...), io.Discard, io.Discard, stop,
			func(a string) { addr <- a })
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
	})

	select {
	case a := <-addr:
		return a
	case err := <-done:
		t.Fatalf("stand %v: %v", args, err)
	case <-time.After(5 * time.Second):
		t.Fatalf("stand %v did not start", args)
	}

	return ""
}

// callTLS checks Health on target over TLS, trusting the CA in certs, with
// the client certificate when withCert.
func callTLS(t *testing.T, target string, certs stand.Certs, withCert bool) error {
	t.Helper()

	raw, err := os.ReadFile(certs.CA())
	if err != nil {
		t.Fatalf("the stand wrote no CA: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		t.Fatalf("%s holds no certificate", certs.CA())
	}
	conf := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12}
	if withCert {
		pair, pairErr := tls.LoadX509KeyPair(certs.ClientCert(), certs.ClientKey())
		if pairErr != nil {
			t.Fatalf("client pair: %v", pairErr)
		}
		conf.Certificates = []tls.Certificate{pair}
	}

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(credentials.NewTLS(conf)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	_, err = grpc_health_v1.NewHealthClient(conn).Check(t.Context(), &grpc_health_v1.HealthCheckRequest{})

	return err
}

func TestRun_MTLSRequiresTheClientCertificate(t *testing.T) {
	certs := stand.Certs{Dir: t.TempDir()}
	target := serve(t, "-mtls", "-certs", certs.Dir)

	if err := callTLS(t, target, certs, true); err != nil {
		t.Errorf("with the client certificate: %v", err)
	}
	if err := callTLS(t, target, certs, false); err == nil {
		t.Error("-mtls answered a client without a certificate")
	}
}

func TestRun_TLSAnswersWithoutAClientCertificate(t *testing.T) {
	certs := stand.Certs{Dir: t.TempDir()}
	target := serve(t, "-tls", "-certs", certs.Dir)

	if err := callTLS(t, target, certs, false); err != nil {
		t.Errorf("-tls without a client certificate: %v", err)
	}
}

func TestRun_CertsDefaultUnderTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	serve(t, "-mtls")

	if _, err := os.Stat(filepath.Join(dir, "test", "stand", "certs", "ca.pem")); err != nil {
		t.Errorf("-mtls without -certs: %v", err)
	}
}
