FROM golang:1.8-alpine
WORKDIR /go/src/app
RUN apk --no-cache add git
COPY openvpn_exporter.go .
RUN go get -d ./...
# Build *really static*
ENV CGO_ENABLED 0
RUN go build -ldflags '-s' openvpn_exporter.go

# This requires Docker version 17.05 or newer
FROM scratch
COPY --from=0 /go/src/app/openvpn_exporter /bin/openvpn_exporter
ENTRYPOINT ["/bin/openvpn_exporter"]
