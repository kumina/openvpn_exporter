# BUILD:
# docker build --force-rm=true -t openvpn_exporter .

# RUN:
# docker run -it -p 9176:9176 -v /path/to/openvpn_server.status:/etc/openvpn_exporter/server.status openvpn_exporter

FROM golang as builder

RUN mkdir /app
RUN mkdir /go/src/app
ADD . /go/src/app
WORKDIR /go/src/app

# Go dep
RUN go get -d ./...

# Build a standalone binary
RUN set -ex && \
  CGO_ENABLED=0 go build \
        -tags netgo \
        -o /app/openvpn_exporter \
        -v -a \
        -ldflags '-extldflags "-static"' && \
  ls

# Create the second stage with a basic image.
# this will drop any previous
# stages (defined as `FROM <some_image> as <some_name>`)
# allowing us to start with a fat build image and end up with
# a very small runtime image.

FROM busybox

# add compiled binary
COPY --from=builder /app/openvpn_exporter /openvpn_exporter

# add a default file to be processed
ADD examples/server2.status /etc/openvpn_exporter/server.status

# run
EXPOSE 9176
CMD ["/openvpn_exporter", "-openvpn.status_paths", "/etc/openvpn_exporter/server.status"]
