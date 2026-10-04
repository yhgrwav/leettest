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

package stand

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// certLife is how long a generated set is valid: long enough for a day of
// runs, short enough that a stray copy is worthless.
const certLife = 24 * time.Hour

// Certs are the PEM files of a TLS stand: a CA, the server's certificate for
// 127.0.0.1 and localhost, and a client certificate, all signed by the CA.
type Certs struct {
	Dir string
}

func (c Certs) CA() string         { return filepath.Join(c.Dir, "ca.pem") }
func (c Certs) ServerCert() string { return filepath.Join(c.Dir, "server.pem") }
func (c Certs) ServerKey() string  { return filepath.Join(c.Dir, "server-key.pem") }
func (c Certs) ClientCert() string { return filepath.Join(c.Dir, "client.pem") }
func (c Certs) ClientKey() string  { return filepath.Join(c.Dir, "client-key.pem") }

// WriteCerts generates a fresh set into dir, replacing what is there: ECDSA
// P-256 keys, valid for 24 hours. The CA is written first: a failure midway
// leaves a new CA beside old leaves, and the next start rewrites them all.
func WriteCerts(dir string) (Certs, error) {
	c := Certs{Dir: dir}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return c, err
	}

	now := time.Now()
	caKey, caCert, caDER, err := issue(&x509.Certificate{
		Subject:               pkix.Name{CommonName: "leettest stand CA"},
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}, nil, nil, now)
	if err != nil {
		return c, err
	}
	if err := writePEM(c.CA(), "CERTIFICATE", caDER); err != nil {
		return c, err
	}

	for _, leaf := range []struct {
		tmpl      *x509.Certificate
		cert, key string
	}{
		{&x509.Certificate{
			Subject:     pkix.Name{CommonName: "localhost"},
			DNSNames:    []string{"localhost"},
			IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}, c.ServerCert(), c.ServerKey()},
		{&x509.Certificate{
			Subject:     pkix.Name{CommonName: "leettest"},
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}, c.ClientCert(), c.ClientKey()},
	} {
		leaf.tmpl.KeyUsage = x509.KeyUsageDigitalSignature
		key, _, der, err := issue(leaf.tmpl, caCert, caKey, now)
		if err != nil {
			return c, err
		}
		keyDER, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return c, err
		}
		if err := writePEM(leaf.cert, "CERTIFICATE", der); err != nil {
			return c, err
		}
		if err := writePEM(leaf.key, "EC PRIVATE KEY", keyDER); err != nil {
			return c, err
		}
	}

	return c, nil
}

// issue makes a key and a certificate for tmpl, signed by parent, or
// self-signed when parent is nil.
func issue(tmpl, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, now time.Time) (
	*ecdsa.PrivateKey, *x509.Certificate, []byte, error,
) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, nil, err
	}
	tmpl.SerialNumber = serial
	// A minute back: a client whose clock is slightly behind still accepts it.
	tmpl.NotBefore = now.Add(-time.Minute)
	tmpl.NotAfter = tmpl.NotBefore.Add(certLife)

	if parent == nil {
		parent, parentKey = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		return nil, nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)

	return key, cert, der, err
}

func writePEM(path, kind string, der []byte) error {
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600)
}

// ServerOption serves the stand over TLS with these certificates; mutual also
// requires a client certificate signed by the CA.
func (c Certs) ServerOption(mutual bool) (grpc.ServerOption, error) {
	pair, err := tls.LoadX509KeyPair(c.ServerCert(), c.ServerKey())
	if err != nil {
		return nil, fmt.Errorf("stand certificate: %w", err)
	}
	conf := &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}

	if mutual {
		raw, err := os.ReadFile(c.CA())
		if err != nil {
			return nil, err
		}
		conf.ClientCAs = x509.NewCertPool()
		if !conf.ClientCAs.AppendCertsFromPEM(raw) {
			return nil, errors.New(c.CA() + " holds no certificate")
		}
		conf.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return grpc.Creds(credentials.NewTLS(conf)), nil
}
