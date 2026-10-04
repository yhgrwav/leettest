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

package stand_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/yhgrwav/leettest/test/stand"
)

func readCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the stand wrote no certificate: %v", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("%s holds no PEM certificate", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}

	return cert
}

// checkTLS calls Health over TLS against the CA, with the client certificate
// when withCert.
func checkTLS(t *testing.T, s *stand.Stand, c stand.Certs, withCert bool) error {
	t.Helper()

	roots := x509.NewCertPool()
	raw, err := os.ReadFile(c.CA())
	if err != nil || !roots.AppendCertsFromPEM(raw) {
		t.Fatalf("CA %s: unreadable (%v)", c.CA(), err)
	}
	conf := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12}
	if withCert {
		pair, pairErr := tls.LoadX509KeyPair(c.ClientCert(), c.ClientKey())
		if pairErr != nil {
			t.Fatalf("client pair: %v", pairErr)
		}
		conf.Certificates = []tls.Certificate{pair}
	}

	conn, err := grpc.NewClient(s.Target(), grpc.WithTransportCredentials(credentials.NewTLS(conf)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), ceiling)
	defer cancel()

	_, err = grpc_health_v1.NewHealthClient(conn).Check(ctx, &grpc_health_v1.HealthCheckRequest{})

	return err
}

// A mutual-TLS stand answers a client holding its certificate and refuses one
// without, before the call arrives.
func TestStand_MTLS(t *testing.T) {
	c, err := stand.WriteCerts(t.TempDir())
	if err != nil {
		t.Fatalf("write certs: %v", err)
	}
	for _, path := range []string{c.CA(), c.ServerCert(), c.ClientCert()} {
		cert := readCert(t, path)
		key, ok := cert.PublicKey.(*ecdsa.PublicKey)
		if !ok || key.Curve != elliptic.P256() {
			t.Errorf("%s: key is %T, want ECDSA P-256", path, cert.PublicKey)
		}
		if life := cert.NotAfter.Sub(cert.NotBefore); life > 24*time.Hour {
			t.Errorf("%s is valid for %v, want at most 24h", path, life)
		}
	}

	opt, err := c.ServerOption(true)
	if err != nil {
		t.Fatalf("server option: %v", err)
	}
	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := stand.StartOn(lis, nil, opt)
	defer s.Stop()

	if err := checkTLS(t, s, c, true); err != nil {
		t.Errorf("with the client certificate: %v", err)
	}
	before := len(s.Arrivals())
	if err := checkTLS(t, s, c, false); err == nil {
		t.Error("without a client certificate the call was answered")
	}
	if n := len(s.Arrivals()) - before; n != 0 {
		t.Errorf("without a client certificate %d calls arrived, want none", n)
	}
}

// Every start writes a new set over the old one.
func TestStand_TLSFilesRewritten(t *testing.T) {
	dir := t.TempDir()

	first, err := stand.WriteCerts(dir)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	was := readCert(t, first.CA()).SerialNumber

	second, err := stand.WriteCerts(dir)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if now := readCert(t, second.CA()).SerialNumber; now.Cmp(was) == 0 {
		t.Errorf("the CA was not replaced: serial %v both times", now)
	}
}
