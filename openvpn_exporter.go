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

var (
	// Metrics exported both for client and server statistics.
	openvpnUpDesc = prometheus.NewDesc(
		prometheus.BuildFQName("openvpn", "", "up"),
		"Whether scraping OpenVPN's metrics was successful.",
		[]string{"status_path"}, nil)
	openvpnStatusUpdateTimeDesc = prometheus.NewDesc(
		prometheus.BuildFQName("openvpn", "", "status_update_time_seconds"),
		"UNIX timestamp at which the OpenVPN statistics were updated.",
		[]string{"status_path"}, nil)

	// Metrics specific to OpenVPN servers.
	openvpnServerClientsDesc = prometheus.NewDesc(
		prometheus.BuildFQName("openvpn", "server", "clients"),
		"Number of clients connected to the OpenVPN server.",
		[]string{"status_path"}, nil)
	openvpnServerRoutesDesc = prometheus.NewDesc(
		prometheus.BuildFQName("openvpn", "server", "routes"),
		"Number of routes propagated by the OpenVPN server.",
		[]string{"status_path"}, nil)

    // Metrics exported about clients from the server
    openvpnServerClientStatsDesc = map[string]*prometheus.Desc{
        "Bytes Received": prometheus.NewDesc(
            prometheus.BuildFQName("openvpn", "server", "client_received_bytes_total"),
            "Total amount of traffic received from client, in bytes.",
            []string{"client", "real_ip", "virtual_ip"}, nil),
        "Bytes Sent": prometheus.NewDesc(
            prometheus.BuildFQName("openvpn", "server", "client_sent_bytes_total"),
            "Total amount of traffic sent to client, in bytes.",
            []string{"client", "real_ip", "virtual_ip"}, nil),
        "Client Up": prometheus.NewDesc(
            prometheus.BuildFQName("openvpn", "server", "client_up"),
            "Client up",
            []string{"client", "real_ip", "virtual_ip"}, nil),
        }



	// Metrics specific to OpenVPN clients.
	openvpnClientDescs = map[string]*prometheus.Desc{
		"TUN/TAP read bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tun_tap_read_bytes_total"),
			"Total amount of TUN/TAP traffic read, in bytes.",
			[]string{"status_path"}, nil),
		"TUN/TAP write bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tun_tap_write_bytes_total"),
			"Total amount of TUN/TAP traffic written, in bytes.",
			[]string{"status_path"}, nil),
		"TCP/UDP read bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tcp_udp_read_bytes_total"),
			"Total amount of TCP/UDP traffic read, in bytes.",
			[]string{"status_path"}, nil),
		"TCP/UDP write bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "tcp_udp_write_bytes_total"),
			"Total amount of TCP/UDP traffic written, in bytes.",
			[]string{"status_path"}, nil),
		"Auth read bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "auth_read_bytes_total"),
			"Total amount of authentication traffic read, in bytes.",
			[]string{"status_path"}, nil),
		"pre-compress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "pre_compress_bytes_total"),
			"Total amount of data before compression, in bytes.",
			[]string{"status_path"}, nil),
		"post-compress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "post_compress_bytes_total"),
			"Total amount of data after compression, in bytes.",
			[]string{"status_path"}, nil),
		"pre-decompress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "pre_decompress_bytes_total"),
			"Total amount of data before decompression, in bytes.",
			[]string{"status_path"}, nil),
		"post-decompress bytes": prometheus.NewDesc(
			prometheus.BuildFQName("openvpn", "client", "post_decompress_bytes_total"),
			"Total amount of data after decompression, in bytes.",
			[]string{"status_path"}, nil),
	}
)

// Converts OpenVPN status information into Prometheus metrics. This
// function automatically detects whether the file contains server or
// client metrics. For server metrics, it also distinguishes between the
// version 2 and 3 file formats.
func CollectStatusFromReader(statusPath string, file io.Reader, ch chan<- prometheus.Metric) error {
	reader := bufio.NewReader(file)
	buf, _ := reader.Peek(18)
	if bytes.HasPrefix(buf, []byte("TITLE,")) {
		// Server statistics, using format version 2.
		return CollectServerStatusFromReader(statusPath, reader, ch, ",")
	} else if bytes.HasPrefix(buf, []byte("TITLE\t")) {
		// Server statistics, using format version 3. The only
		// difference compared to version 2 is that it uses tabs
		// instead of spaces.
		return CollectServerStatusFromReader(statusPath, reader, ch, "\t")
	} else if bytes.HasPrefix(buf, []byte("OpenVPN STATISTICS")) {
		// Client statistics.
		return CollectClientStatusFromReader(statusPath, reader, ch)
	} else {
		return fmt.Errorf("Unexpected file contents: %q", buf)
	}
}

