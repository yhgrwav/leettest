#!/bin/sh
# Regenerates the stand's Go code from wallet.proto with pinned tools, on Linux
# x86-64. Run from the repository root; `make proto` runs it in Docker, CI runs
# it and checks that nothing changed.
set -eu

PROTOC_VERSION=36.2
PROTOC_SHA256=121f6c7afe1d4d0e3ea6aab9432038599250134cbf4474cb1167d2c7decd4278
# protoc-gen-go follows google.golang.org/protobuf in go.mod; protoc-gen-go-grpc
# is the release built against grpc not newer than go.mod's.
PROTOC_GEN_GO=v1.36.12
PROTOC_GEN_GO_GRPC=v1.6.2

tools=$(mktemp -d)
trap 'rm -rf "$tools"' EXIT

curl -fsSLo "$tools/protoc.zip" \
  "https://github.com/protocolbuffers/protobuf/releases/download/v$PROTOC_VERSION/protoc-$PROTOC_VERSION-linux-x86_64.zip"
echo "$PROTOC_SHA256  $tools/protoc.zip" | sha256sum -c -
unzip -q "$tools/protoc.zip" -d "$tools"

GOBIN="$tools/bin" go install "google.golang.org/protobuf/cmd/protoc-gen-go@$PROTOC_GEN_GO"
GOBIN="$tools/bin" go install "google.golang.org/grpc/cmd/protoc-gen-go-grpc@$PROTOC_GEN_GO_GRPC"

PATH="$tools/bin:$PATH" protoc -I test/stand/proto \
  --go_out=test/stand/proto --go_opt=paths=source_relative \
  --go-grpc_out=test/stand/proto --go-grpc_opt=paths=source_relative \
  test/stand/proto/wallet/v1/wallet.proto
