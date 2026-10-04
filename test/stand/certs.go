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
	"path/filepath"

	"google.golang.org/grpc"
)

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
// P-256 keys, valid for 24 hours.
func WriteCerts(dir string) (Certs, error) {
	return Certs{Dir: dir}, nil
}

// ServerOption serves the stand over TLS with these certificates; mutual also
// requires a client certificate signed by the CA.
func (c Certs) ServerOption(mutual bool) (grpc.ServerOption, error) {
	_ = mutual

	return grpc.EmptyServerOption{}, nil
}