// Converts OpenVPN server status information into Prometheus metrics.
func CollectServerStatusFromReader(statusPath string, file io.Reader, ch chan<- prometheus.Metric, separator string) error {
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	clients := 0
	routes := 0
    client_name := ""
    real_address := ""
    virtual_address := ""
    traffic_data := map[string]float64 {
        "Bytes Sent": 0,
        "Bytes Received": 0,
        "Client Up": 0,
    }
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), separator)
		if fields[0] == "CLIENT_LIST" {
			// Per-client stats.
            client_name = fields[1]
            real_address = fields[2]
            virtual_address = fields[3]
            traffic_data["Bytes Received"],_ = strconv.ParseFloat(fields[4], 64)
            traffic_data["Bytes Sent"],_ = strconv.ParseFloat(fields[5], 64)
            traffic_data["Client Up"] = 1
			clients++
            for key, desc := range openvpnServerClientStatsDesc {
                ch <- prometheus.MustNewConstMetric(
                    desc,
                    prometheus.CounterValue,
                    traffic_data[key],
                    client_name, real_address, virtual_address,
                )
            }
		} else if fields[0] == "END" && len(fields) == 1 {
			// Stats footer.
		} else if fields[0] == "GLOBAL_STATS" {
			// Global server statistics.
		} else if fields[0] == "HEADER" && len(fields) > 2 {
			// Column names for CLIENT_LIST and ROUTING_TABLE.
		} else if fields[0] == "ROUTING_TABLE" {
			// Per-route stats.
			routes++
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
				statusPath)
		} else if fields[0] == "TITLE" && len(fields) == 2 {
			// OpenVPN version number.
		} else {
			return fmt.Errorf("Unsupported key: %q", fields[0])
		}
	}
	ch <- prometheus.MustNewConstMetric(
		openvpnServerClientsDesc,
		prometheus.GaugeValue,
		float64(clients),
		statusPath)
	ch <- prometheus.MustNewConstMetric(
		openvpnServerRoutesDesc,
		prometheus.GaugeValue,
		float64(routes),
		statusPath)
	return scanner.Err()
}

// Converts OpenVPN client status information into Prometheus metrics.
func CollectClientStatusFromReader(statusPath string, file io.Reader, ch chan<- prometheus.Metric) error {
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
				statusPath)
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
				statusPath)
		} else {
			return fmt.Errorf("Unsupported key: %q", fields[0])
		}
	}
	return scanner.Err()
}

func CollectStatusFromFile(statusPath string, ch chan<- prometheus.Metric) error {
	conn, err := os.Open(statusPath)
	if err != nil {
		return err
	}
	return CollectStatusFromReader(statusPath, conn, ch)
}

type OpenVPNExporter struct {
	statusPaths []string
}

func NewOpenVPNExporter(statusPaths []string) (*OpenVPNExporter, error) {
	return &OpenVPNExporter{
		statusPaths: statusPaths,
	}, nil
}

func (e *OpenVPNExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- openvpnUpDesc
}

func (e *OpenVPNExporter) Collect(ch chan<- prometheus.Metric) {
	for _, statusPath := range e.statusPaths {
		err := CollectStatusFromFile(statusPath, ch)
		if err == nil {
			ch <- prometheus.MustNewConstMetric(
				openvpnUpDesc,
				prometheus.GaugeValue,
				1.0,
				statusPath)
		} else {
			log.Printf("Failed to scrape showq socket: %s", err)
			ch <- prometheus.MustNewConstMetric(
				openvpnUpDesc,
				prometheus.GaugeValue,
				0.0,
				statusPath)
		}
	}
}

func main() {
	var (
		listenAddress      = flag.String("web.listen-address", ":9176", "Address to listen on for web interface and telemetry.")
		metricsPath        = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		openvpnStatusPaths = flag.String("openvpn.status_paths", "examples/client.status,examples/server2.status,examples/server3.status", "Paths at which OpenVPN places its status files.")
	)
	flag.Parse()

	exporter, err := NewOpenVPNExporter(strings.Split(*openvpnStatusPaths, ","))
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
