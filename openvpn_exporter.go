// Copyright 2017 Kumina, https://kumina.nl/
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type OpenvpnServerHeader struct {
	LabelColumns []string
	Metrics      []OpenvpnServerHeaderField
}

type OpenvpnServerHeaderField struct {
	Column    string
	Desc      *prometheus.Desc
	ValueType prometheus.ValueType
}

var (
	// Metrics exported both for client and server statistics.
	openvpnUpDesc = prometheus.NewDesc(
		prometheus.BuildFQName("openvpn", "", "up"),
		"Whether scraping OpenVPN's metrics was successful.",
		[]string{"status_path", "instance_name"}, nil)
	openvpnStatusUpdateTimeDesc = prometheus.NewDesc(
		prometheus.BuildFQName("openvpn", "", "status_update_time_seconds"),
		"UNIX timestamp at which the OpenVPN statistics were updated.",
		[]string{"status_path", "instance_name"}, nil)

	// Metrics specific to OpenVPN servers.
	openvpnServerHeaders = map[string]OpenvpnServerHeader{
		"CLIENT_LIST": OpenvpnServerHeader{
			LabelColumns: []string{
				"Common Name",
				"Connected Since (time_t)",
				"Real Address",
				"Virtual Address",
				"Username",
			},
			Metrics: []OpenvpnServerHeaderField{
				OpenvpnServerHeaderField{
					Column: "Bytes Received",
					Desc: prometheus.NewDesc(
						prometheus.BuildFQName("openvpn", "server", "client_received_bytes_total"),
						"Amount of data received over a connection on the VPN server, in bytes.",
						[]string{"status_path", "instance_name", "common_name", "connection_time", "real_address", "virtual_address", "username"}, nil),
					ValueType: prometheus.CounterValue,
				},
				OpenvpnServerHeaderField{
					Column: "Bytes Sent",
					Desc: prometheus.NewDesc(
						prometheus.BuildFQName("openvpn", "server", "client_sent_bytes_total"),
						"Amount of data sent over a connection on the VPN server, in bytes.",
						[]string{"status_path", "instance_name", "common_name", "connection_time", "real_address", "virtual_address", "username"}, nil),
					ValueType: prometheus.CounterValue,
				},
			},
		},
		"ROUTING_TABLE": OpenvpnServerHeader{
			LabelColumns: []string{
				"Common Name",
				"Real Address",
				"Virtual Address",
			},
			Metrics: []OpenvpnServerHeaderField{
				OpenvpnServerHeaderField{
					Column: "Last Ref (time_t)",
					Desc: prometheus.NewDesc(
						prometheus.BuildFQName("openvpn", "server", "route_last_reference_time_seconds"),
						"Time at which a route was last referenced, in seconds.",
						[]string{"status_path", "instance_name", "common_name", "real_address", "virtual_address"}, nil),
					ValueType: prometheus.GaugeValue,
				},
			},
		},
	}

	// Metrics specific to OpenVPN clients.
	openvpnClientDescs = map[string]*prometheus.Desc{
		"TUN/TAP read bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tun_tap_read_bytes_total"),
			"Total amount of TUN/TAP traffic read, in bytes.",
			[]string{"status_path", "instance_name"}, nil),
		"TUN/TAP write bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tun_tap_write_bytes_total"),
			"Total amount of TUN/TAP traffic written, in bytes.",
			[]string{"status_path", "instance_name"}, nil),
		"TCP/UDP read bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tcp_udp_read_bytes_total"),
			"Total amount of TCP/UDP traffic read, in bytes.",
			[]string{"status_path", "instance_name"}, nil),
		"TCP/UDP write bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tcp_udp_write_bytes_total"),
			"Total amount of TCP/UDP traffic written, in bytes.",
			[]string{"status_path", "instance_name"}, nil),
		"Auth read bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "auth_read_bytes_total"),
			"Total amount of authentication traffic read, in bytes.",
			[]string{"status_path", "instance_name"}, nil),
		"pre-compress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "pre_compress_bytes_total"),
			"Total amount of data before compression, in bytes.",
			[]string{"status_path", "instance_name"}, nil),
		"post-compress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "post_compress_bytes_total"),
			"Total amount of data after compression, in bytes.",
			[]string{"status_path", "instance_name"}, nil),
		"pre-decompress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "pre_decompress_bytes_total"),
			"Total amount of data before decompression, in bytes.",
			[]string{"status_path", "instance_name"}, nil),
		"post-decompress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "post_decompress_bytes_total"),
			"Total amount of data after decompression, in bytes.",
			[]string{"status_path", "instance_name"}, nil),
	}
)

