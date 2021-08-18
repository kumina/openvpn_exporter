# Prometheus OpenVPN exporter

**Please note:** This repository is currently unmaintained. Due to insufficient time and not using the exporter anymore
we decided to archive this project.

---

This repository provides code for a simple Prometheus metrics exporter
for [OpenVPN](https://openvpn.net/). Right now it can parse files
generated by OpenVPN's `--status`, having one of the following formats:

* Client statistics,
* Server statistics with `--status-version 2` (comma delimited),
* Server statistics with `--status-version 3` (tab delimited).

As it is not uncommon to run multiple instances of OpenVPN on a single
system (e.g., multiple servers, multiple clients or a mixture of both),
this exporter can be configured to scrape and export the status of
multiple status files, using the `-openvpn.status_paths` command line
flag. Paths need to be comma separated. Metrics for all status files are
exported over TCP port 9176.

The exporter also supports the OpenVPN "Access Server" via `sacli VPNStatus`, 
the "Access Server Client API" command. Use the `-openvpn.status_type api` 
command line flag to enable this mode and get the status from all of the 
OpenVPN processes running on the Access Server. In "api" mode the 
`-openvpn.status_paths` files are not used.

Please refer to this utility's `main()` function for a full list of
supported command line flags.

## Exposed metrics example

### Client statistics

For clients status files, the exporter generates metrics that may look
like this:

```
openvpn_client_auth_read_bytes_total{status_path="..."} 3.08854782e+08
openvpn_client_post_compress_bytes_total{status_path="..."} 4.5446864e+07
openvpn_client_post_decompress_bytes_total{status_path="..."} 2.16965355e+08
openvpn_client_pre_compress_bytes_total{status_path="..."} 4.538819e+07
openvpn_client_pre_decompress_bytes_total{status_path="..."} 1.62596168e+08
openvpn_client_tcp_udp_read_bytes_total{status_path="..."} 2.92806201e+08
openvpn_client_tcp_udp_write_bytes_total{status_path="..."} 1.97558969e+08
openvpn_client_tun_tap_read_bytes_total{status_path="..."} 1.53789941e+08
openvpn_client_tun_tap_write_bytes_total{status_path="..."} 3.08764078e+08
openvpn_status_update_time_seconds{status_path="..."} 1.490092749e+09
openvpn_up{status_path="..."} 1
```

### Server statistics

For server status files (both version 2 and 3), the exporter generates
metrics that may look like this:

```
openvpn_server_client_received_bytes_total{common_name="...",connection_time="...",real_address="...",status_path="...",username="...",virtual_address="..."} 139583
openvpn_server_client_sent_bytes_total{common_name="...",connection_time="...",real_address="...",status_path="...",username="...",virtual_address="..."} 710764
openvpn_server_route_last_reference_time_seconds{common_name="...",real_address="...",status_path="...",virtual_address="..."} 1.493018841e+09
openvpn_status_update_time_seconds{status_path="..."} 1.490089154e+09
openvpn_up{status_path="..."} 1
openvpn_server_connected_clients 1
```

## Usage

Usage of openvpn_exporter:

```sh
  -openvpn.status_paths string
    	Paths at which OpenVPN places its status files. (default "examples/client.status,examples/server2.status,examples/server3.status")
  -openvpn.status_type string
      Type of OpenVPN status, either "file" (personal vpn) or "api" (access server). (default "file")
  -web.listen-address string
    	Address to listen on for web interface and telemetry. (default ":9176")
  -web.telemetry-path string
    	Path under which to expose metrics. (default "/metrics")
  -ignore.individuals bool
        If ignoring metrics for individuals (default false)
```

## Execution

OpenVPN Personal:
```sh
openvpn_exporter -openvpn.status_paths /etc/openvpn/openvpn-status.log
```

OpenVPN Access Server:
```sh
openvpn_exporter -openvpn.status_type api
```

## Docker

To use with docker you must mount your status file to `/etc/openvpn_exporter/server.status`.

OpenVPN Personal:
```sh
docker run -p 9176:9176 \
  -v /path/to/openvpn_server.status:/etc/openvpn_exporter/server.status \
  kumina/openvpn-exporter -openvpn.status_paths /etc/openvpn_exporter/server.status
```

OpenVPN Access Server:
```sh
docker run -p 9176:9176 \
  -v /usr/local/openvpn_as/scripts:/usr/local/openvpn_as/scripts \
  kumina/openvpn-exporter -openvpn.status_type api
```

Metrics should be available at http://localhost:9176/metrics.

## Get a standalone executable binary

You can download the pre-compiled binaries from the
[releases page](https://github.com/kumina/openvpn_exporter/releases).
