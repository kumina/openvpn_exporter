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
	"flag"
	"github.com/kumina/openvpn_exporter/exporters"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	"strings"
)

func main() {
	var (
		listenAddress      = flag.String("web.listen-address", ":9176", "Address to listen on for web interface and telemetry.")
		metricsPath        = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		openvpnStatusPaths = flag.String("openvpn.status_paths", "examples/client.status,examples/server2.status,examples/server3.status", "Paths at which OpenVPN places its status files.")
		ignoreIndividuals  = flag.Bool("ignore.individuals", false, "If ignoring metrics for individuals")
		openvpnStatusType  = flag.String("openvpn.status_type", "file", "Type of OpenVPN status, 'file' (personal vpn) or 'api' (access server).")
	)
	flag.Parse()

	log.Printf("Starting OpenVPN Exporter\n")
	log.Printf("Listen address: %v\n", *listenAddress)
	log.Printf("Metrics path: %v\n", *metricsPath)
	log.Printf("openvpn.status_path: %v\n", *openvpnStatusPaths)
	log.Printf("Ignore Individuals: %v\n", *ignoreIndividuals)
	log.Printf("openvpn.status_type: %v\n", *openvpnStatusType)
	exporter, err := exporters.NewOpenVPNExporter(strings.Split(*openvpnStatusPaths, ","), *ignoreIndividuals, *openvpnStatusType)
	if err != nil {
		panic(err)
	}
	prometheus.MustRegister(exporter)

	http.Handle(*metricsPath, promhttp.Handler())
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