// Converts OpenVPN status information into Prometheus metrics. This
// function automatically detects whether the file contains server or
// client metrics. For server metrics, it also distinguishes between the
// version 2 and 3 file formats.
func CollectStatusFromReader(statusPath string, instanceName string, file io.Reader, ch chan<- prometheus.Metric) error {
	reader := bufio.NewReader(file)
	buf, _ := reader.Peek(18)
	if bytes.HasPrefix(buf, []byte("TITLE,")) {
		// Server statistics, using format version 2.
		return CollectServerStatusFromReader(statusPath, instanceName, reader, ch, ",")
	} else if bytes.HasPrefix(buf, []byte("TITLE\t")) {
		// Server statistics, using format version 3. The only
		// difference compared to version 2 is that it uses tabs
		// instead of spaces.
		return CollectServerStatusFromReader(statusPath, instanceName, reader, ch, "\t")
	} else if bytes.HasPrefix(buf, []byte("OpenVPN STATISTICS")) {
		// Client statistics.
		return CollectClientStatusFromReader(statusPath, instanceName, reader, ch)
	} else {
		return fmt.Errorf("Unexpected file contents: %q", buf)
	}
}

// Converts OpenVPN server status information into Prometheus metrics.
func CollectServerStatusFromReader(statusPath string, instanceName string, file io.Reader, ch chan<- prometheus.Metric, separator string) error {
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	headersFound := map[string][]string{}
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), separator)
		if fields[0] == "END" && len(fields) == 1 {
			// Stats footer.
		} else if fields[0] == "GLOBAL_STATS" {
			// Global server statistics.
		} else if fields[0] == "HEADER" && len(fields) > 2 {
			// Column names for CLIENT_LIST and ROUTING_TABLE.
			headersFound[fields[1]] = fields[2:]
		} else if fields[0] == "TIME" && len(fields) == 3 {
			// Time at which the statistics were updated.
			time, err := strconv.ParseFloat(fields[2], 64)
			if err != nil {
				return err
			}
			ch <- prometheus.MustNewConstMetric(
				openvpnStatusUpdateTimeDesc,
				prometheus.GaugeValue,
				time,
				statusPath,
				instanceName)
		} else if fields[0] == "TITLE" && len(fields) == 2 {
			// OpenVPN version number.
		} else if header, ok := openvpnServerHeaders[fields[0]]; ok {
			// Entry that depends on a preceding HEADERS directive.
			columnNames, ok := headersFound[fields[0]]
			if !ok {
				return fmt.Errorf("%s should be preceded by HEADERS", fields[0])
			}
			if len(fields) != len(columnNames)+1 {
				return fmt.Errorf("HEADER for %s describes a different number of columns", fields[0])
			}

			// Store entry values in a map indexed by column name.
			columnValues := map[string]string{}
			for _, column := range header.LabelColumns {
				columnValues[column] = ""
			}
			for i, column := range columnNames {
				columnValues[column] = fields[i+1]
			}

			// Extract columns that should act as entry labels.
			labels := []string{statusPath, instanceName}
			for _, column := range header.LabelColumns {
				labels = append(labels, columnValues[column])
			}

			// Export relevant columns as individual metrics.
			for _, metric := range header.Metrics {
				if columnValue, ok := columnValues[metric.Column]; ok {
					value, err := strconv.ParseFloat(columnValue, 64)
					if err != nil {
						return err
					}
					ch <- prometheus.MustNewConstMetric(
						metric.Desc,
						metric.ValueType,
						value,
						labels...)
				}
			}
		} else {
			return fmt.Errorf("Unsupported key: %q", fields[0])
		}
	}
	return scanner.Err()
}

