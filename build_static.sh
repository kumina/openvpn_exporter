#!/bin/sh

docker run -i -v `pwd`:/openvpn_exporter alpine:edge /bin/sh << 'EOF'
set -ex

# Install prerequisites for the build process.
apk update
apk add ca-certificates git go libc-dev
update-ca-certificates

# Build the openvpn_exporter.
cd /openvpn_exporter
export GOPATH=/gopath
go get -d ./...
go build --ldflags '-extldflags "-static"'
strip openvpn_exporter
EOF