// Converts OpenVPN client status information into Prometheus metrics.
func CollectClientStatusFromReader(statusPath string, instanceName string, file io.Reader, ch chan<- prometheus.Metric) error {
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ",")
		if fields[0] == "END" && len(fields) == 1 {
			// Stats footer.
		} else if fields[0] == "OpenVPN STATISTICS" && len(fields) == 1 {
			// Stats header.
		} else if fields[0] == "Updated" && len(fields) == 2 {
			// Time at which the statistics were updated.
			location, _ := time.LoadLocation("Local")
			time, err := time.ParseInLocation("Mon Jan 2 15:04:05 2006", fields[1], location)
			if err != nil {
				return err
			}
			ch <- prometheus.MustNewConstMetric(
				openvpnStatusUpdateTimeDesc,
				prometheus.GaugeValue,
				float64(time.Unix()),
				statusPath,
				instanceName)
		} else if desc, ok := openvpnClientDescs[fields[0]]; ok && len(fields) == 2 {
			// Traffic counters.
			value, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				return err
			}
			ch <- prometheus.MustNewConstMetric(
				desc,
				prometheus.CounterValue,
				value,
				statusPath,
				instanceName)
		} else {
			return fmt.Errorf("Unsupported key: %q", fields[0])
		}
	}
	return scanner.Err()
}

func CollectStatusFromFile(statusPath string, instanceName string, ch chan<- prometheus.Metric) error {
	conn, err := os.Open(statusPath)
	defer conn.Close()
	if err != nil {
		return err
	}
	return CollectStatusFromReader(statusPath, instanceName, conn, ch)
}

type OpenVPNExporter struct {
	statusPaths map[string]string
}

func NewOpenVPNExporter(statusPaths map[string]string) (*OpenVPNExporter, error) {
	return &OpenVPNExporter{
		statusPaths: statusPaths,
	}, nil
}

func (e *OpenVPNExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- openvpnUpDesc
}

func (e *OpenVPNExporter) Collect(ch chan<- prometheus.Metric) {
	for instanceName, statusPath := range e.statusPaths {
		err := CollectStatusFromFile(statusPath, instanceName, ch)
		if err == nil {
			ch <- prometheus.MustNewConstMetric(
				openvpnUpDesc,
				prometheus.GaugeValue,
				1.0,
				statusPath,
				instanceName)
		} else {
			log.Printf("Failed to scrape showq socket: %s", err)
			ch <- prometheus.MustNewConstMetric(
				openvpnUpDesc,
				prometheus.GaugeValue,
				0.0,
				statusPath,
				instanceName)
		}
	}
}

func main() {
	var (
		listenAddress      = flag.String("web.listen-address", ":9176", "Address to listen on for web interface and telemetry.")
		metricsPath        = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		openvpnStatusPaths = flag.String("openvpn.status_paths", "examples/client.status,SomeServer:examples/server2.status,instance-three:examples/server3.status", "Comma-separated paths at which OpenVPN places its status files. Optionally prefixed with an instance name followed by a colon.")
	)
	flag.Parse()

	statusPaths := make(map[string]string)
	for _, segment := range strings.Split(*openvpnStatusPaths, ",") {
		s := strings.Split(segment, ":")
		n := len(s)
		if n < 1 || n > 2 || s[0] == "" || s[n-1] == "" {
			continue
		}
		statusPaths[s[0]] = s[n-1]
	}

	exporter, err := NewOpenVPNExporter(statusPaths)
	if err != nil {
		panic(err)
	}
	prometheus.MustRegister(exporter)

	http.Handle(*metricsPath, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`
			<html>
			<head><title>OpenVPN Exporter</title></head>
			<body>
			<h1>OpenVPN Exporter</h1>
			<p><a href='` + *metricsPath + `'>Metrics</a></p>
			</body>
			</html>`))
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
